package service

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/setting/system_setting"
)

const (
	defaultOutboundMaxRedirects    = 10
	workerManualRedirectMode       = "manual"
	workerRedirectTokenHeader      = "X-New-API-Worker-Redirect-Token"
	workerTargetIPHeader           = "X-New-API-Worker-Target-IP"
	workerTargetProofHeader        = "X-New-API-Worker-Target-Proof"
	workerTargetBindingVersion     = "2"
	workerRedirectTokenRandomBytes = 32
)

type outboundRequest struct {
	URL     string
	Method  string
	Headers map[string]string
	Body    []byte
}

type outboundHTTPAdapter interface {
	Do(context.Context, outboundRequest) (*http.Response, error)
}

type outboundHTTPAdapterFunc func(context.Context, outboundRequest) (*http.Response, error)

func (f outboundHTTPAdapterFunc) Do(ctx context.Context, request outboundRequest) (*http.Response, error) {
	return f(ctx, request)
}

type directOutboundHTTPAdapter struct {
	getClient func() *http.Client
}

func (a directOutboundHTTPAdapter) Do(ctx context.Context, request outboundRequest) (*http.Response, error) {
	var body io.Reader
	if request.Body != nil {
		body = bytes.NewReader(request.Body)
	}
	httpRequest, err := http.NewRequestWithContext(ctx, request.Method, request.URL, body)
	if err != nil {
		return nil, &outboundRequestCreationError{Err: err}
	}
	for key, value := range request.Headers {
		httpRequest.Header.Set(key, value)
	}

	getClient := a.getClient
	if getClient == nil {
		getClient = GetSSRFProtectedHTTPClient
	}
	client := getClient()
	if client == nil {
		return nil, fmt.Errorf("outbound HTTP client is not initialized")
	}

	// Keep the configured transport, timeout and protected dialer, while exposing
	// redirect responses to the common policy for identical direct/worker rules.
	redirectlessClient := *client
	redirectlessClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return redirectlessClient.Do(httpRequest)
}

type workerOutboundHTTPAdapter struct {
	settings         system_setting.WorkerSettings
	do               func(*WorkerRequest) (*http.Response, error)
	newRedirectToken func() (string, error)
	resolveTarget    func(context.Context, string) (*WorkerTargetBinding, error)
}

func (a workerOutboundHTTPAdapter) Do(ctx context.Context, request outboundRequest) (*http.Response, error) {
	newRedirectToken := a.newRedirectToken
	if newRedirectToken == nil {
		newRedirectToken = generateWorkerRedirectToken
	}
	redirectToken, err := newRedirectToken()
	if err != nil {
		return nil, fmt.Errorf("failed to create worker redirect token: %w", err)
	}
	if redirectToken == "" {
		return nil, fmt.Errorf("failed to create worker redirect token: token is empty")
	}
	resolveTarget := a.resolveTarget
	if resolveTarget == nil {
		resolveTarget = func(ctx context.Context, rawURL string) (*WorkerTargetBinding, error) {
			return resolveWorkerTargetBinding(ctx, nil, rawURL)
		}
	}
	target, err := resolveTarget(ctx, request.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to bind worker target: %w", err)
	}

	workerRequest := &WorkerRequest{
		URL:           request.URL,
		Key:           a.settings.ValidKey,
		Method:        request.Method,
		Headers:       cloneOutboundHeaders(request.Headers),
		Body:          json.RawMessage(append([]byte(nil), request.Body...)),
		Redirect:      workerManualRedirectMode,
		RedirectToken: redirectToken,
		Target:        cloneWorkerTargetBinding(target),
	}
	var response *http.Response
	if a.do == nil {
		response, err = doWorkerRequest(ctx, a.settings, workerRequest)
	} else {
		response, err = a.do(workerRequest)
	}
	if err != nil || response == nil {
		return response, err
	}
	acknowledgedToken := response.Header.Get(workerRedirectTokenHeader)
	if subtle.ConstantTimeCompare([]byte(acknowledgedToken), []byte(redirectToken)) != 1 {
		return response, &outboundWorkerRedirectContractError{}
	}
	response.Header.Del(workerRedirectTokenHeader)
	if target != nil {
		connectedIP := response.Header.Get(workerTargetIPHeader)
		proof := response.Header.Get(workerTargetProofHeader)
		if err := verifyWorkerTargetProof(redirectToken, *target, connectedIP, proof); err != nil {
			return response, &outboundWorkerTargetContractError{Err: err}
		}
	}
	response.Header.Del(workerTargetIPHeader)
	response.Header.Del(workerTargetProofHeader)
	return response, nil
}

