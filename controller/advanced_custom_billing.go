package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	relaychannel "eflow.local/new-api/relay/channel"
	"eflow.local/new-api/relaykit/dto"
	"eflow.local/new-api/service"
)

const maxAdvancedCustomBalanceResponseBytes = 256 << 10

type advancedCustomBalanceResult struct {
	Balance     *float64
	RawResponse string
}

func fetchAdvancedCustomManagementBody(channel *model.Channel, managementPath string) ([]byte, error) {
	if channel == nil {
		return nil, errors.New("channel is nil")
	}
	config := channel.GetOtherSettings().AdvancedCustom
	if config == nil {
		return nil, errors.New("advanced_custom is not configured")
	}
	if err := config.Validate(); err != nil {
		return nil, err
	}
	var route dto.AdvancedCustomRoute
	var ok bool
	switch managementPath {
	case dto.AdvancedCustomModelListPath:
		route, ok = config.ModelListRoute()
	case dto.AdvancedCustomBalancePath:
		route, ok = config.BalanceRoute()
	default:
		return nil, fmt.Errorf("unsupported advanced custom management path: %s", managementPath)
	}
	if !ok {
		return nil, fmt.Errorf("advanced custom channel does not configure a %s route", managementPath)
	}
	requestURL, err := advancedCustomRouteURL(channel.GetBaseURL(), route.UpstreamPath)
	if err != nil {
		return nil, err
	}
	key, _, apiErr := channel.GetNextEnabledKey()
	if apiErr != nil {
		return nil, apiErr
	}
	key = strings.TrimSpace(key)
	requestURL, headers, err := applyAdvancedCustomRouteAuth(requestURL, route.Auth, key)
	if err != nil {
		return nil, err
	}
	if err := applyAdvancedCustomManagementHeaderOverrides(channel, key, headers); err != nil {
		return nil, err
	}
	request, err := http.NewRequest(http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	for name, values := range headers {
		for _, value := range values {
			if strings.EqualFold(name, "Host") {
				request.Host = value
				continue
			}
			request.Header.Add(name, value)
		}
	}
	channelSettings := channel.GetSetting()
	client, err := service.GetHttpClientWithProxySettings(channelSettings.Proxy, channelSettings)
	if err != nil {
		return nil, err
	}
	// Route auth and header overrides carry the channel key; never follow a
	// redirect with them.
	response, err := service.NewRedirectlessHTTPClient(client).Do(request)
	if err != nil {
		return nil, sanitizeAdvancedCustomSecretError(err, key, requestURL)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("advanced custom management status code: %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxAdvancedCustomBalanceResponseBytes+1))
	if err != nil {
		return nil, sanitizeAdvancedCustomSecretError(err, key, requestURL)
	}
	if len(body) > maxAdvancedCustomBalanceResponseBytes {
		return nil, fmt.Errorf("advanced custom management response exceeds %d bytes", maxAdvancedCustomBalanceResponseBytes)
	}
	return body, nil
}

// applyAdvancedCustomManagementHeaderOverrides applies saved channel header
// overrides after route authentication, matching the precedence used by
// ordinary model discovery. Client-header placeholders cannot be resolved for
// a background management request and are therefore omitted.
func applyAdvancedCustomManagementHeaderOverrides(channel *model.Channel, key string, headers http.Header) error {
	if channel == nil {
		return errors.New("channel is nil")
	}
	for name, rawValue := range channel.GetHeaderOverride() {
		if relaychannel.IsHeaderPassthroughRuleKey(name) {
			continue
		}
		value, ok := rawValue.(string)
		if !ok {
			return fmt.Errorf("invalid header override for key %s", name)
		}
		trimmed := strings.TrimSpace(value)
		if strings.HasPrefix(strings.ToLower(trimmed), "{client_header:") {
			headers.Del(name)
			continue
		}
		value = strings.ReplaceAll(value, "{api_key}", key)
		if strings.TrimSpace(value) == "" {
			headers.Del(name)
			continue
		}
		headers.Set(name, value)
	}
	return nil
}

// fetchAdvancedCustomBalance handles only the management route configured in
// settings. It intentionally does not alter the legacy Custom relay path.
func fetchAdvancedCustomBalance(channel *model.Channel) (advancedCustomBalanceResult, error) {
	if channel == nil {
		return advancedCustomBalanceResult{}, errors.New("channel is nil")
	}
	body, err := fetchAdvancedCustomManagementBody(channel, dto.AdvancedCustomBalancePath)
	if err != nil {
		return advancedCustomBalanceResult{}, err
	}

	var raw json.RawMessage
	if err := common.Unmarshal(body, &raw); err != nil {
		// Providers are allowed to expose a provider-specific text payload. Keep
		// it available to the management UI instead of treating it as a failed
		// balance probe (and, consequently, auto-banning the channel).
		return advancedCustomBalanceResult{RawResponse: string(body)}, nil
	}
	if common.GetJsonType(raw) == "object" {
		var summary struct {
			Object         string          `json:"object"`
			TotalAvailable json.RawMessage `json:"total_available"`
		}
		if err := common.Unmarshal(body, &summary); err == nil &&
			summary.Object == "credit_summary" &&
			common.GetJsonType(summary.TotalAvailable) == "number" {
			var balance float64
			if err := common.Unmarshal(summary.TotalAvailable, &balance); err == nil &&
				!math.IsNaN(balance) && !math.IsInf(balance, 0) {
				// Management probes are also used with an in-memory temporary
				// channel (for preview/tests), where no model DB is configured.
				// Keep the parsed value available without dereferencing a nil DB;
				// normal persisted channels still update their balance here.
				if model.DB != nil {
					channel.UpdateBalance(balance)
				}
				return advancedCustomBalanceResult{Balance: &balance}, nil
			}
		}
	}
	// Keep the validated payload as-is. The management UI renders it in a
	// code block, and avoiding a second marshal preserves provider-specific
	// numeric/string fields exactly.
	return advancedCustomBalanceResult{RawResponse: string(body)}, nil
}

func advancedCustomRouteURL(baseURL, upstreamPath string) (string, error) {
	upstreamPath = strings.TrimSpace(upstreamPath)
	if strings.HasPrefix(upstreamPath, "/") {
		if strings.HasPrefix(upstreamPath, "//") {
			return "", errors.New("advanced custom upstream path must be a full URL or a path starting with /")
		}
		parsedBase, err := url.Parse(strings.TrimSpace(baseURL))
		if err != nil || parsedBase.Host == "" || (parsedBase.Scheme != "http" && parsedBase.Scheme != "https") {
			return "", errors.New("channel base URL must use http(s) for a relative advanced custom route")
		}
		parsedPath, err := url.Parse(upstreamPath)
		if err != nil {
			return "", err
		}
		parsedBase.Path = strings.TrimRight(parsedBase.Path, "/") + "/" + strings.TrimLeft(parsedPath.Path, "/")
		parsedBase.RawPath = ""
		parsedBase.RawQuery = parsedPath.RawQuery
		parsedBase.Fragment = parsedPath.Fragment
		return parsedBase.String(), nil
	}
	parsed, err := url.Parse(upstreamPath)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return "", errors.New("advanced custom upstream path must use http(s) or a relative path")
	}
	return parsed.String(), nil
}

func applyAdvancedCustomRouteAuth(requestURL string, auth *dto.AdvancedCustomRouteAuth, key string) (string, http.Header, error) {
	headers := make(http.Header)
	if auth == nil {
		headers.Set("Authorization", "Bearer "+key)
		return requestURL, headers, nil
	}
	authType := strings.TrimSpace(auth.Type)
	value := strings.ReplaceAll(auth.Value, "{api_key}", key)
	switch authType {
	case dto.AdvancedCustomAuthTypeNone:
		return requestURL, headers, nil
	case dto.AdvancedCustomAuthTypeHeader:
		name := strings.TrimSpace(auth.Name)
		if name == "" {
			return "", nil, errors.New("advanced custom auth header name is required")
		}
		headers.Set(name, value)
		return requestURL, headers, nil
	case dto.AdvancedCustomAuthTypeQuery:
		name := strings.TrimSpace(auth.Name)
		if name == "" {
			return "", nil, errors.New("advanced custom auth query name is required")
		}
		parsed, err := url.Parse(requestURL)
		if err != nil {
			return "", nil, err
		}
		query := parsed.Query()
		query.Set(name, value)
		parsed.RawQuery = query.Encode()
		return parsed.String(), headers, nil
	default:
		return "", nil, fmt.Errorf("invalid advanced custom auth type: %s", auth.Type)
	}
}

func sanitizeAdvancedCustomSecretError(err error, key, requestURL string) error {
	message := err.Error()
	for _, secret := range []string{key, requestURL} {
		if secret != "" {
			message = strings.ReplaceAll(message, secret, "[REDACTED]")
		}
	}
	if parsed, parseErr := url.Parse(requestURL); parseErr == nil {
		for _, values := range parsed.Query() {
			for _, value := range values {
				if value != "" {
					message = strings.ReplaceAll(message, value, "[REDACTED]")
				}
			}
		}
	}
	return errors.New(message)
}
