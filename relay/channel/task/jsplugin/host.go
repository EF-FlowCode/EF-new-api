// Package jsplugin implements the data-only Task Plugin host contract. It is
// independent of relay dispatch, model persistence and billing.
package jsplugin

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	runtime "eflow.local/new-api/pkg/jsplugin"
)

const MaxPayloadBytes = 1 << 20
const MaxInlineVideoBytes = 64 << 20

type Identity struct {
	Key        string `json:"key"`
	Version    string `json:"version"`
	SourceHash string `json:"source_hash"`
}

// Handle pins compiled source for the entire operation, including its response.
// Registry activation never mutates an existing handle.
type Handle struct {
	plugin   *runtime.LoadedPlugin
	identity Identity
}

func Compile(source string) (*Handle, error) {
	if len(source) == 0 || len(source) > MaxPayloadBytes {
		return nil, errors.New("invalid plugin source size")
	}
	p, err := runtime.CompilePlugin(source, runtime.Options{Timeout: time.Second, Concurrency: 4})
	if err != nil {
		return nil, err
	}
	return &Handle{plugin: p, identity: Identity{p.Meta.Key, p.Meta.Version, fmt.Sprintf("%x", sha256.Sum256([]byte(source)))}}, nil
}
func (h *Handle) Identity() Identity { return h.identity }
func (h *Handle) Meta() runtime.Meta { // Return a deep copy; callers cannot alter routing/auth declarations.
	var meta runtime.Meta
	raw, _ := json.Marshal(h.plugin.Meta)
	_ = json.Unmarshal(raw, &meta)
	return meta
}
func (h *Handle) Invoke(ctx context.Context, hook string, args ...any) (any, error) {
	return h.invoke(ctx, nil, hook, args...)
}

// Native only exposes a hook named by this handle's validated route manifest.
func (h *Handle) Native(ctx context.Context, hook string, args ...any) (any, error) {
	for _, r := range h.plugin.Meta.Routes {
		if hook != "" && (hook == r.Decode || hook == r.Render) {
			return h.invoke(ctx, []string{hook}, "native", args...)
		}
	}
	return nil, errors.New("undeclared native hook")
}

// Protocol calls only the reviewed protocol surface on this fixed source.
func (h *Handle) Protocol(ctx context.Context, protocol, hook string, args ...any) (any, error) {
	if protocol != "openai_responses" && protocol != "openai_video" {
		return nil, errors.New("unsupported public protocol")
	}
	switch hook {
	case "decodeRequest", "render", "renderFinal", "renderEvents":
	default:
		return nil, errors.New("unsupported protocol hook")
	}
	return h.invoke(ctx, []string{protocol, hook}, "protocols", args...)
}

func (h *Handle) invoke(ctx context.Context, path []string, hook string, args ...any) (any, error) {
	if len(args) > 16 {
		return nil, errors.New("too many hook arguments")
	}
	raw, err := json.Marshal(args)
	limit := MaxPayloadBytes
	if h.identity.Key == "vertex-ai" {
		limit = MaxInlineVideoBytes
	}
	if err != nil || len(raw) > limit {
		return nil, errors.New("hook input exceeds limit or is not JSON")
	}
	var copied []any
	if err = json.Unmarshal(raw, &copied); err != nil {
		return nil, err
	}
	var value any
	if path == nil {
		value, err = h.plugin.Engine.Call(ctx, hook, copied...)
	} else {
		value, err = h.plugin.Engine.CallPath(ctx, hook, path, copied...)
	}
	if err != nil {
		return nil, err
	}
	raw, err = json.Marshal(value)
	if err != nil || len(raw) > limit {
		return nil, errors.New("hook output exceeds limit or is not JSON")
	}
	var out any
	err = json.Unmarshal(raw, &out)
	return out, err
}

type Part struct {
	Name     string `json:"name"`
	Value    any    `json:"value"`
	FileRef  string `json:"fileRef"`
	Filename string `json:"filename"`
}
type Descriptor struct {
	URL            string            `json:"url"`
	Method         string            `json:"method"`
	Headers        map[string]string `json:"headers"`
	Body           any               `json:"body,omitempty"`
	BodyType       string            `json:"bodyType,omitempty"`
	Parts          []Part            `json:"parts,omitempty"`
	ResponseType   string            `json:"responseType,omitempty"`
	Credentialless bool              `json:"credentialless,omitempty"`
	Action         string            `json:"action,omitempty"`
	Model          string            `json:"model,omitempty"`
	RewriteModel   string            `json:"rewriteModel,omitempty"`
}

