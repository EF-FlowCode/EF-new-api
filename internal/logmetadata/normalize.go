// Package logmetadata defines the shared private metadata boundary for log readers
// and the deliberately stricter, lossless historical migration.
package logmetadata

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"
)

func PrivateKeys() [4]string {
	return [4]string{"channel_id", "channel_name", "channel_type", "reject_reason"}
}

// Normalize changes only the location of historical private fields. On any
// ambiguity it returns the original input and an error; it never repairs data.
func Normalize(value string) (string, error) {
	if value == "" {
		return value, nil
	}
	if !utf8.ValidString(value) || !json.Valid([]byte(value)) {
		return value, errors.New("invalid JSON or UTF-8")
	}
	d := json.NewDecoder(bytes.NewBufferString(value))
	d.UseNumber()
	if err := uniqueKeys(d); err != nil {
		return value, err
	}
	if _, err := d.Token(); err != io.EOF {
		return value, errors.New("trailing JSON")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(value), &fields); err != nil || fields == nil {
		return value, errors.New("metadata is not an object")
	}
	admin := map[string]json.RawMessage{}
	if raw, ok := fields["admin_info"]; ok {
		if err := json.Unmarshal(raw, &admin); err != nil || admin == nil {
			return value, errors.New("admin_info is not an object")
		}
	}
	changed := false
	for _, key := range PrivateKeys() {
		if raw, ok := fields[key]; ok {
			if existing, exists := admin[key]; exists {
				var a, b bytes.Buffer
				_ = json.Compact(&a, raw)
				_ = json.Compact(&b, existing)
				if !bytes.Equal(a.Bytes(), b.Bytes()) {
					return value, errors.New("conflicting private field")
				}
			}
			admin[key] = raw
			delete(fields, key)
			changed = true
		}
	}
	if !changed {
		return value, nil
	}
	fields["admin_info"], _ = json.Marshal(admin)
	result, err := json.Marshal(fields)
	if err != nil {
		return value, errors.New("cannot encode metadata")
	}
	return string(result), nil
}

// Reject duplicate keys at every depth, including escaped equivalents. Do not
// include field names or values in errors: metadata may contain private data.
func uniqueKeys(d *json.Decoder) error {
	token, err := d.Token()
	if err != nil {
		return errors.New("invalid JSON")
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	seen := map[string]bool{}
	for d.More() {
		if delim == '{' {
			key, err := d.Token()
			if err != nil {
				return errors.New("invalid JSON key")
			}
			name, ok := key.(string)
			if strings.ContainsRune(name, utf8.RuneError) {
				return errors.New("ambiguous Unicode JSON key")
			}
			if !ok || seen[name] {
				return errors.New("duplicate JSON key")
			}
			seen[name] = true
		}
		if err := uniqueKeys(d); err != nil {
			return err
		}
	}
	_, err = d.Token()
	return err
}
