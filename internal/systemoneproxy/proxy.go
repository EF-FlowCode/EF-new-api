package systemoneproxy

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

const nativeLimit = 8 << 20
const envelopeLimit = 8*nativeLimit + 16384
const userAgent = "eflow-systemone-proxy/1"

type Proxy struct {
	config Config
	client *http.Client
	secret [32]byte
}

// Each process handles both legs. The ephemeral signature keeps unrelated Chat
// requests/responses from being mistaken for native decisions.
func New(config Config) (*Proxy, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}
	// Own the target map so configuration cannot change midway through a request.
	targets := make(map[string]Target, len(config.Targets))
	for name, target := range config.Targets {
		target.FallbackEndpoints = append([]string(nil), target.FallbackEndpoints...)
		targets[name] = target
	}
	config.Targets = targets
	// Short dial/TLS limits keep a dead primary from consuming the fallback's time.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.TLSHandshakeTimeout = 10 * time.Second
	p := &Proxy{config: config, client: &http.Client{Transport: transport, Timeout: 120 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
	if _, err := rand.Read(p.secret[:]); err != nil {
		return nil, err
	}
	return p, nil
}

type envelope struct {
	Nonce     string `json:"nonce"`
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}
type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type chatRequest struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
	Stream   bool      `json:"stream"`
}
type choice struct {
	Index        int     `json:"index"`
	Message      message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}
type chatResponse struct {
	ID      string    `json:"id"`
	Object  string    `json:"object"`
	Created int64     `json:"created"`
	Model   string    `json:"model"`
	Choices []choice  `json:"choices"`
	Usage   chatUsage `json:"usage"`
}

func (p *Proxy) signature(kind, nonce, payload string) string {
	mac := hmac.New(sha256.New, p.secret[:])
	_, _ = io.WriteString(mac, kind+"\n"+nonce+"\n"+payload)
	return hex.EncodeToString(mac.Sum(nil))
}
func (p *Proxy) pack(kind, nonce string, data []byte) string {
	e := envelope{Nonce: nonce, Payload: string(data), Signature: p.signature(kind, nonce, string(data))}
	raw, _ := json.Marshal(e)
	return string(raw)
}
func (p *Proxy) unpack(kind, content string) (envelope, error) {
	var e envelope
	if err := json.Unmarshal([]byte(content), &e); err != nil || len(e.Nonce) != 32 || len(e.Payload) > nativeLimit || !hmac.Equal([]byte(e.Signature), []byte(p.signature(kind, e.Nonce, e.Payload))) {
		return e, errors.New("invalid SystemOne transport envelope")
	}
	return e, nil
}

func readBody(body io.ReadCloser, limit int64) ([]byte, error) {
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("body exceeds size limit")
	}
	return data, nil
}
func jsonResponse(w http.ResponseWriter, status int, data []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(data)
}
func fail(w http.ResponseWriter, status int, code, msg string) {
	raw, _ := json.Marshal(map[string]any{"error": map[string]string{"type": code, "code": code, "message": msg}})
	jsonResponse(w, status, raw)
}
func requireBearer(w http.ResponseWriter, r *http.Request) bool {
	parts := strings.Fields(r.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		fail(w, 401, "authentication_error", "Bearer authentication is required")
		return false
	}
	return true
}

// post reports whether the request was handed to the connection, so callers can
// tell a pre-send connection failure from one where the origin may have run it.
func (p *Proxy) post(r *http.Request, endpoint string, body []byte) (*http.Response, bool, error) {
	var sent atomic.Bool
	ctx := httptrace.WithClientTrace(r.Context(), &httptrace.ClientTrace{WroteHeaders: func() { sent.Store(true) }})
	out, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	// Only this hop's Bearer is forwarded. Never forward cookies or user-supplied URLs.
	out.Header.Set("Authorization", r.Header.Get("Authorization"))
	out.Header.Set("Content-Type", "application/json")
	out.Header.Set("Accept", "application/json")
	// Go's default User-Agent is a common target of edge bot rules.
	out.Header.Set("User-Agent", userAgent)
	resp, err := p.client.Do(out)
	return resp, sent.Load(), err
}

var errEdgeBlocked = errors.New("blocked at the Cloudflare edge")

