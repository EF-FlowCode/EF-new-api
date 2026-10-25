package systemoneproxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

const testNative = `{"model":"jev-1.13","state":"refund","questions":{"q":{"type":"noul","instructions":"refund?"}}}`

func TestUsageAliases(t *testing.T) {
	for _, tc := range []struct {
		raw           string
		input, output int
	}{
		{`{"input_tokens":14,"output_tokens":3}`, 14, 3},
		{`{"input_tokens":0,"prompt_tokens":14,"output_tokens":0,"completion_tokens":3}`, 14, 3},
		{`{"input_tokens":14,"prompt_tokens":14,"output_tokens":3,"completion_tokens":3}`, 14, 3},
		{`{"input_tokens":14,"prompt_tokens":99,"output_tokens":3}`, 14, 3},
		{`{"prompt_tokens":14,"completion_tokens":0,"total_tokens":999}`, 14, 0},
	} {
		usage, err := normalizeUsage(json.RawMessage(tc.raw))
		require.NoError(t, err)
		require.Equal(t, tc.input, usage.PromptTokens)
		require.Equal(t, tc.output, usage.CompletionTokens)
		require.Equal(t, tc.input+tc.output, usage.TotalTokens)
	}
	for _, raw := range []string{`{}`, `null`, `[]`, `{"input_tokens":14}`, `{"input_tokens":0,"output_tokens":3}`, `{"input_tokens":14,"output_tokens":-1}`, `{"input_tokens":"14"}`, `{"input_tokens":1.2}`, `{"input_tokens":9223372036854775807}`} {
		_, err := normalizeUsage(json.RawMessage(raw))
		require.Error(t, err, raw)
	}
}

func TestCompleteEndpoints(t *testing.T) {
	config := Config{GatewayChatEndpoint: "http://gateway/v1/chat/completions", Targets: map[string]Target{"typesafe": {Protocol: "typesafe", Endpoint: "https://api.typesafe.ai/v1/systemone"}, "openrouter": {Protocol: "openrouter", Endpoint: "https://openrouter.ai/api/alpha/decisions"}}}
	require.NoError(t, config.Validate())
	for _, endpoint := range []string{"/v1/systemone", "file:///key", "https://user:password@host/v1/systemone", "https://host/path#fragment"} {
		config.Targets["typesafe"] = Target{Protocol: "typesafe", Endpoint: endpoint}
		require.Error(t, config.Validate())
	}
}

func TestProxyRejectsUnrelatedOrAlteredChat(t *testing.T) {
	calls := 0
	supplier := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
	defer supplier.Close()
	p, err := New(Config{GatewayChatEndpoint: "http://gateway/v1/chat/completions", Targets: map[string]Target{"test": {Protocol: "typesafe", Endpoint: supplier.URL + "/native"}}})
	require.NoError(t, err)
	for _, content := range []string{"hello", p.pack("request", strings.Repeat("a", 32), []byte(testNative)) + "tampered", p.pack("response", strings.Repeat("a", 32), []byte(testNative))} {
		raw, _ := json.Marshal(chatRequest{Model: "jev-1.13", Messages: []message{{Role: "user", Content: content}}})
		r := httptest.NewRequest("POST", "/test/v1/chat/completions", strings.NewReader(string(raw)))
		r.Header.Set("Authorization", "Bearer upstreamkey")
		w := httptest.NewRecorder()
		p.UpstreamHandler().ServeHTTP(w, r)
		require.Equal(t, 400, w.Code)
	}
	require.Zero(t, calls)
}

func TestProxyDoesNotFollowCredentialRedirects(t *testing.T) {
	for _, leg := range []string{"gateway", "supplier"} {
		t.Run(leg, func(t *testing.T) {
			leaks := 0
			other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaks++ }))
			defer other.Close()
			redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, other.URL, 307) }))
			defer redirect.Close()
			p, err := New(Config{GatewayChatEndpoint: redirect.URL, Targets: map[string]Target{"test": {Protocol: "typesafe", Endpoint: redirect.URL}}})
			require.NoError(t, err)
			handler, path, body := p.PublicHandler(), "/v1/systemone", testNative
			if leg == "supplier" {
				handler, path = p.UpstreamHandler(), "/test/v1/chat/completions"
				raw, _ := json.Marshal(chatRequest{Model: "jev-1.13", Messages: []message{{Role: "user", Content: p.pack("request", strings.Repeat("a", 32), []byte(testNative))}}})
				body = string(raw)
			}
			r := httptest.NewRequest("POST", path, strings.NewReader(body))
			r.Header.Set("Authorization", "Bearer testkey")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			require.Equal(t, 307, w.Code)
			require.Zero(t, leaks)
			require.Empty(t, w.Header().Get("Location"))
		})
	}
}

