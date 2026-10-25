package systemoneproxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// SystemOneRequest keeps the native structured decision protocol, including
// extension fields. Only model is rewritten when applying channel mapping.
type SystemOneRequest struct {
	Model     string                       `json:"model"`
	State     json.RawMessage              `json:"state"`
	Questions map[string]SystemOneQuestion `json:"questions"`
	RawBody   json.RawMessage              `json:"-"`
}

type SystemOneQuestion struct {
	Type         string          `json:"type"`
	Instructions json.RawMessage `json:"instructions"`
	Criteria     json.RawMessage `json:"criteria,omitempty"`
}

func (r *SystemOneRequest) UnmarshalJSON(data []byte) error {
	type plain SystemOneRequest
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*r = SystemOneRequest(value)
	r.RawBody = append(json.RawMessage(nil), data...)
	return nil
}

func (r SystemOneRequest) MarshalJSON() ([]byte, error) {
	type plain SystemOneRequest
	if len(r.RawBody) == 0 {
		return json.Marshal(plain(r))
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(r.RawBody, &fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, fmt.Errorf("SystemOne request must be an object")
	}
	fields["model"], _ = json.Marshal(r.Model)
	return json.Marshal(fields)
}

func structuredValue(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return len(raw) > 0 && json.Valid(raw) && (raw[0] == '"' || raw[0] == '{' || raw[0] == '[')
}

func (r *SystemOneRequest) Validate() error {
	if r == nil || strings.TrimSpace(r.Model) == "" {
		return fmt.Errorf("model is required")
	}
	if !structuredValue(r.State) {
		return fmt.Errorf("state must be a string, object or array")
	}
	if len(r.Questions) == 0 {
		return fmt.Errorf("questions must be a non-empty object")
	}
	// This endpoint is synchronous. Reject rather than silently discard streaming.
	var fields map[string]json.RawMessage
	if len(r.RawBody) > 0 {
		_ = json.Unmarshal(r.RawBody, &fields)
	}
	if stream, ok := fields["stream"]; ok && !bytes.Equal(bytes.TrimSpace(stream), []byte("false")) {
		return fmt.Errorf("systemone does not support streaming")
	}
	for id, q := range r.Questions {
		if !structuredValue(q.Instructions) {
			return fmt.Errorf("questions.%s.instructions must be a string, object or array", id)
		}
		switch q.Type {
		case "noul":
			if len(q.Criteria) == 0 {
				continue
			}
			var criteria map[string]*string
			if err := json.Unmarshal(q.Criteria, &criteria); err != nil || criteria == nil {
				return fmt.Errorf("questions.%s.criteria must contain string true/false descriptions", id)
			}
			for key, value := range criteria {
				if value == nil {
					return fmt.Errorf("questions.%s.criteria descriptions must be strings", id)
				}
				if key != "true" && key != "false" {
					return fmt.Errorf("questions.%s.criteria has an invalid noul key", id)
				}
			}
		case "choice":
			var criteria map[string]*string
			if err := json.Unmarshal(q.Criteria, &criteria); err != nil || len(criteria) == 0 {
				return fmt.Errorf("questions.%s.criteria must be a non-empty map of string or null choices", id)
			}
		case "score":
			var criteria []*string
			if err := json.Unmarshal(q.Criteria, &criteria); err != nil || len(criteria) < 2 {
				return fmt.Errorf("questions.%s.criteria must contain at least two string levels", id)
			}
			for _, value := range criteria {
				if value == nil {
					return fmt.Errorf("questions.%s.criteria levels must be strings", id)
				}
			}
		default:
			return fmt.Errorf("questions.%s.type must be noul, choice or score", id)
		}
	}
	return nil
}
