package knowledge

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const MaxInputBytes = 8 * 1024 * 1024
const MaxBatchRequests = 2000
const MaxLineBytes = 6 * 1024 * 1024

type BatchLine struct {
	CustomID string `json:"custom_id"`
	Method   string `json:"method"`
	URL      string `json:"url"`
	Body     struct {
		Model          string          `json:"model"`
		Input          json.RawMessage `json:"input"`
		Dimensions     *int            `json:"dimensions,omitempty"`
		EncodingFormat string          `json:"encoding_format,omitempty"`
		Instruct       string          `json:"instruct,omitempty"`
	} `json:"body"`
}

type BatchManifest struct {
	Model     string
	Count     int
	TextBytes int
	IDs       map[string]bool
	// Canonical is the input rewritten from the validated fields only; it is
	// what gets stored and uploaded, so the provider executes exactly what was
	// priced.
	Canonical []byte
}

func ValidateBatch(content []byte) (BatchManifest, error) {
	m := BatchManifest{IDs: map[string]bool{}}
	if len(content) > MaxInputBytes || !utf8.Valid(content) {
		return m, fmt.Errorf("batch input must be UTF-8 JSONL, at most 8 MiB")
	}
	scanner := bufio.NewScanner(bytes.NewReader(content))
	scanner.Buffer(make([]byte, 4096), MaxLineBytes)
	for scanner.Scan() {
		line, body, err := decodeBatchLine(scanner.Bytes())
		if err != nil {
			return m, fmt.Errorf("invalid JSONL line %d: %w", m.Count+1, err)
		}
		if line.CustomID == "" || len(line.CustomID) > 128 || m.IDs[line.CustomID] {
			return m, fmt.Errorf("custom_id must be unique and 1-128 bytes")
		}
		if line.Method != "POST" || line.URL != "/v1/embeddings" {
			return m, fmt.Errorf("only POST /v1/embeddings is supported by knowledge Batch File")
		}
		if !SupportsAliBatch(line.Body.Model) || (m.Model != "" && m.Model != line.Body.Model) {
			return m, fmt.Errorf("batch must contain one supported text embedding model")
		}
		if line.Body.Dimensions != nil && *line.Body.Dimensions <= 0 {
			return m, fmt.Errorf("dimensions must be positive")
		}
		if line.Body.EncodingFormat != "" && line.Body.EncodingFormat != "float" && line.Body.EncodingFormat != "base64" {
			return m, fmt.Errorf("encoding_format must be float or base64")
		}
		var texts []string
		var single string
		if json.Unmarshal(line.Body.Input, &single) == nil {
			texts = []string{single}
		} else if json.Unmarshal(line.Body.Input, &texts) != nil {
			return m, fmt.Errorf("input must be text or a text array")
		}
		maxRows := 10
		if line.Body.Model == "text-embedding-v1" || line.Body.Model == "text-embedding-v2" {
			maxRows = 25
		}
		if line.Body.Model == "qwen3.7-text-embedding" || line.Body.Model == "qwen3.7-text-embedding-flash" {
			maxRows = 20
		}
		if len(texts) == 0 || len(texts) > maxRows {
			return m, fmt.Errorf("input array exceeds model limit")
		}
		for _, text := range texts {
			if text == "" {
				return m, fmt.Errorf("empty input text")
			}
			m.TextBytes += len(text)
		}
		// Query instructions may be billed once per input text.
		m.TextBytes += len(line.Body.Instruct) * len(texts)
		m.Model = line.Body.Model
		m.Count++
		m.IDs[line.CustomID] = true
		canonical, err := json.Marshal(struct {
			CustomID string          `json:"custom_id"`
			Method   string          `json:"method"`
			URL      string          `json:"url"`
			Body     json.RawMessage `json:"body"`
		}{line.CustomID, line.Method, line.URL, body})
		if err != nil {
			return m, err
		}
		m.Canonical = append(append(m.Canonical, canonical...), '\n')
		if m.Count > MaxBatchRequests {
			return m, fmt.Errorf("batch exceeds 2000 requests")
		}
	}
	if err := scanner.Err(); err != nil {
		return m, err
	}
	if m.Count == 0 {
		return m, fmt.Errorf("empty batch")
	}
	return m, nil
}