func TestProxyRejectsWrongGatewayResult(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"choices":[{"message":{"role":"assistant","content":"ordinary chat response"}}]}`)
	}))
	defer gateway.Close()
	p, err := New(Config{GatewayChatEndpoint: gateway.URL, Targets: map[string]Target{"test": {Protocol: "typesafe", Endpoint: "https://api.typesafe.ai/v1/systemone"}}})
	require.NoError(t, err)
	r := httptest.NewRequest("POST", "/v1/systemone", strings.NewReader(testNative))
	r.Header.Set("Authorization", "Bearer gatewaykey")
	w := httptest.NewRecorder()
	p.PublicHandler().ServeHTTP(w, r)
	require.Equal(t, 502, w.Code)
}

const testAnswers = `{"answers":{"q":{"type":"noul","noul":0.99}},"usage":{"prompt_tokens":14,"completion_tokens":0}}`

func callUpstream(t *testing.T, p *Proxy) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(chatRequest{Model: "jev-1.13", Messages: []message{{Role: "user", Content: p.pack("request", strings.Repeat("a", 32), []byte(testNative))}}})
	r := httptest.NewRequest("POST", "/test/v1/chat/completions", strings.NewReader(string(raw)))
	r.Header.Set("Authorization", "Bearer upstreamkey")
	w := httptest.NewRecorder()
	p.UpstreamHandler().ServeHTTP(w, r)
	return w
}

func edgeChallenge(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Server", "cloudflare")
	w.Header().Set("Content-Type", "text/html; charset=UTF-8")
	w.Header().Set("Cf-Mitigated", "challenge")
	w.WriteHeader(403)
	_, _ = io.WriteString(w, "<!DOCTYPE html><title>Just a moment...</title>")
}

func TestFallbackEndpointsValidation(t *testing.T) {
	base := Config{GatewayChatEndpoint: "http://gateway/v1/chat/completions"}
	for _, tc := range []struct {
		fallbacks []string
		ok        bool
	}{
		{[]string{"https://b.example/v1/systemone"}, true},
		{[]string{"https://a.example/v1/systemone"}, false},
		{[]string{"/v1/systemone"}, false},
		{[]string{"https://b/1", "https://b/2", "https://b/3", "https://b/4"}, false},
	} {
		base.Targets = map[string]Target{"t": {Protocol: "typesafe", Endpoint: "https://a.example/v1/systemone", FallbackEndpoints: tc.fallbacks}}
		if tc.ok {
			require.NoError(t, base.Validate(), tc.fallbacks)
		} else {
			require.Error(t, base.Validate(), tc.fallbacks)
		}
	}
}

func TestFallbackOnlyBeforeOrigin(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	for _, tc := range []struct {
		name          string
		primary       http.HandlerFunc
		closedPrimary bool
		status        int
		fallbackCalls int
	}{
		{name: "cloudflare challenge", primary: edgeChallenge, status: 200, fallbackCalls: 1},
		{name: "edge origin unreachable", primary: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Server", "cloudflare")
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(522)
		}, status: 200, fallbackCalls: 1},
		{name: "connection refused", closedPrimary: true, status: 200, fallbackCalls: 1},
		{name: "supplier business error", primary: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Server", "cloudflare")
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"code":400,"msg":"bad request"}`)
		}, status: 400},
		{name: "edge timeout after origin received", primary: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Server", "cloudflare")
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(524)
		}, status: 524},
		{name: "plain origin html 403", primary: func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Server", "nginx")
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(403)
		}, status: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var agents, auths []string
			fallbackCalls := 0
			fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fallbackCalls++
				agents = append(agents, r.UserAgent())
				auths = append(auths, r.Header.Get("Authorization"))
				_, _ = io.WriteString(w, testAnswers)
			}))
			defer fallback.Close()
			primaryURL := closedURL
			if !tc.closedPrimary {
				primary := httptest.NewServer(tc.primary)
				defer primary.Close()
				primaryURL = primary.URL
			}
			p, err := New(Config{GatewayChatEndpoint: "http://gateway/v1/chat/completions", Targets: map[string]Target{"test": {Protocol: "typesafe", Endpoint: primaryURL + "/v1/systemone", FallbackEndpoints: []string{fallback.URL + "/v1/systemone"}}}})
			require.NoError(t, err)
			w := callUpstream(t, p)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.fallbackCalls, fallbackCalls)
			for _, agent := range agents {
				require.Equal(t, userAgent, agent)
			}
			for _, auth := range auths {
				require.Equal(t, "Bearer upstreamkey", auth)
			}
		})
	}
}

func TestAllEndpointsBlockedAtEdge(t *testing.T) {
	calls := 0
	blocked := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++; edgeChallenge(w, r) }))
	defer blocked.Close()
	p, err := New(Config{GatewayChatEndpoint: "http://gateway/v1/chat/completions", Targets: map[string]Target{"test": {Protocol: "typesafe", Endpoint: blocked.URL + "/a", FallbackEndpoints: []string{blocked.URL + "/b"}}}})
	require.NoError(t, err)
	w := callUpstream(t, p)
	require.Equal(t, 502, w.Code)
	require.Contains(t, w.Body.String(), "blocked at the Cloudflare edge")
	require.Equal(t, 2, calls)
}