func resolveWorkerTargetBinding(ctx context.Context, resolver ssrfResolver, rawURL string) (*WorkerTargetBinding, error) {
	fetchSetting := system_setting.GetFetchSetting()
	if !fetchSetting.EnableSSRFProtection || !fetchSetting.ApplyIPFilterForDomain {
		return nil, nil
	}

	protection, err := common.NewSSRFProtectionFromFetchSetting(
		fetchSetting.AllowPrivateIp,
		fetchSetting.DomainFilterMode,
		fetchSetting.IpFilterMode,
		fetchSetting.DomainList,
		fetchSetting.IpList,
		fetchSetting.AllowedPorts,
		fetchSetting.ApplyIPFilterForDomain,
	)
	if err != nil {
		return nil, err
	}

	targetURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("invalid URL format: %w", err)
	}
	host := strings.TrimSuffix(strings.ToLower(targetURL.Hostname()), ".")
	port, err := strconv.Atoi(effectiveOutboundPort(targetURL))
	if err != nil {
		return nil, fmt.Errorf("invalid port: %s", targetURL.Port())
	}
	if err := protection.ValidateNetworkTarget(host, port); err != nil {
		return nil, err
	}
	if net.ParseIP(host) != nil {
		return nil, nil
	}

	if resolver == nil {
		resolver = net.DefaultResolver
	}
	resolved, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("DNS resolution failed for %s: %w", host, err)
	}

	uniqueIPs := make(map[string]struct{}, len(resolved))
	for _, ipAddr := range resolved {
		if ipAddr.IP == nil {
			continue
		}
		if err := protection.ValidateResolvedIP(host, ipAddr.IP); err != nil {
			return nil, err
		}
		uniqueIPs[ipAddr.IP.String()] = struct{}{}
	}
	if len(uniqueIPs) == 0 {
		return nil, fmt.Errorf("DNS resolution for %s returned no usable IP addresses", host)
	}

	ips := make([]string, 0, len(uniqueIPs))
	for ip := range uniqueIPs {
		ips = append(ips, ip)
	}
	sort.Strings(ips)
	return &WorkerTargetBinding{
		Version: workerTargetBindingVersion,
		Scheme:  strings.ToLower(targetURL.Scheme),
		Host:    host,
		Port:    port,
		IPs:     ips,
	}, nil
}

func cloneWorkerTargetBinding(binding *WorkerTargetBinding) *WorkerTargetBinding {
	if binding == nil {
		return nil
	}
	cloned := *binding
	cloned.IPs = append([]string(nil), binding.IPs...)
	return &cloned
}

