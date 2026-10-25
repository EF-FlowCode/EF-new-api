// Package knowledge describes retrieval model capabilities independently of
// routing groups. A group is a commercial configuration, not a protocol.
package knowledge

import "strings"

type Capability struct {
	Kind       string `json:"kind"`
	Multimodal bool   `json:"multimodal"`
	BatchFile  bool   `json:"batch_file"`
}

func Lookup(name string) (Capability, bool) {
	n := strings.ToLower(name)
	c := Capability{}
	switch {
	case strings.Contains(n, "rerank"):
		c.Kind = "rerank"
	case strings.Contains(n, "embedding"):
		c.Kind = "embedding"
	default:
		return c, false
	}
	c.Multimodal = strings.Contains(n, "vl-") || strings.Contains(n, "embedding-vision") || strings.Contains(n, "multimodal-embedding")
	c.BatchFile = SupportsAliBatch(n)
	return c, true
}

// An explicit provider allowlist prevents synchronous array requests or model
// name lookalikes from being advertised/charged as native Batch File jobs.
func SupportsAliBatch(name string) bool {
	switch name {
	case "text-embedding-v1", "text-embedding-v2", "text-embedding-v3", "text-embedding-v4", "qwen3.7-text-embedding", "qwen3.7-text-embedding-flash":
		return true
	}
	return false
}
