package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/logger"
	"eflow.local/new-api/setting/system_setting"

	"golang.org/x/net/proxy"
)

var (
	httpClient              *http.Client
	ssrfProtectedHTTPClient *http.Client
	proxyClients            = proxyHTTPClientCache{
		clients: make(map[string]*http.Client),
		aliases: make(map[string]string),
	}
	legacyProxyURLWarnings sync.Map
)

type proxyHTTPClientCache struct {
	mutex   sync.RWMutex
	clients map[string]*http.Client
	aliases map[string]string
}

type proxyURLConfig struct {
	parsedURL *url.URL
	cacheKey  string
}

func checkRedirect(req *http.Request, via []*http.Request) error {
	urlStr := req.URL.String()
	if err := validateURLWithCurrentFetchSetting(urlStr, true); err != nil {
		return fmt.Errorf("redirect to %s blocked: %v", urlStr, err)
	}
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	return nil
}

func keepRedirectResponse(*http.Request, []*http.Request) error {
	return http.ErrUseLastResponse
}

// NewRedirectlessHTTPClient returns a shallow copy of client that never follows
// redirects; the 3xx response is handed back to the caller as is. Use it for
// outbound requests that carry channel credentials: on a cross-host redirect
// net/http only strips Authorization/Cookie, so custom auth headers such as
// x-api-key, x-goog-api-key, api-key or mj-api-secret would otherwise reach the
// redirect target. The cached client is not mutated and the copy keeps sharing
// its transport and connection pool.
func NewRedirectlessHTTPClient(client *http.Client) *http.Client {
	if client == nil {
		return &http.Client{CheckRedirect: keepRedirectResponse}
	}
	redirectless := *client
	redirectless.CheckRedirect = keepRedirectResponse
	return &redirectless
}

// NewCredentialStrippingRedirectClient returns a shallow copy of client that
// still follows redirects through the client's own CheckRedirect (SSRF checks
// included), but removes headerNames from every hop whose origin differs from
// the initial request. Use it only where following redirects is required, e.g.
// media downloads that are redirected to signed storage URLs.
func NewCredentialStrippingRedirectClient(client *http.Client, headerNames ...string) *http.Client {
	var cloned http.Client
	if client != nil {
		cloned = *client
	}
	base := cloned.CheckRedirect
	cloned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if base != nil {
			if err := base(req, via); err != nil {
				return err
			}
		} else if len(via) >= 10 {
			return fmt.Errorf("stopped after 10 redirects")
		}
		if len(via) > 0 && !sameURLOrigin(via[0].URL, req.URL) {
			for _, name := range headerNames {
				req.Header.Del(name)
			}
		}
		return nil
	}
	return &cloned
}

func sameURLOrigin(a, b *url.URL) bool {
	if a == nil || b == nil {
		return false
	}
	return strings.EqualFold(a.Scheme, b.Scheme) && strings.EqualFold(canonicalURLHostPort(a), canonicalURLHostPort(b))
}

func canonicalURLHostPort(u *url.URL) string {
	port := u.Port()
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "https":
			port = "443"
		case "http":
			port = "80"
		}
	}
	return net.JoinHostPort(u.Hostname(), port)
}

func checkProtectedFetchRedirect(req *http.Request, via []*http.Request) error {
	urlStr := req.URL.String()
	if err := ValidateSSRFProtectedFetchURL(urlStr); err != nil {
		return fmt.Errorf("redirect to %s blocked: %v", urlStr, err)
	}
	if len(via) >= 10 {
		return fmt.Errorf("stopped after 10 redirects")
	}
	return nil
}

func validateURLWithCurrentFetchSetting(urlStr string, applyDomainIPFilter bool) error {
	fetchSetting := system_setting.GetFetchSetting()
	return common.ValidateURLWithFetchSetting(urlStr, fetchSetting.EnableSSRFProtection, fetchSetting.AllowPrivateIp, fetchSetting.DomainFilterMode, fetchSetting.IpFilterMode, fetchSetting.DomainList, fetchSetting.IpList, fetchSetting.AllowedPorts, applyDomainIPFilter && fetchSetting.ApplyIPFilterForDomain)
}

func ValidateSSRFProtectedFetchURL(urlStr string) error {
	return validateURLWithCurrentFetchSetting(urlStr, true)
}

