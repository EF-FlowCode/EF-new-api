package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/internal/testconfig"
	"github.com/stretchr/testify/require"
)

func TestWebhookNotifyReplacesValuePlaceholders(t *testing.T) {
	testconfig.Set(t, "fetch_setting", map[string]string{"enable_ssrf_protection": "false"})
	InitHttpClient()
	var got WebhookPayload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = common.Unmarshal(body, &got)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	err := SendWebhookNotify(server.URL, "", dto.NewNotify("quota", "额度预警", "剩余 {{value}}，请前往 {{value}} 充值", []any{"$1.00", "https://example.com/topup"}))
	require.NoError(t, err)
	require.Equal(t, "剩余 $1.00，请前往 https://example.com/topup 充值", got.Content)
}

func TestNotifyOutboundRequestsTimeOut(t *testing.T) {
	testconfig.Set(t, "fetch_setting", map[string]string{"enable_ssrf_protection": "false"})
	InitHttpClient()
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(release)
	previous := notifyOutboundTimeout
	notifyOutboundTimeout = 200 * time.Millisecond
	t.Cleanup(func() { notifyOutboundTimeout = previous })

	start := time.Now()
	err := sendBarkNotify(server.URL+"/{{title}}/{{content}}", dto.NewNotify("quota", "t", "c", nil))
	require.ErrorContains(t, err, context.DeadlineExceeded.Error())
	require.Less(t, time.Since(start), 5*time.Second)
}

func TestLegacyEmailTemplateSubjectUsesUnescapedValues(t *testing.T) {
	vars := map[string]string{"ticket_subject": "A &amp; B &lt;x&gt;"}
	subject, body := renderEmailTemplate(constant.EmailTemplateSpec{}, "工单：{{ticket_subject}}", "<p>{{ticket_subject}}</p>", vars)
	require.Equal(t, "工单：A & B <x>", subject)
	require.Equal(t, "<p>A &amp; B &lt;x&gt;</p>", body)
}