func (h *Handle) Build(ctx context.Context, hook string, input map[string]any, extra ...any) (Descriptor, error) {
	var d Descriptor
	switch hook {
	case "buildSubmitRequest", "buildQueryRequest", "buildBatchQueryRequest", "buildContentRequest":
	default:
		return d, errors.New("not a request builder")
	}
	args := append([]any{input}, extra...)
	v, err := h.Invoke(ctx, hook, args...)
	if err != nil {
		return d, err
	}
	raw, _ := json.Marshal(v)
	if err = json.Unmarshal(raw, &d); err != nil {
		return d, err
	}
	base, _ := input["baseUrl"].(string)
	if err = h.ValidateDescriptor(d, base); err != nil {
		return d, err
	}
	return d, nil
}

func (h *Handle) ValidateDescriptor(d Descriptor, base string) error {
	u, err := parseURL(d.URL)
	if err != nil {
		return err
	}
	b, err := parseURL(base)
	if err != nil {
		return errors.New("invalid channel base URL")
	}
	allowed := strings.EqualFold(u.Host, b.Host) && u.Scheme == b.Scheme
	if d.Credentialless {
		if len(d.Headers) != 0 {
			return errors.New("credentialless content cannot contain headers")
		}
		allowed = true // The executor still applies the host SSRF policy.
	}
	for _, host := range h.plugin.Meta.AllowedHosts {
		name := strings.ToLower(u.Hostname())
		host = strings.ToLower(host)
		if name == host || (strings.HasPrefix(host, "*.") && strings.HasSuffix(name, host[1:]) && name != host[2:]) {
			allowed = true
		}
	}
	if !allowed {
		return errors.New("plugin request origin is not allowed")
	}
	switch d.Method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD":
	default:
		return errors.New("invalid request method")
	}
	if d.BodyType != "" && d.BodyType != "json" && d.BodyType != "multipart" {
		return errors.New("unsupported body type")
	}
	if d.ResponseType != "" && d.ResponseType != "json" && d.ResponseType != "sse" {
		return errors.New("unsupported response type")
	}
	for key, value := range d.Headers {
		if strings.ContainsAny(value, "\r\n\x00") || !headerName(key) {
			return errors.New("invalid request header")
		}
		switch strings.ToLower(key) {
		case "host", "content-length", "transfer-encoding", "connection", "proxy-authorization", "proxy-connection", "upgrade", "trailer":
			return errors.New("plugin cannot set transport headers")
		}
	}
	return nil
}
func headerName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("!#$%&'*+-.^_`|~", r)) {
			return false
		}
	}
	return true
}
func parseURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u == nil || u.Hostname() == "" || u.User != nil || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return nil, errors.New("invalid plugin HTTP URL")
	}
	return u, nil
}

type File struct {
	Name        string
	ContentType string
	Data        []byte
}