func signWorkerTargetProof(token string, binding WorkerTargetBinding, connectedIP string) string {
	mac := hmac.New(sha256.New, []byte(token))
	_, _ = io.WriteString(mac, workerTargetProofPayload(binding, connectedIP))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func workerTargetProofPayload(binding WorkerTargetBinding, connectedIP string) string {
	return strings.Join([]string{
		binding.Version,
		binding.Scheme,
		binding.Host,
		strconv.Itoa(binding.Port),
		strings.Join(binding.IPs, ","),
		connectedIP,
	}, "\n")
}

func verifyWorkerTargetProof(token string, binding WorkerTargetBinding, connectedIP, proof string) error {
	if binding.Version != workerTargetBindingVersion {
		return fmt.Errorf("unsupported target binding version %q", binding.Version)
	}
	parsedConnectedIP := net.ParseIP(connectedIP)
	if parsedConnectedIP == nil {
		return fmt.Errorf("worker did not report a valid connected IP")
	}
	canonicalConnectedIP := parsedConnectedIP.String()
	approved := false
	for _, candidate := range binding.IPs {
		candidateIP := net.ParseIP(candidate)
		if candidateIP != nil && candidateIP.Equal(parsedConnectedIP) {
			approved = true
			break
		}
	}
	if !approved {
		return fmt.Errorf("worker connected to IP %s outside the approved target set", canonicalConnectedIP)
	}

	expected := signWorkerTargetProof(token, binding, canonicalConnectedIP)
	if subtle.ConstantTimeCompare([]byte(proof), []byte(expected)) != 1 {
		return fmt.Errorf("worker target binding proof is missing or invalid")
	}
	return nil
}

func generateWorkerRedirectToken() (string, error) {
	randomBytes := make([]byte, workerRedirectTokenRandomBytes)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(randomBytes), nil
}

type outboundResponseOwnership uint8

const (
	outboundResponseOwnedByCaller outboundResponseOwnership = iota
	outboundResponseOwnedByPolicy
)

type outboundHTTPPolicy struct {
	acceptStatus      func(int) bool
	responseOwnership outboundResponseOwnership
	validateURL       func(string) error
	maxRedirects      int
}

func notificationOutboundPolicy() outboundHTTPPolicy {
	return outboundHTTPPolicy{
		acceptStatus: func(statusCode int) bool {
			return statusCode >= http.StatusOK && statusCode < http.StatusMultipleChoices
		},
		responseOwnership: outboundResponseOwnedByPolicy,
	}
}

func downloadOutboundPolicy() outboundHTTPPolicy {
	return outboundHTTPPolicy{
		acceptStatus: func(int) bool { return true },
	}
}

func configuredOutboundHTTPAdapter() (outboundHTTPAdapter, bool) {
	settings := system_setting.GetWorkerSettings()
	if settings.Enabled() {
		return workerOutboundHTTPAdapter{settings: settings}, true
	}
	return directOutboundHTTPAdapter{}, false
}

func sendOutboundNotification(ctx context.Context, adapter outboundHTTPAdapter, throughWorker bool, subject string, request outboundRequest) error {
	_, err := doOutboundHTTPRequest(ctx, adapter, request, notificationOutboundPolicy())
	if err == nil {
		return nil
	}

	var statusErr *outboundHTTPStatusError
	if errors.As(err, &statusErr) {
		return fmt.Errorf("%s request failed with status code: %d", subject, statusErr.StatusCode)
	}
	var rejectedErr *outboundURLRejectedError
	if errors.As(err, &rejectedErr) && !throughWorker && !rejectedErr.Redirect {
		return rejectedErr
	}
	var creationErr *outboundRequestCreationError
	if errors.As(err, &creationErr) && !throughWorker {
		return fmt.Errorf("failed to create %s request: %v", subject, creationErr.Err)
	}
	if throughWorker {
		return fmt.Errorf("failed to send %s request through worker: %v", subject, err)
	}
	return fmt.Errorf("failed to send %s request: %v", subject, err)
}

type outboundURLRejectedError struct {
	URL      string
	Redirect bool
	Err      error
}

func (e *outboundURLRejectedError) Error() string {
	if e.Redirect {
		return fmt.Sprintf("redirect to %s blocked: %v", e.URL, e.Err)
	}
	return fmt.Sprintf("request reject: %v", e.Err)
}

func (e *outboundURLRejectedError) Unwrap() error {
	return e.Err
}

type outboundHTTPStatusError struct {
	StatusCode int
}

func (e *outboundHTTPStatusError) Error() string {
	return fmt.Sprintf("unexpected response status code: %d", e.StatusCode)
}

type outboundRequestCreationError struct {
	Err error
}

func (e *outboundRequestCreationError) Error() string {
	return e.Err.Error()
}

func (e *outboundRequestCreationError) Unwrap() error {
	return e.Err
}

type outboundWorkerRedirectContractError struct{}

func (e *outboundWorkerRedirectContractError) Error() string {
	return "worker did not confirm required manual redirect handling"
}

type outboundWorkerTargetContractError struct {
	Err error
}

func (e *outboundWorkerTargetContractError) Error() string {
	return fmt.Sprintf("worker did not confirm required target binding: %v", e.Err)
}

func (e *outboundWorkerTargetContractError) Unwrap() error {
	return e.Err
}

func doOutboundHTTPRequest(ctx context.Context, adapter outboundHTTPAdapter, request outboundRequest, policy outboundHTTPPolicy) (*http.Response, error) {
	validateURL := policy.validateURL
	if validateURL == nil {
		validateURL = ValidateSSRFProtectedFetchURL
	}
	if err := validateURL(request.URL); err != nil {
		return nil, &outboundURLRejectedError{URL: request.URL, Err: err}
	}

	maxRedirects := policy.maxRedirects
	if maxRedirects <= 0 {
		maxRedirects = defaultOutboundMaxRedirects
	}
	current := cloneOutboundRequest(request)
	redirects := 0
	for {
		response, err := adapter.Do(ctx, current)
		if err != nil {
			closeOutboundResponse(response)
			return nil, err
		}
		if response == nil {
			return nil, fmt.Errorf("outbound HTTP adapter returned a nil response")
		}

		location := ""
		if isOutboundRedirect(response.StatusCode) {
			location = response.Header.Get("Location")
		}
		if location != "" {
			redirects++
			if redirects >= maxRedirects {
				closeOutboundResponse(response)
				return nil, fmt.Errorf("stopped after %d redirects", maxRedirects)
			}

			next, err := redirectedOutboundRequest(current, response.StatusCode, location)
			if err != nil {
				closeOutboundResponse(response)
				return nil, err
			}
			if err := validateURL(next.URL); err != nil {
				closeOutboundResponse(response)
				return nil, &outboundURLRejectedError{URL: next.URL, Redirect: true, Err: err}
			}
			closeOutboundResponse(response)
			current = next
			continue
		}

		if policy.acceptStatus != nil && !policy.acceptStatus(response.StatusCode) {
			closeOutboundResponse(response)
			return nil, &outboundHTTPStatusError{StatusCode: response.StatusCode}
		}
		if policy.responseOwnership == outboundResponseOwnedByPolicy {
			closeOutboundResponse(response)
		}
		return response, nil
	}
}

func redirectedOutboundRequest(request outboundRequest, statusCode int, location string) (outboundRequest, error) {
	currentURL, err := url.Parse(request.URL)
	if err != nil {
		return outboundRequest{}, fmt.Errorf("failed to parse request URL for redirect: %v", err)
	}
	locationURL, err := url.Parse(location)
	if err != nil {
		return outboundRequest{}, fmt.Errorf("failed to parse redirect URL %q: %v", location, err)
	}
	nextURL := currentURL.ResolveReference(locationURL)
	next := cloneOutboundRequest(request)
	next.URL = nextURL.String()

	method := request.Method
	if method == "" {
		method = http.MethodGet
	}
	switch statusCode {
	case http.StatusMovedPermanently, http.StatusFound:
		if method != http.MethodGet && method != http.MethodHead {
			next.Method = http.MethodGet
			next.Body = nil
		}
	case http.StatusSeeOther:
		if method != http.MethodHead {
			next.Method = http.MethodGet
		}
		next.Body = nil
	}

	if !sameOutboundOrigin(currentURL, nextURL) {
		removeOutboundHeaders(next.Headers,
			"Authorization",
			"Proxy-Authorization",
			"Cookie",
			"Cookie2",
			"WWW-Authenticate",
			"X-Webhook-Signature",
		)
	}
	return next, nil
}

func sameOutboundOrigin(left *url.URL, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(left.Hostname(), right.Hostname()) &&
		effectiveOutboundPort(left) == effectiveOutboundPort(right)
}

func effectiveOutboundPort(target *url.URL) string {
	if port := target.Port(); port != "" {
		return port
	}
	switch strings.ToLower(target.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func isOutboundRedirect(statusCode int) bool {
	switch statusCode {
	case http.StatusMovedPermanently,
		http.StatusFound,
		http.StatusSeeOther,
		http.StatusTemporaryRedirect,
		http.StatusPermanentRedirect:
		return true
	default:
		return false
	}
}

func closeOutboundResponse(response *http.Response) {
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
}

func cloneOutboundRequest(request outboundRequest) outboundRequest {
	request.Headers = cloneOutboundHeaders(request.Headers)
	request.Body = append([]byte(nil), request.Body...)
	return request
}

func cloneOutboundHeaders(headers map[string]string) map[string]string {
	if headers == nil {
		return nil
	}
	cloned := make(map[string]string, len(headers))
	maps.Copy(cloned, headers)
	return cloned
}

func removeOutboundHeaders(headers map[string]string, names ...string) {
	for key := range headers {
		for _, name := range names {
			if strings.EqualFold(key, name) {
				delete(headers, key)
				break
			}
		}
	}
}