func newRelayHTTPTransport() *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = common.RelayMaxIdleConns
	transport.MaxIdleConnsPerHost = common.RelayMaxIdleConnsPerHost
	transport.IdleConnTimeout = time.Duration(common.RelayIdleConnTimeout) * time.Second
	transport.ResponseHeaderTimeout = 0
	if seconds := common.RelayResponseHeaderTimeout; seconds > 0 {
		// Clamp before conversion so large values cannot wrap into a short timeout.
		seconds = min(seconds, int(math.MaxInt64/int64(time.Second)))
		transport.ResponseHeaderTimeout = time.Duration(seconds) * time.Second
	}
	transport.ForceAttemptHTTP2 = true
	if common.TLSInsecureSkipVerify {
		transport.TLSClientConfig = common.InsecureTLSConfig.Clone()
	}
	return transport
}

func newRelayHTTPClient(transport http.RoundTripper) *http.Client {
	client := &http.Client{Transport: transport, CheckRedirect: checkRedirect}
	if common.RelayTimeout != 0 {
		client.Timeout = time.Duration(common.RelayTimeout) * time.Second
	}
	return client
}

func clientCacheKey(proxyCacheKey string, policy HTTPTransportPolicy) string {
	return proxyCacheKey + "\x00" + policy.cacheKeyPart()
}

func InitHttpClient() {
	policy := defaultHTTPTransportPolicy()
	httpClient = newDirectHTTPClient(policy, nil)
	proxyClients.store(clientCacheKey("", policy), httpClient)
	ssrfProtectedHTTPClient = newProtectedFetchHTTPClient()
}

// GetHttpClient returns the general outbound client used by relay/provider integrations.
func GetHttpClient() *http.Client {
	return httpClient
}

func GetSSRFProtectedHTTPClient() *http.Client {
	if fetchSetting := system_setting.GetFetchSetting(); fetchSetting != nil && !fetchSetting.EnableSSRFProtection {
		return GetHttpClient()
	}
	return ssrfProtectedHTTPClient
}

func newProxyURLConfig(parsedURL *url.URL) *proxyURLConfig {
	return &proxyURLConfig{parsedURL: parsedURL, cacheKey: parsedURL.String()}
}

func warnLegacyProxyURLOnce(config *proxyURLConfig) {
	if _, loaded := legacyProxyURLWarnings.LoadOrStore(config.cacheKey, struct{}{}); loaded {
		return
	}
	logger.LogWarn(context.Background(), fmt.Sprintf(
		"legacy proxy URL suffix ignored at runtime: scheme=%s host=%s; update the channel proxy setting",
		config.parsedURL.Scheme, config.parsedURL.Host,
	))
}

func NormalizeProxyURL(rawProxyURL string) (string, error) {
	parsedURL, legacySuffixStripped, err := common.ParseProxyURLRuntime(rawProxyURL)
	if err != nil || parsedURL == nil {
		return "", err
	}
	config := newProxyURLConfig(parsedURL)
	if legacySuffixStripped {
		warnLegacyProxyURLOnce(config)
	}
	return config.cacheKey, nil
}

func ValidateProxyURL(rawProxyURL string) error {
	_, err := common.ParseProxyURLStrict(rawProxyURL)
	return err
}

func (cache *proxyHTTPClientCache) store(key string, client *http.Client) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	cache.clients[key] = client
}

func (cache *proxyHTTPClientCache) get(rawProxyURL string, policy HTTPTransportPolicy) (*http.Client, bool) {
	cache.mutex.RLock()
	defer cache.mutex.RUnlock()
	proxyKey := rawProxyURL
	if canonicalKey, ok := cache.aliases[rawProxyURL]; ok {
		proxyKey = canonicalKey
	}
	client, ok := cache.clients[clientCacheKey(proxyKey, policy)]
	return client, ok
}

func (cache *proxyHTTPClientCache) getOrCreate(rawProxyURL string, config *proxyURLConfig, policy HTTPTransportPolicy, factory func() (*http.Client, error)) (*http.Client, error) {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	proxyKey := ""
	if config != nil {
		proxyKey = config.cacheKey
		cache.aliases[rawProxyURL] = proxyKey
	}
	key := clientCacheKey(proxyKey, policy)
	if client, ok := cache.clients[key]; ok {
		return client, nil
	}
	client, err := factory()
	if err != nil {
		return nil, err
	}
	cache.clients[key] = client
	return client, nil
}

func (cache *proxyHTTPClientCache) removeProxy(proxyCacheKey string) []*http.Client {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	var removed []*http.Client
	prefix := proxyCacheKey + "\x00"
	for key, client := range cache.clients {
		if strings.HasPrefix(key, prefix) {
			removed = append(removed, client)
			delete(cache.clients, key)
		}
	}
	for alias, canonicalKey := range cache.aliases {
		if canonicalKey == proxyCacheKey {
			delete(cache.aliases, alias)
		}
	}
	return removed
}

func (cache *proxyHTTPClientCache) reset() map[string]*http.Client {
	cache.mutex.Lock()
	defer cache.mutex.Unlock()
	oldClients := cache.clients
	cache.clients = make(map[string]*http.Client)
	cache.aliases = make(map[string]string)
	return oldClients
}