func (d Descriptor) Request(ctx context.Context, files map[string]File) (*http.Request, error) {
	var body []byte
	contentType := ""
	if d.BodyType == "multipart" {
		if len(d.Parts) > 64 {
			return nil, errors.New("too many multipart parts")
		}
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		for _, p := range d.Parts {
			if p.Name == "" || strings.ContainsAny(p.Name+p.Filename, "\r\n\x00") {
				return nil, errors.New("invalid multipart name")
			}
			if p.FileRef != "" {
				f, ok := files[p.FileRef]
				if !ok {
					return nil, errors.New("unknown file reference")
				}
				if len(f.Data)+buf.Len() > MaxPayloadBytes {
					return nil, errors.New("multipart exceeds limit")
				}
				name := p.Filename
				if name == "" {
					name = f.Name
				}
				if strings.ContainsAny(name, "\r\n\x00") {
					return nil, errors.New("invalid file name")
				}
				w, err := writer.CreateFormFile(p.Name, name)
				if err != nil {
					return nil, err
				}
				if _, err = w.Write(f.Data); err != nil {
					return nil, err
				}
			} else {
				value, ok := p.Value.(string)
				if !ok {
					raw, err := json.Marshal(p.Value)
					if err != nil {
						return nil, err
					}
					value = string(raw)
				}
				if len(value)+buf.Len() > MaxPayloadBytes {
					return nil, errors.New("multipart exceeds limit")
				}
				if err := writer.WriteField(p.Name, value); err != nil {
					return nil, err
				}
			}
		}
		if err := writer.Close(); err != nil {
			return nil, err
		}
		body = buf.Bytes()
		contentType = writer.FormDataContentType()
	} else if value, ok := d.Body.(string); ok {
		// Signed providers such as Jimeng sign the exact JSON bytes. Encoding
		// that string a second time invalidates both its shape and signature.
		if !json.Valid([]byte(value)) {
			return nil, errors.New("raw request body is not JSON")
		}
		body = []byte(value)
		contentType = "application/json"
	} else if d.Body != nil {
		raw, err := json.Marshal(d.Body)
		if err != nil || len(raw) > MaxPayloadBytes {
			return nil, errors.New("JSON body exceeds limit")
		}
		var v any
		_ = json.Unmarshal(raw, &v)
		remaining := MaxPayloadBytes
		v, err = expandFiles(v, files, &remaining)
		if err != nil {
			return nil, err
		}
		body, err = json.Marshal(v)
		if err != nil {
			return nil, err
		}
		contentType = "application/json"
	}
	if len(body) > MaxPayloadBytes {
		return nil, errors.New("request body exceeds limit")
	}
	req, err := http.NewRequestWithContext(ctx, d.Method, d.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	for k, v := range d.Headers {
		req.Header.Set(k, v)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req, nil
}
func expandFiles(v any, files map[string]File, remaining *int) (any, error) {
	switch obj := v.(type) {
	case map[string]any:
		if ref, ok := obj["__fileRef"].(string); ok {
			f, found := files[ref]
			if !found {
				return nil, errors.New("unknown inline file reference")
			}
			if n, ok := obj["maxBytes"].(float64); ok && float64(len(f.Data)) > n {
				return nil, errors.New("inline file exceeds plugin limit")
			}
			if len(f.Data) > *remaining {
				return nil, errors.New("inline files exceed host limit")
			}
			*remaining -= len(f.Data)
			encoded := base64.StdEncoding.EncodeToString(f.Data)
			switch obj["encoding"] {
			case "base64":
				return encoded, nil
			case "dataUrl":
				if f.ContentType == "" || strings.ContainsAny(f.ContentType, "\r\n;,") {
					return nil, errors.New("invalid inline file MIME type")
				}
				return "data:" + f.ContentType + ";base64," + encoded, nil
			default:
				return nil, errors.New("unsupported file encoding")
			}
		}
		for k, x := range obj {
			value, err := expandFiles(x, files, remaining)
			if err != nil {
				return nil, err
			}
			obj[k] = value
		}
	case []any:
		for i, x := range obj {
			value, err := expandFiles(x, files, remaining)
			if err != nil {
				return nil, err
			}
			obj[i] = value
		}
	}
	return v, nil
}

type Response struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    any               `json:"body"`
}

func (h *Handle) ParseSubmit(ctx context.Context, input map[string]any, response Response) (map[string]any, error) {
	v, err := h.Invoke(ctx, "parseSubmitResponse", input, response)
	if err != nil {
		return nil, err
	}
	result, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("submit result must be an object")
	}
	id, ok := result["taskId"].(string)
	if !ok || strings.TrimSpace(id) == "" || len(id) > 4096 {
		return nil, errors.New("invalid upstream task identity")
	}
	if immediate, ok := result["immediate"]; ok {
		if _, err := validateTaskResult(immediate); err != nil {
			return nil, err
		}
	}
	return result, nil
}
func (h *Handle) ParseTask(ctx context.Context, input map[string]any, response Response) (map[string]any, error) {
	v, err := h.Invoke(ctx, "parseTaskResult", input, response.Body, response)
	if err != nil {
		return nil, err
	}
	return validateTaskResult(v)
}
func (h *Handle) ParseBatch(ctx context.Context, input map[string]any, response Response) ([]map[string]any, error) {
	v, err := h.Invoke(ctx, "parseBatchResult", input, response.Body, response)
	if err != nil {
		return nil, err
	}
	rows, ok := v.([]any)
	if !ok || len(rows) > 100 {
		return nil, errors.New("invalid batch task result")
	}
	results := make([]map[string]any, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		r, err := validateTaskResult(row)
		if err != nil {
			return nil, err
		}
		id, ok := r["taskId"].(string)
		if !ok || id == "" || seen[id] {
			return nil, errors.New("invalid or duplicate batch task identity")
		}
		seen[id] = true
		results = append(results, r)
	}
	return results, nil
}
func validateTaskResult(v any) (map[string]any, error) {
	r, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("task result must be an object")
	}
	switch r["status"] {
	case "NOT_START", "SUBMITTED", "QUEUED", "IN_PROGRESS", "SUCCESS", "FAILURE", "UNKNOWN":
	default:
		return nil, errors.New("invalid task status")
	}
	for _, k := range []string{"url", "remoteUrl", "reason", "progress"} {
		if val, exists := r[k]; exists && val != nil {
			if _, ok := val.(string); !ok {
				return nil, errors.New("invalid task result field type")
			}
		}
	}
	return r, nil
}