// blockedAtEdge matches Cloudflare challenge and edge error pages. The supplier
// origin never ran these requests, so another endpoint cannot double-bill.
// 520 and 524 are excluded: the origin may already have processed the call.
func blockedAtEdge(resp *http.Response) bool {
	if resp.Header.Get("Cf-Mitigated") != "" {
		return true
	}
	if !strings.EqualFold(resp.Header.Get("Server"), "cloudflare") || !strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/html") {
		return false
	}
	switch resp.StatusCode {
	case 403, 429, 503, 521, 522, 523, 525, 526, 530:
		return true
	}
	return false
}

// callSupplier moves to the next endpoint only when the previous attempt never
// reached the supplier origin. Business errors and post-send failures return as is.
func (p *Proxy) callSupplier(r *http.Request, name string, target Target, body []byte) (*http.Response, error) {
	var last error
	for _, endpoint := range target.endpoints() {
		if r.Context().Err() != nil {
			break
		}
		resp, sent, err := p.post(r, endpoint, body)
		var reason string
		switch {
		case err != nil && sent:
			return nil, err
		case err != nil:
			reason, last = "connection failed before sending", err
		case blockedAtEdge(resp):
			reason, last = fmt.Sprintf("Cloudflare edge returned HTTP %d", resp.StatusCode), errEdgeBlocked
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
			_ = resp.Body.Close()
		default:
			return resp, nil
		}
		host := endpoint
		if u, err := url.Parse(endpoint); err == nil {
			host = u.Host
		}
		log.Printf("SystemOne target %s: %s via %s", name, reason, host)
	}
	if last == nil {
		last = r.Context().Err()
	}
	return nil, last
}

func (p *Proxy) PublicHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" {
			fail(w, 404, "not_found", "unknown endpoint")
			return
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			fail(w, 405, "invalid_request_error", "POST is required")
			return
		}
		if !requireBearer(w, r) {
			return
		}
		raw, err := readBody(r.Body, nativeLimit)
		if err != nil {
			fail(w, 413, "invalid_request_error", "request body is unreadable or too large")
			return
		}
		var native SystemOneRequest
		if err = json.Unmarshal(raw, &native); err == nil {
			err = native.Validate()
		}
		if err != nil {
			fail(w, 400, "invalid_request_error", "invalid state, model or questions: "+err.Error())
			return
		}
		var nonce [16]byte
		if _, err = rand.Read(nonce[:]); err != nil {
			fail(w, 500, "internal_error", "could not initialize request")
			return
		}
		id := hex.EncodeToString(nonce[:])
		request := chatRequest{Model: native.Model, Messages: []message{{Role: "user", Content: p.pack("request", id, raw)}}}
		wrapped, _ := json.Marshal(request)
		resp, _, err := p.post(r, p.config.GatewayChatEndpoint, wrapped)
		if err != nil {
			fail(w, 502, "gateway_error", "Chat gateway request failed")
			return
		}
		data, err := readBody(resp.Body, envelopeLimit)
		if err != nil {
			fail(w, 502, "gateway_error", "invalid Chat gateway response")
			return
		}
		for _, header := range []string{"X-Request-Id", "Retry-After"} {
			if v := resp.Header.Get(header); v != "" {
				w.Header().Set(header, v)
			}
		}
		if resp.StatusCode != 200 {
			if json.Valid(data) {
				jsonResponse(w, resp.StatusCode, data)
			} else {
				fail(w, resp.StatusCode, "gateway_error", "Chat gateway rejected the request")
			}
			return
		}
		var result chatResponse
		if json.Unmarshal(data, &result) != nil || len(result.Choices) != 1 {
			fail(w, 502, "gateway_error", "Chat gateway did not return a SystemOne result")
			return
		}
		e, err := p.unpack("response", result.Choices[0].Message.Content)
		if err != nil || e.Nonce != id || !json.Valid([]byte(e.Payload)) {
			fail(w, 502, "gateway_error", "Chat gateway altered the SystemOne result")
			return
		}
		jsonResponse(w, 200, []byte(e.Payload))
	})
}

func (p *Proxy) UpstreamHandler() http.Handler {
	mux := http.NewServeMux()
	for name, target := range p.config.Targets {
		mux.HandleFunc("/"+name+"/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) { p.upstream(w, r, name, target) })
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) { fail(w, 404, "not_found", "unknown upstream target") })
	return mux
}

