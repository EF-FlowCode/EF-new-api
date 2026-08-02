package service

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/setting/system_setting"

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
	transport.ResponseHeaderTimeout = time.Duration(common.RelayResponseHeaderTimeout) * time.Second
	transport.ForceAttemptHTTP2 = true
	if common.TLSInsecureSkipVerify {
		transport.TLSClientConfig = common.InsecureTLSConfig
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
