// Package systemoneproxy adapts native decisions through an unmodified Chat gateway.
// Production code depends only on the standard library, not on the gateway runtime.
package systemoneproxy

import (
	"fmt"
	"net/url"
	"regexp"
)

// maxFallbackEndpoints bounds how many supplier calls one decision can attempt.
const maxFallbackEndpoints = 3

type Target struct {
	Protocol string `json:"protocol"`
	Endpoint string `json:"endpoint"`
	// FallbackEndpoints are complete endpoints of the same supplier and API key,
	// tried in order only when the previous attempt never reached the origin.
	FallbackEndpoints []string `json:"fallback_endpoints,omitempty"`
}

func (t Target) endpoints() []string {
	return append([]string{t.Endpoint}, t.FallbackEndpoints...)
}

type Config struct {
	PublicListen        string            `json:"public_listen"`
	UpstreamListen      string            `json:"upstream_listen"`
	GatewayChatEndpoint string            `json:"gateway_chat_endpoint"`
	Targets             map[string]Target `json:"targets"`
}

func validateEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Fragment != "" {
		return fmt.Errorf("endpoint must be a complete HTTP(S) URL without credentials or fragment")
	}
	return nil
}

func (c Config) Validate() error {
	if err := validateEndpoint(c.GatewayChatEndpoint); err != nil {
		return fmt.Errorf("gateway_chat_endpoint: %w", err)
	}
	if len(c.Targets) == 0 {
		return fmt.Errorf("at least one upstream target is required")
	}
	for name, target := range c.Targets {
		if !regexp.MustCompile(`^[a-zA-Z0-9_-]+$`).MatchString(name) {
			return fmt.Errorf("invalid upstream target name")
		}
		if target.Protocol != "typesafe" && target.Protocol != "openrouter" {
			return fmt.Errorf("target %s: protocol must be typesafe or openrouter", name)
		}
		if len(target.FallbackEndpoints) > maxFallbackEndpoints {
			return fmt.Errorf("target %s: at most %d fallback endpoints are allowed", name, maxFallbackEndpoints)
		}
		seen := make(map[string]bool)
		for _, endpoint := range target.endpoints() {
			if err := validateEndpoint(endpoint); err != nil {
				return fmt.Errorf("target %s: %w", name, err)
			}
			if seen[endpoint] {
				return fmt.Errorf("target %s: duplicate endpoint", name)
			}
			seen[endpoint] = true
		}
	}
	return nil
}
