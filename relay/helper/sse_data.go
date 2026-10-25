package helper

import (
	"encoding/json"
	"fmt"
	"strings"
)

// sseDataAssembler joins multiline JSON data fields. Complete single-line JSON
// and opaque legacy payloads are dispatched immediately, even without a blank
// separator. Pending JSON is flushed at the event boundary, DONE, or EOF.
type sseDataAssembler struct {
	pending strings.Builder
	limit   int
}

func (a *sseDataAssembler) flush() string {
	data := a.pending.String()
	a.pending.Reset()
	return data
}

func (a *sseDataAssembler) push(line string) (data string, done bool, err error) {
	if line == "" {
		return a.flush(), false, nil
	}
	if line != "[DONE]" {
		if !strings.HasPrefix(line, "data:") {
			return "", false, nil
		}
		line = strings.TrimPrefix(line, "data:")
	}
	line = strings.TrimSpace(line)
	if line == "[DONE]" {
		return a.flush(), true, nil
	}
	if a.pending.Len() == 0 {
		if len(line) > a.limit {
			return "", false, fmt.Errorf("SSE event exceeds %d bytes", a.limit)
		}
		if line == "" || (line[0] != '{' && line[0] != '[') || json.Valid([]byte(line)) {
			return line, false, nil
		}
		a.pending.WriteString(line)
		return "", false, nil
	}
	if a.pending.Len()+1+len(line) > a.limit {
		return "", false, fmt.Errorf("SSE event exceeds %d bytes", a.limit)
	}
	a.pending.WriteByte('\n')
	a.pending.WriteString(line)
	return "", false, nil
}