func (p *Proxy) upstream(w http.ResponseWriter, r *http.Request, name string, target Target) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		fail(w, 405, "invalid_request_error", "POST is required")
		return
	}
	if !requireBearer(w, r) {
		return
	}
	raw, err := readBody(r.Body, envelopeLimit)
	var request chatRequest
	if err != nil || json.Unmarshal(raw, &request) != nil || request.Stream || strings.TrimSpace(request.Model) == "" {
		fail(w, 400, "invalid_request_error", "invalid Chat transport request")
		return
	}
	// A configured system prompt may add a system message. Only our signed user
	// message is meaningful; no Chat-only field is sent to the native upstream.
	var packed *envelope
	for _, m := range request.Messages {
		if m.Role != "user" {
			continue
		}
		e, err := p.unpack("request", m.Content)
		if err != nil || packed != nil {
			fail(w, 400, "invalid_request_error", "expected one SystemOne transport message")
			return
		}
		packed = &e
	}
	if packed == nil {
		fail(w, 400, "invalid_request_error", "SystemOne transport message is required")
		return
	}
	var native SystemOneRequest
	if err = json.Unmarshal([]byte(packed.Payload), &native); err == nil {
		err = native.Validate()
	}
	if err != nil {
		fail(w, 400, "invalid_request_error", "invalid native request")
		return
	}
	// Use the model after the gateway's mapping, not the original wrapped name.
	native.Model = request.Model
	body, err := json.Marshal(native)
	if err != nil {
		fail(w, 400, "invalid_request_error", "invalid native request")
		return
	}
	// TypeSafe and OpenRouter Decisions share state/questions for the supported
	// subset. Their URLs are separate complete endpoints, never domain replacement.
	resp, err := p.callSupplier(r, name, target, body)
	if errors.Is(err, errEdgeBlocked) {
		fail(w, 502, "upstream_error", fmt.Sprintf("SystemOne %s upstream was blocked at the Cloudflare edge", target.Protocol))
		return
	}
	if err != nil {
		fail(w, 502, "upstream_error", "SystemOne upstream request failed")
		return
	}
	data, err := readBody(resp.Body, nativeLimit)
	if err != nil {
		fail(w, 502, "upstream_error", "SystemOne upstream response is unreadable or too large")
		return
	}
	if resp.StatusCode != 200 {
		if retry := resp.Header.Get("Retry-After"); retry != "" {
			w.Header().Set("Retry-After", retry)
		}
		// Do not echo arbitrary provider bodies: they can contain credentials.
		fail(w, resp.StatusCode, "upstream_error", fmt.Sprintf("SystemOne %s upstream returned HTTP %d", target.Protocol, resp.StatusCode))
		return
	}
	usage, err := validateResult(data, native.Questions)
	if err != nil {
		fail(w, 502, "upstream_error", err.Error())
		return
	}
	result := chatResponse{ID: "systemone-" + packed.Nonce, Object: "chat.completion", Created: time.Now().Unix(), Model: request.Model,
		Choices: []choice{{Index: 0, Message: message{Role: "assistant", Content: p.pack("response", packed.Nonce, data)}, FinishReason: "stop"}}, Usage: usage}
	wrapped, _ := json.Marshal(result)
	jsonResponse(w, 200, wrapped)
}

func validateResult(data []byte, questions map[string]SystemOneQuestion) (chatUsage, error) {
	var result struct {
		Answers map[string]json.RawMessage `json:"answers"`
		Usage   json.RawMessage            `json:"usage"`
		Error   json.RawMessage            `json:"error"`
	}
	if json.Unmarshal(data, &result) != nil || result.Answers == nil || (len(result.Error) > 0 && string(result.Error) != "null") {
		return chatUsage{}, errors.New("upstream did not return SystemOne answers")
	}
	for id, q := range questions {
		var a struct {
			Type   string   `json:"type"`
			Noul   *float64 `json:"noul"`
			Choice *string  `json:"choice"`
			Score  *float64 `json:"score"`
		}
		if json.Unmarshal(result.Answers[id], &a) != nil || a.Type != q.Type ||
			(a.Type == "noul" && (a.Noul == nil || *a.Noul < 0 || *a.Noul > 1)) ||
			(a.Type == "choice" && a.Choice == nil) || (a.Type == "score" && a.Score == nil) {
			return chatUsage{}, errors.New("upstream returned an invalid SystemOne answer")
		}
	}
	return normalizeUsage(result.Usage)
}
