package systemoneproxy_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"eflow.local/new-api/model"
	"github.com/stretchr/testify/require"
)

func TestGatewayNativeRoundTrip(t *testing.T) {
	for _, protocol := range []string{"typesafe", "openrouter"} {
		t.Run(protocol, func(t *testing.T) {
			f := setupSystemOne(t)
			calls := 0
			var captured map[string]json.RawMessage
			var path, auth string
			// Whitespace, large integers and extension fields must survive forced Chat formatting.
			response := " \n" + systemOneAnswers + "\n"
			supplier := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				path, auth = r.URL.Path, r.Header.Get("Authorization")
				_ = json.NewDecoder(r.Body).Decode(&captured)
				_, _ = io.WriteString(w, response)
			}))
			defer supplier.Close()
			wantPath := "/v1/systemone"
			if protocol == "openrouter" {
				wantPath = "/api/alpha/decisions"
			}
			handler := f.proxy(t, supplier.URL+wantPath, protocol, "mockupstreamkey")
			for _, body := range []string{systemOneMinimal, systemOneAllQuestions} {
				w := nativeRequest(handler, body, f.token.Key)
				require.Equal(t, 200, w.Code, w.Body.String())
				require.Equal(t, response, w.Body.String())
				require.Equal(t, wantPath, path)
				require.True(t, auth == "Bearer mockupstreamkey", "must use upstream channel credentials")
				require.JSONEq(t, `"jev-1.13"`, string(captured["model"]))
				require.NotContains(t, captured, "messages")
				if strings.Contains(body, "extension") {
					require.JSONEq(t, `{"exact":9007199254740993}`, string(captured["extension"]))
				}
			}
			require.Equal(t, 2, calls)
			logs := f.assertBilling(t, 40, 2) // 2 * (14 input + 3 output * 2).
			for _, entry := range logs {
				require.Equal(t, 14, entry.PromptTokens)
				require.Equal(t, 3, entry.CompletionTokens)
				require.Equal(t, 20, entry.Quota)
			}
			require.Equal(t, "decision-alias", logs[1].ModelName)
		})
	}
}

func TestGatewayErrorsRefund(t *testing.T) {
	for _, scenario := range []string{"invalid", "unauthenticated", "badkey", "forbidden", "nochannel", "400", "401", "422", "429", "529", "malformed", "invalidusage", "zerousage", "missingusage"} {
		t.Run(scenario, func(t *testing.T) {
			f := setupSystemOne(t)
			supplier := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if status, err := strconv.Atoi(scenario); err == nil {
					w.WriteHeader(status)
					_, _ = io.WriteString(w, `{"error":{"message":"secret-like value must not be echoed"}}`)
					return
				}
				body := systemOneAnswers
				switch scenario {
				case "malformed":
					body = `{"choices":[]}`
				case "invalidusage":
					body = strings.Replace(body, `"prompt_tokens":14`, `"prompt_tokens":-1`, 1)
				case "zerousage":
					body = strings.Replace(body, `"prompt_tokens":14`, `"prompt_tokens":0`, 1)
				case "missingusage":
					body = strings.Replace(body, `"usage":`, `"missing_usage":`, 1)
				}
				_, _ = io.WriteString(w, body)
			}))
			defer supplier.Close()
			handler := f.proxy(t, supplier.URL+"/v1/systemone", "typesafe", "mockupstreamkey")
			body, key, want := systemOneMinimal, f.token.Key, 502
			switch scenario {
			case "invalid":
				body = strings.Replace(body, `"state":"Please refund the duplicate charge."`, `"state":null`, 1)
				want = 400
			case "unauthenticated":
				key = ""
				want = 401
			case "badkey":
				key = "invalidkey"
				want = 401
			case "forbidden":
				require.NoError(t, model.DB.Model(&model.Token{}).Where("id = ?", f.token.Id).Updates(map[string]any{"model_limits_enabled": true, "model_limits": "other"}).Error)
				want = 403
			case "nochannel":
				require.NoError(t, model.DB.Model(&model.Ability{}).Where("channel_id > 0").Update("enabled", false).Error)
				want = 503
			default:
				if status, err := strconv.Atoi(scenario); err == nil {
					want = status
				}
			}
			w := nativeRequest(handler, body, key)
			require.Equal(t, want, w.Code, w.Body.String())
			require.Contains(t, w.Body.String(), `"error"`)
			require.NotContains(t, w.Body.String(), "secret-like")
			f.assertBilling(t, 0, 0)
		})
	}
}

// Explicit opt-in only. Credentials are never fixtures or command-line flags.
func TestLiveSystemOne(t *testing.T) {
	key, endpoint := os.Getenv("SYSTEMONE_LIVE_KEY"), os.Getenv("SYSTEMONE_LIVE_ENDPOINT")
	if key == "" || endpoint == "" {
		t.Skip("SYSTEMONE_LIVE_KEY and SYSTEMONE_LIVE_ENDPOINT are required for live smoke testing")
	}
	f := setupSystemOne(t)
	handler := f.proxy(t, endpoint, "typesafe", "mockupstreamkey", key)
	for _, body := range []string{systemOneMinimal, systemOneAllQuestions} {
		w := nativeRequest(handler, body, f.token.Key)
		require.Equal(t, 200, w.Code, "live upstream status")
		var result struct {
			Answers map[string]json.RawMessage `json:"answers"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
		require.Contains(t, result.Answers, "refund_requested")
		if strings.Contains(body, "decision-alias") {
			require.Contains(t, result.Answers, "team")
			require.Contains(t, result.Answers, "urgency")
		}
	}
	f.inFlight.Wait()
	var logs []model.Log
	require.NoError(t, model.LOG_DB.Where("type = ?", model.LogTypeConsume).Find(&logs).Error)
	require.Len(t, logs, 2)
	total := 0
	for _, entry := range logs {
		require.Greater(t, entry.PromptTokens, 0)
		require.Equal(t, entry.PromptTokens+2*entry.CompletionTokens, entry.Quota)
		total += entry.Quota
		t.Logf("live model=%s input=%d output=%d quota=%d", entry.ModelName, entry.PromptTokens, entry.CompletionTokens, entry.Quota)
	}
	f.assertBilling(t, total, 2)
}
