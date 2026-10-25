package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/dto"
)

// WebhookPayload webhook 通知的负载数据
type WebhookPayload struct {
	Type      string `json:"type"`
	Title     string `json:"title"`
	Content   string `json:"content"`
	Values    []any  `json:"values,omitempty"`
	Timestamp int64  `json:"timestamp"`
}

// generateSignature 生成 webhook 签名
func generateSignature(secret string, payload []byte) string {
	h := hmac.New(sha256.New, []byte(secret))
	h.Write(payload)
	return hex.EncodeToString(h.Sum(nil))
}

func buildWebhookOutboundRequest(webhookURL string, secret string, payload []byte, throughWorker bool) outboundRequest {
	headers := map[string]string{"Content-Type": "application/json"}
	if secret != "" {
		headers["X-Webhook-Signature"] = generateSignature(secret, payload)
		if throughWorker {
			headers["Authorization"] = "Bearer " + secret
		}
	}
	return outboundRequest{
		URL:     webhookURL,
		Method:  http.MethodPost,
		Headers: headers,
		Body:    payload,
	}
}

// SendWebhookNotify 发送 webhook 通知
func SendWebhookNotify(webhookURL string, secret string, data dto.Notify) error {
	// 处理占位符
	content := data.Content
	for _, value := range data.Values {
		content = strings.Replace(content, dto.ContentValueParam, fmt.Sprintf("%v", value), 1)
	}

	// 构建 webhook 负载
	payload := WebhookPayload{
		Type:      data.Type,
		Title:     data.Title,
		Content:   content,
		Values:    data.Values,
		Timestamp: time.Now().Unix(),
	}

	// 序列化负载
	payloadBytes, err := common.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal webhook payload: %v", err)
	}

	adapter, throughWorker := configuredOutboundHTTPAdapter()
	request := buildWebhookOutboundRequest(webhookURL, secret, payloadBytes, throughWorker)
	ctx, cancel := context.WithTimeout(context.Background(), notifyOutboundTimeout)
	defer cancel()
	return sendOutboundNotification(ctx, adapter, throughWorker, "webhook", request)
}
