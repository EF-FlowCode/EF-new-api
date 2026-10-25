package knowledge

import (
	"strings"
	"testing"
)

func TestKnowledgeUsage(t *testing.T) {
	for _, tt := range []struct {
		name, body         string
		legacy             bool
		input, image       int
		estimated, invalid bool
	}{
		{"total only", `{"usage":{"total_tokens":184}}`, false, 184, 0, false, false},
		{"prompt only", `{"usage":{"prompt_tokens":79}}`, false, 79, 0, false, false},
		{"authoritative zero", `{"usage":{"total_tokens":0}}`, false, 0, 0, false, false},
		{"missing", `{}`, false, 999, 0, true, false},
		{"vision inclusive", `{"usage":{"input_tokens":150,"image_tokens":100,"output_tokens":7}}`, false, 150, 100, false, false},
		{"legacy additive", `{"usage":{"input_tokens":50,"image_tokens":100}}`, true, 150, 100, false, false},
		{"details", `{"usage":{"total_tokens":150,"prompt_tokens_details":{"image_tokens":100}}}`, false, 150, 100, false, false},
		{"negative", `{"usage":{"total_tokens":-1}}`, false, 0, 0, false, true},
		{"inconsistent", `{"usage":{"total_tokens":10,"image_tokens":20}}`, false, 0, 0, false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			u, err := ParseUsage([]byte(tt.body), 999, tt.legacy)
			if (err != nil) != tt.invalid {
				t.Fatalf("error=%v", err)
			}
			if !tt.invalid && (u.Input != tt.input || u.Image != tt.image || u.Estimated != tt.estimated) {
				t.Fatalf("usage=%+v", u)
			}
		})
	}
}

func TestKnowledgeBatchValidationAndAccounting(t *testing.T) {
	input := `{"custom_id":"a","method":"POST","url":"/v1/embeddings","body":{"model":"qwen3.7-text-embedding","input":["hello","world"]}}`
	m, err := ValidateBatch([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = ValidateBatch([]byte(input + "\n" + input)); err == nil {
		t.Fatal("duplicate ID accepted")
	}
	if _, err = ValidateBatch([]byte(strings.Replace(input, "qwen3.7-text-embedding", "qwen3-rerank", 1))); err == nil {
		t.Fatal("unverified batch model accepted")
	}
	result := `{"custom_id":"a","response":{"status_code":200,"body":{"usage":{"total_tokens":184}}},"error":null}`
	tokens, count, err := ReadBatchUsage(strings.NewReader(result), m.IDs)
	if err != nil || tokens != 184 || count != 1 {
		t.Fatalf("%d %d %v", tokens, count, err)
	}
	if _, _, err = ReadBatchUsage(strings.NewReader(result+"\n"+result), m.IDs); err == nil {
		t.Fatal("duplicate result charged")
	}
	if _, _, err = ReadBatchUsage(strings.NewReader(strings.Replace(result, `"total_tokens":184`, `"unused":184`, 1)), m.IDs); err == nil {
		t.Fatal("missing authoritative usage charged")
	}
}

func TestKnowledgeBatchRejectsKeysThatPriceAndExecuteDifferently(t *testing.T) {
	for name, line := range map[string]string{
		// Priced from "Body" (flash), executed by the provider from "body".
		"body case variant":  `{"custom_id":"a","method":"POST","url":"/v1/embeddings","body":{"model":"qwen3.7-text-embedding","input":["big"]},"Body":{"model":"qwen3.7-text-embedding-flash","input":"x"}}`,
		"model case variant": `{"custom_id":"a","method":"POST","url":"/v1/embeddings","body":{"model":"qwen3.7-text-embedding","MODEL":"qwen3.7-text-embedding-flash","input":"x"}}`,
		"duplicate body key": `{"custom_id":"a","method":"POST","url":"/v1/embeddings","body":{"model":"qwen3.7-text-embedding","input":"x","input":"y"}}`,
		"duplicate top key":  `{"custom_id":"a","custom_id":"b","method":"POST","url":"/v1/embeddings","body":{"model":"qwen3.7-text-embedding","input":"x"}}`,
		"unknown top key":    `{"custom_id":"a","method":"POST","url":"/v1/embeddings","extra":1,"body":{"model":"qwen3.7-text-embedding","input":"x"}}`,
		"trailing object":    `{"custom_id":"a","method":"POST","url":"/v1/embeddings","body":{"model":"qwen3.7-text-embedding","input":"x"}} {}`,
	} {
		if _, err := ValidateBatch([]byte(line)); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}

	input := `{"body":{"input":"hello","model":"qwen3.7-text-embedding","text_type":"query"},"url":"/v1/embeddings","method":"POST","custom_id":"a"}`
	m, err := ValidateBatch([]byte(input))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"custom_id":"a","method":"POST","url":"/v1/embeddings","body":{"input":"hello","model":"qwen3.7-text-embedding","text_type":"query"}}` + "\n"
	if string(m.Canonical) != want {
		t.Fatalf("canonical=%s", m.Canonical)
	}
	if _, err = ValidateBatch(m.Canonical); err != nil {
		t.Fatalf("canonical output must validate again: %v", err)
	}
}
