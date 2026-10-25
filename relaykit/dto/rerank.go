package dto

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"

	"eflow.local/new-api/relaykit/types"
)

type RerankRequest struct {
	Documents       []any    `json:"documents"`
	Query           any      `json:"query"`
	Instruct        string   `json:"instruct,omitempty"`
	FPS             *float64 `json:"fps,omitempty"`
	Model           string   `json:"model"`
	TopN            *int     `json:"top_n,omitempty"`
	ReturnDocuments *bool    `json:"return_documents,omitempty"`
	MaxChunkPerDoc  *int     `json:"max_chunk_per_doc,omitempty"`
	OverLapTokens   *int     `json:"overlap_tokens,omitempty"`
}

func (r *RerankRequest) IsStream(c *http.Request) bool {
	return false
}

func (r *RerankRequest) GetTokenCountMeta() *types.TokenCountMeta {
	var texts = make([]string, 0)

	for _, document := range r.Documents {
		texts = append(texts, fmt.Sprintf("%v", document))
	}

	query, _ := r.Query.(string)
	if content, ok := r.Query.(map[string]any); ok {
		query, _ = content["text"].(string)
	}
	if query != "" {
		// Rerank evaluates query/document pairs; upstream usage supersedes this estimate.
		for range r.Documents {
			texts = append(texts, query)
		}
	}

	return &types.TokenCountMeta{
		CombineText: strings.Join(texts, "\n"),
	}
}

func (r *RerankRequest) ValidateQuery() error {
	if r == nil {
		return errors.New("query is empty")
	}
	switch q := r.Query.(type) {
	case string:
		if strings.TrimSpace(q) == "" {
			return errors.New("query is empty")
		}
	case map[string]any:
		if len(q) != 1 {
			return errors.New("query must contain one text or image field")
		}
		text, textOK := q["text"].(string)
		image, imageOK := q["image"].(string)
		if !(textOK && strings.TrimSpace(text) != "" || imageOK && strings.TrimSpace(image) != "") {
			return errors.New("query must contain nonempty text or image")
		}
	default:
		return errors.New("query must be text or a text/image object")
	}
	if r.TopN != nil && *r.TopN <= 0 {
		return errors.New("top_n must be positive")
	}
	if r.FPS != nil && (*r.FPS <= 0 || math.IsNaN(*r.FPS) || math.IsInf(*r.FPS, 0)) {
		return errors.New("fps must be positive and finite")
	}
	return nil
}

func (r *RerankRequest) SetModelName(modelName string) {
	if modelName != "" {
		r.Model = modelName
	}
}

func (r *RerankRequest) GetReturnDocuments() bool {
	if r.ReturnDocuments == nil {
		return false
	}
	return *r.ReturnDocuments
}

type RerankResponseResult struct {
	Document       any     `json:"document,omitempty"`
	Index          int     `json:"index"`
	RelevanceScore float64 `json:"relevance_score"`
}

type RerankDocument struct {
	Text any `json:"text"`
}

type RerankResponse struct {
	Results []RerankResponseResult `json:"results"`
	Usage   Usage                  `json:"usage"`
}