func configureProxyTransport(transport *http.Transport, proxyURL *url.URL) error {
	switch proxyURL.Scheme {
	case "http", "https":
		transport.Proxy = http.ProxyURL(proxyURL)
	case "socks5", "socks5h":
		transport.Proxy = nil
		forwardDialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
		dialer, err := proxy.FromURL(proxyURL, forwardDialer)
		if err != nil {
			return err
		}
		contextDialer, ok := dialer.(proxy.ContextDialer)
		if !ok {
			return fmt.Errorf("SOCKS proxy dialer does not support context cancellation")
		}
		transport.DialContext = contextDialer.DialContext
	default:
		return fmt.Errorf("unsupported proxy scheme")
	}
	return nil
}

func newTransportFactory(proxyURL *url.URL, tlsConfig *tls.Config) (func() *http.Transport, error) {
	if proxyURL != nil {
		probe := newRelayHTTPTransport()
		if err := configureProxyTransport(probe, proxyURL); err != nil {
			return nil, err
		}
	}
	return func() *http.Transport {
		transport := newRelayHTTPTransport()
		if proxyURL != nil {
			_ = configureProxyTransport(transport, proxyURL)
		}
		if tlsConfig != nil {
			transport.TLSClientConfig = tlsConfig.Clone()
		}
		return transport
	}, nil
}

func newHTTPClientFromPolicy(policy HTTPTransportPolicy, proxyURL *url.URL, tlsConfig *tls.Config) (*http.Client, error) {
	factory, err := newTransportFactory(proxyURL, tlsConfig)
	if err != nil {
		return nil, err
	}
	if policy.Protocol == dto.HTTPProtocolHTTP1 || policy.Shards == 1 {
		transport := factory()
		applyHTTPTransportPolicy(transport, policy)
		return newRelayHTTPClient(transport), nil
	}
	return newRelayHTTPClient(newShardedRoundTripper(policy, func() *http.Transport {
		transport := factory()
		applyHTTPTransportPolicy(transport, policy)
		return transport
	})), nil
}

func newDirectHTTPClient(policy HTTPTransportPolicy, tlsConfig *tls.Config) *http.Client {
	client, _ := newHTTPClientFromPolicy(policy, nil, tlsConfig)
	return client
}

func GetHttpClientWithProxy(rawProxyURL string) (*http.Client, error) {
	return GetHttpClientWithProxySettings(rawProxyURL, dto.ChannelSettings{})
}

func GetHttpClientWithProxySettings(rawProxyURL string, settings dto.ChannelSettings) (*http.Client, error) {
	policy := NormalizeHTTPTransportPolicy(settings)
	trimmedProxyURL := strings.TrimSpace(rawProxyURL)
	if trimmedProxyURL == "" {
		if policy == defaultHTTPTransportPolicy() {
			if client := GetHttpClient(); client != nil {
				return client, nil
			}
			return http.DefaultClient, nil
		}
		if client, ok := proxyClients.get("", policy); ok {
			return client, nil
		}
		return proxyClients.getOrCreate("", nil, policy, func() (*http.Client, error) {
			return newDirectHTTPClient(policy, nil), nil
		})
	}
	if client, ok := proxyClients.get(trimmedProxyURL, policy); ok {
		return client, nil
	}
	parsedURL, legacySuffixStripped, err := common.ParseProxyURLRuntime(trimmedProxyURL)
	if err != nil {
		return nil, err
	}
	config := newProxyURLConfig(parsedURL)
	if legacySuffixStripped {
		warnLegacyProxyURLOnce(config)
	}
	return proxyClients.getOrCreate(trimmedProxyURL, config, policy, func() (*http.Client, error) {
		return newHTTPClientFromPolicy(policy, config.parsedURL, nil)
	})
}

func InvalidateProxyClient(rawProxyURL string) {
	parsedURL, _, err := common.ParseProxyURLRuntime(rawProxyURL)
	if err != nil || parsedURL == nil {
		return
	}
	for _, client := range proxyClients.removeProxy(parsedURL.String()) {
		client.CloseIdleConnections()
	}
}

func ResetProxyClientCache() {
	defaultClient := httpClient
	for _, client := range proxyClients.reset() {
		client.CloseIdleConnections()
	}
	if defaultClient != nil {
		proxyClients.store(clientCacheKey("", defaultHTTPTransportPolicy()), defaultClient)
	}
}

// Deprecated: use GetHttpClientWithProxy.
func NewProxyHttpClient(proxyURL string) (*http.Client, error) {
	return GetHttpClientWithProxy(proxyURL)
}