func DecodeResponse(status int, headers http.Header, body []byte) (Response, error) {
	return decodeResponseLimit(status, headers, body, MaxPayloadBytes)
}

func decodeResponseLimit(status int, headers http.Header, body []byte, limit int) (Response, error) {
	r := Response{Status: status, Headers: map[string]string{}}
	if len(body) > limit {
		return r, errors.New("response exceeds limit")
	}
	for k, v := range headers {
		if len(v) > 0 {
			r.Headers[k] = v[0]
		}
	}
	if err := json.Unmarshal(body, &r.Body); err != nil {
		return r, errors.New("response is not JSON")
	}
	return r, nil
}

// Executor requires the application's SSRF-protected transport and URL policy.
// It is not constructed or called by shadow observation. Redirects are denied
// so plugin credentials cannot leak through a redirect to a different origin.
type Executor struct {
	Client      *http.Client
	ValidateURL func(string) error
}

func (e Executor) Execute(ctx context.Context, h *Handle, base string, d Descriptor, files map[string]File) (Response, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	resp, err := e.Open(ctx, h, base, d, files)
	if err != nil {
		return Response{}, err
	}
	defer resp.Body.Close()
	limit := MaxPayloadBytes
	if h.identity.Key == "vertex-ai" {
		limit = MaxInlineVideoBytes
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit+1)))
	if err != nil {
		return Response{}, err
	}
	return decodeResponseLimit(resp.StatusCode, resp.Header, body, limit)
}

// Open streams authenticated content without treating binary data as JSON.
// The caller owns the response body and supplies the request deadline.
func (e Executor) Open(ctx context.Context, h *Handle, base string, d Descriptor, files map[string]File) (*http.Response, error) {
	if e.Client == nil || e.ValidateURL == nil {
		return nil, errors.New("HTTP policy is required")
	}
	if d.ResponseType == "sse" {
		return nil, errors.New("SSE transport requires a streaming host")
	}
	if err := h.ValidateDescriptor(d, base); err != nil {
		return nil, err
	}
	if err := e.ValidateURL(d.URL); err != nil {
		return nil, err
	}
	req, err := d.Request(ctx, files)
	if err != nil {
		return nil, err
	}
	client := *e.Client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		resp.Body.Close()
		return nil, errors.New("plugin redirects are disabled")
	}
	return resp, nil
}

// Usage returns validated facts, never prices or quota changes.
func (h *Handle) Usage(ctx context.Context, model, hook string, args ...any) (map[string]any, error) {
	if hook != "extractUsage" && hook != "extractUsageOnComplete" {
		return nil, errors.New("invalid usage hook")
	}
	has, err := h.plugin.Engine.HasExport(ctx, hook)
	if err != nil {
		return nil, err
	}
	if !has {
		return nil, nil
	}
	v, err := h.Invoke(ctx, hook, args...)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return nil, nil
	}
	facts, ok := v.(map[string]any)
	if !ok {
		return nil, errors.New("usage must be an object")
	}
	schema, _ := h.plugin.Meta.UsageForModel(model)
	if err := runtime.ValidateUsageFacts(schema, facts); err != nil {
		return nil, err
	}
	return facts, nil
}

// DifferencePaths intentionally exports neither values nor attacker-controlled
// object keys. Only stable field names are retained in diagnostic reports.
func DifferencePaths(a, b any) []string {
	var paths []string
	var walk func(any, any, string, int)
	walk = func(x, y any, path string, depth int) {
		if len(paths) >= 32 {
			return
		}
		xb, _ := json.Marshal(x)
		yb, _ := json.Marshal(y)
		if bytes.Equal(xb, yb) {
			return
		}
		xm, xok := x.(map[string]any)
		ym, yok := y.(map[string]any)
		if xok && yok && depth < 6 {
			keys := map[string]bool{}
			for k := range xm {
				keys[k] = true
			}
			for k := range ym {
				keys[k] = true
			}
			ordered := make([]string, 0, len(keys))
			for k := range keys {
				ordered = append(ordered, k)
			}
			sort.Strings(ordered)
			for _, k := range ordered {
				label := "*"
				if safeField(k) {
					label = k
				}
				walk(xm[k], ym[k], path+"/"+label, depth+1)
			}
			return
		}
		paths = append(paths, path)
	}
	walk(a, b, "", 0)
	return paths
}
func safeField(k string) bool {
	switch k {
	case "model", "input", "parameters", "prompt", "img_url", "first_frame_url", "last_frame_url", "audio_url", "negative_prompt", "template", "resolution", "size", "duration", "prompt_extend", "watermark", "audio", "seed", "taskId", "status", "url", "reason", "seconds", "progress", "method", "body", "headers", "action":
		return true
	}
	return false
}
