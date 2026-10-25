package service

import (
	"errors"
	"net/http"
	"net/url"

	host "eflow.local/new-api/relay/channel/task/jsplugin"
)

// NewTaskPluginHTTPExecutor provides the existing SSRF policy to the standalone
// host and bound plugin submit/poll paths. Shadow workers never create an
// executor. Production bindings remain disabled until explicitly configured.
func NewTaskPluginHTTPExecutor() host.Executor {
	return host.Executor{Client: GetSSRFProtectedHTTPClient(), ValidateURL: ValidateSSRFProtectedFetchURL}
}

func NewTaskPluginHTTPExecutorForProxy(proxy string) (host.Executor, error) {
	if proxy == "" {
		return NewTaskPluginHTTPExecutor(), nil
	}
	u, err := url.Parse(proxy)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5" && u.Scheme != "socks5h") {
		return host.Executor{}, errors.New("invalid plugin channel proxy")
	}
	return host.Executor{Client: newProtectedFetchHTTPClientWithProxy(nil, nil, nil, http.ProxyURL(u)), ValidateURL: ValidateSSRFProtectedFetchURL}, nil
}