var batchLineKeys = []string{"custom_id", "method", "url", "body"}
var batchBodyKeys = []string{"model", "input", "dimensions", "encoding_format", "instruct"}

// decodeBatchLine parses one request line the way the provider does. Go's
// decoder matches keys case-insensitively and lets a later duplicate win, so
// a line carrying both "body" and "Body" was priced from one object while the
// provider executed the other. Duplicates, unknown top-level keys and case
// variants of the priced body fields are therefore rejected outright.
func decodeBatchLine(raw []byte) (BatchLine, json.RawMessage, error) {
	var line BatchLine
	top, err := decodeStrictObject(raw, batchLineKeys, true)
	if err != nil {
		return line, nil, err
	}
	body, ok := top["body"]
	if !ok {
		return line, nil, fmt.Errorf("body is required")
	}
	if _, err = decodeStrictObject(body, batchBodyKeys, false); err != nil {
		return line, nil, fmt.Errorf("body: %w", err)
	}
	if err = json.Unmarshal(raw, &line); err != nil {
		return line, nil, err
	}
	return line, body, nil
}

// decodeStrictObject reads one JSON object level. Keys must be unique; a key
// that differs from a known key only by case is rejected, and with onlyKnown
// every key must be one of known.
func decodeStrictObject(raw []byte, known []string, onlyKnown bool) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if token, err := decoder.Token(); err != nil || token != json.Delim('{') {
		return nil, fmt.Errorf("expected a JSON object")
	}
	fields := map[string]json.RawMessage{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		key := token.(string)
		if _, duplicated := fields[key]; duplicated {
			return nil, fmt.Errorf("duplicate key %q", key)
		}
		isKnown := false
		for _, name := range known {
			if key == name {
				isKnown = true
			} else if strings.EqualFold(key, name) {
				return nil, fmt.Errorf("key %q must be written as %q", key, name)
			}
		}
		if onlyKnown && !isKnown {
			return nil, fmt.Errorf("unsupported key %q", key)
		}
		var value json.RawMessage
		if err = decoder.Decode(&value); err != nil {
			return nil, err
		}
		fields[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if decoder.More() {
		return nil, fmt.Errorf("trailing data after object")
	}
	return fields, nil
}

// ReadBatchUsage rejects duplicates/missing usage instead of silently
// undercharging a partially downloaded or corrupt result file.
func ReadBatchUsage(r io.Reader, ids map[string]bool) (tokens int, completed int, err error) {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 4096), MaxLineBytes)
	seen := map[string]bool{}
	for s.Scan() {
		var line struct {
			CustomID string          `json:"custom_id"`
			Error    json.RawMessage `json:"error"`
			Response *struct {
				Status int             `json:"status_code"`
				Body   json.RawMessage `json:"body"`
			} `json:"response"`
		}
		if e := json.Unmarshal(s.Bytes(), &line); e != nil {
			return 0, 0, e
		}
		if !ids[line.CustomID] || seen[line.CustomID] {
			return 0, 0, fmt.Errorf("unknown or repeated batch custom_id")
		}
		seen[line.CustomID] = true
		if line.Response == nil || line.Response.Status < 200 || line.Response.Status >= 300 || (len(line.Error) > 0 && string(line.Error) != "null") {
			continue
		}
		u, e := ParseUsage(line.Response.Body, 0, false)
		if e != nil || u.Estimated || u.Output != 0 {
			return 0, 0, fmt.Errorf("successful batch result has invalid or missing usage")
		}
		if u.Input > int(^uint(0)>>1)-tokens {
			return 0, 0, fmt.Errorf("batch token overflow")
		}
		tokens += u.Input
		completed++
	}
	return tokens, completed, s.Err()
}
