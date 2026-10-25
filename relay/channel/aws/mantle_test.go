package aws

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	relaycommon "eflow.local/new-api/relay/common"
	relaytypes "eflow.local/new-api/types"
	"github.com/aws/aws-sdk-go-v2/aws"
	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const awsTestModel = "anthropic.claude-3-5-sonnet-20240620-v1:0"

func newAwsTestContext(writer http.ResponseWriter, requestContext context.Context) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil).WithContext(requestContext)
	return c
}

func newAwsTestRelayInfo() *relaycommon.RelayInfo {
	return &relaycommon.RelayInfo{
		StartTime:          time.Now(),
		IsStream:           true,
		OriginModelName:    awsTestModel,
		RelayFormat:        relaytypes.RelayFormatOpenAI,
		ShouldIncludeUsage: true,
		ChannelMeta: &relaycommon.ChannelMeta{
			UpstreamModelName: awsTestModel,
		},
	}
}

func newMantleTestInfo() *relaycommon.RelayInfo {
	info := newAwsTestRelayInfo()
	info.IsStream = false
	info.RelayFormat = relaytypes.RelayFormatClaude
	info.UpstreamModelName = "claude-opus-4-7"
	info.ApiKey = "test-api-key|us-east-1"
	info.ChannelOtherSettings.AwsApiType = dto.AwsApiTypeMantle
	return info
}

func TestMantleRequestPreservesNativeFieldsAndSignsFinalPayload(t *testing.T) {
	for _, key := range []string{"test-api-key|us-east-1", "access-key|secret-key|eu-west-1"} {
		t.Run(strings.Split(key, "|")[0], func(t *testing.T) {
			info := newMantleTestInfo()
			info.ApiKey = key
			info.UseRuntimeHeadersOverride = true
			info.RuntimeHeadersOverride = map[string]any{"anthropic-beta": "test-beta", "X-Test": "override"}
			info.ChannelSetting.PassThroughBodyEnabled = true
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			c := newAwsTestContext(httptest.NewRecorder(), ctx)
			c.Request.Header.Set("Authorization", "Bearer client-secret")
			c.Request.Header.Set("x-api-key", "client-secret")
			a := &Adaptor{}
			body := `{"model":"client-alias","stream":true,"max_tokens":128,"temperature":0,"cache_control":{"type":"ephemeral","ttl":"1h"},"messages":[{"role":"user","content":[{"type":"text","text":"hello","cache_control":{"type":"ephemeral"}}]}],"future_option":{"enabled":false},"anthropic_version":"bedrock-2023-05-31"}`
			req, err := a.newMantleRequest(c, info, strings.NewReader(body))
			require.NoError(t, err)
			assert.Equal(t, "/anthropic/v1/messages", req.URL.Path)
			assert.Equal(t, http.MethodPost, req.Method)
			assert.Equal(t, "2023-06-01", req.Header.Get("anthropic-version"))
			assert.Equal(t, "test-beta", req.Header.Get("anthropic-beta"))
			assert.Equal(t, "override", req.Header.Get("X-Test"))
			payload, err := io.ReadAll(req.Body)
			require.NoError(t, err)
			var data map[string]json.RawMessage
			require.NoError(t, common.Unmarshal(payload, &data))
			assert.JSONEq(t, `"anthropic.claude-opus-4-7"`, string(data["model"]))
			assert.JSONEq(t, `{"type":"ephemeral","ttl":"1h"}`, string(data["cache_control"]))
			assert.JSONEq(t, `[{"role":"user","content":[{"type":"text","text":"hello","cache_control":{"type":"ephemeral"}}]}]`, string(data["messages"]))
			assert.JSONEq(t, `{"enabled":false}`, string(data["future_option"]))
			assert.Equal(t, "0", string(data["temperature"]))
			assert.Equal(t, "true", string(data["stream"]))
			assert.NotContains(t, data, "anthropic_version")
			replay, err := req.GetBody()
			require.NoError(t, err)
			defer replay.Close()
			replayed, err := io.ReadAll(replay)
			require.NoError(t, err)
			assert.Equal(t, payload, replayed)
			if strings.HasPrefix(key, "access-key|") {
				assert.Empty(t, req.Header.Get("x-api-key"))
				auth := req.Header.Get("Authorization")
				assert.Contains(t, auth, "/eu-west-1/bedrock-mantle/aws4_request")
				assert.NotContains(t, auth, "secret-key")
				signedAt, err := time.Parse("20060102T150405Z", req.Header.Get("X-Amz-Date"))
				require.NoError(t, err)
				verification := req.Clone(ctx)
				verification.Header.Del("Authorization")
				hash := sha256.Sum256(payload)
				err = v4.NewSigner().SignHTTP(ctx, aws.Credentials{AccessKeyID: "access-key", SecretAccessKey: "secret-key"}, verification, hex.EncodeToString(hash[:]), "bedrock-mantle", "eu-west-1", signedAt)
				require.NoError(t, err)
				assert.Equal(t, auth, verification.Header.Get("Authorization"))
			} else {
				assert.Equal(t, "test-api-key", req.Header.Get("x-api-key"))
				assert.Empty(t, req.Header.Get("Authorization"))
			}
			cancel()
			assert.ErrorIs(t, req.Context().Err(), context.Canceled)
		})
	}
}

func TestMantleEndpointAndCredentialValidation(t *testing.T) {
	for _, test := range []struct {
		name, key, base, want string
		invalid               bool
	}{
		{name: "regional default", key: "key|us-west-2", want: "https://bedrock-mantle.us-west-2.api.aws/anthropic/v1/messages"},
		{name: "proxy root", key: "ak|sk|us-east-1", base: "https://proxy.example/gateway/", want: "https://proxy.example/gateway/anthropic/v1/messages"},
		{name: "anthropic root", key: "key|us-east-1", base: "https://proxy.example/anthropic/", want: "https://proxy.example/anthropic/v1/messages"},
		{name: "version root", key: "key|us-east-1", base: "https://proxy.example/anthropic/v1", want: "https://proxy.example/anthropic/v1/messages"},
		{name: "full endpoint", key: "key|us-east-1", base: "https://proxy.example/anthropic/v1/messages", want: "https://proxy.example/anthropic/v1/messages"},
		{name: "missing region", key: "secret", invalid: true},
		{name: "empty key", key: "|us-east-1", invalid: true},
		{name: "empty secret", key: "ak||us-east-1", invalid: true},
		{name: "injected host", key: "key|us-east-1.evil.example/", invalid: true},
		{name: "plaintext URL", key: "key|us-east-1", base: "http://proxy.example", invalid: true},
		{name: "URL credentials", key: "key|us-east-1", base: "https://user:secret@proxy.example", invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			info := newMantleTestInfo()
			info.ApiKey, info.ChannelBaseUrl = test.key, test.base
			endpoint, err := (&Adaptor{}).GetRequestURL(info)
			if test.invalid {
				require.Error(t, err)
				assert.NotContains(t, err.Error(), "secret")
				assert.Empty(t, endpoint)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, test.want, endpoint)
		})
	}
}

func TestMantleHTTPResponsesPreserveCacheUsage(t *testing.T) {
	originalTimeout := constant.StreamingTimeout
	constant.StreamingTimeout = 30
	t.Cleanup(func() { constant.StreamingTimeout = originalTimeout })
	const usageJSON = `{"input_tokens":100,"output_tokens":23,"cache_read_input_tokens":1000,"cache_creation_input_tokens":50,"cache_creation":{"ephemeral_5m_input_tokens":20,"ephemeral_1h_input_tokens":30}}`
	for _, stream := range []bool{false, true} {
		for _, format := range []relaytypes.RelayFormat{relaytypes.RelayFormatClaude, relaytypes.RelayFormatOpenAI} {
			t.Run(fmt.Sprintf("stream=%t/format=%s", stream, format), func(t *testing.T) {
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if stream {
						w.Header().Set("Content-Type", "text/event-stream")
						fmt.Fprintf(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_mantle\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"anthropic.claude-opus-4-7\",\"content\":[],\"usage\":%s}}\n\n", usageJSON)
						fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hello\"}}\n\n")
						fmt.Fprint(w, "event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":23}}\n\n")
						fmt.Fprint(w, "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
						return
					}
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprintf(w, `{"id":"msg_mantle","type":"message","role":"assistant","model":"anthropic.claude-opus-4-7","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":%s}`, usageJSON)
				}))
				defer server.Close()
				original := http.DefaultClient
				http.DefaultClient = server.Client()
				defer func() { http.DefaultClient = original }()
				info := newMantleTestInfo()
				info.ChannelBaseUrl, info.IsStream, info.RelayFormat = server.URL, stream, format
				recorder := httptest.NewRecorder()
				c := newAwsTestContext(recorder, context.Background())
				a := &Adaptor{}
				a.Init(info)
				var request dto.ClaudeRequest
				require.NoError(t, common.UnmarshalJsonStr(`{"model":"claude-opus-4-7","max_tokens":128,"messages":[{"role":"user","content":"hello"}],"cache_control":{"type":"ephemeral"}}`, &request))
				request.Stream = &stream
				converted, err := a.ConvertClaudeRequest(c, info, &request)
				require.NoError(t, err)
				body, err := common.Marshal(converted)
				require.NoError(t, err)
				response, err := a.DoRequest(c, info, bytes.NewReader(body))
				require.NoError(t, err)
				httpResponse, ok := response.(*http.Response)
				require.True(t, ok)
				result, apiErr := a.DoResponse(c, httpResponse, info)
				require.Nil(t, apiErr)
				usage, ok := result.(*dto.Usage)
				require.True(t, ok)
				// eflow keeps Claude cache usage on dto.Usage (same as the
				// Anthropic channel) rather than upstream's BillingUsage.
				assert.Equal(t, 23, usage.CompletionTokens)
				assert.Equal(t, 1000, usage.PromptTokensDetails.CachedTokens)
				assert.Equal(t, 50, usage.PromptTokensDetails.CachedCreationTokens)
				assert.Equal(t, 20, usage.ClaudeCacheCreation5mTokens)
				assert.Equal(t, 30, usage.ClaudeCacheCreation1hTokens)
				assert.Contains(t, recorder.Body.String(), "hello")
				if stream && format == relaytypes.RelayFormatOpenAI {
					assert.Contains(t, recorder.Body.String(), "[DONE]")
				}
			})
		}
	}
}

func TestAWSLegacyInterfaceRemainsDefault(t *testing.T) {
	for _, mode := range []dto.AwsApiType{"", dto.AwsApiTypeInvokeModel} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%t", mode, stream), func(t *testing.T) {
				info := newMantleTestInfo()
				info.ChannelOtherSettings.AwsApiType = mode
				info.IsStream = stream
				info.UpstreamModelName = "claude-3-5-sonnet-20240620"
				c := newAwsTestContext(httptest.NewRecorder(), context.Background())
				a := &Adaptor{}
				a.Init(info)
				response, err := a.DoRequest(c, info, strings.NewReader(`{"max_tokens":128,"messages":[{"role":"user","content":[{"type":"text","text":"hi","cache_control":{"type":"ephemeral"}}]}],"cache_control":{"type":"ephemeral"}}`))
				require.NoError(t, err)
				assert.Nil(t, response)
				var body []byte
				if stream {
					input, ok := a.AwsReq.(*bedrockruntime.InvokeModelWithResponseStreamInput)
					require.True(t, ok)
					body = input.Body
					assert.Equal(t, "us."+awsTestModel, *input.ModelId)
				} else {
					input, ok := a.AwsReq.(*bedrockruntime.InvokeModelInput)
					require.True(t, ok)
					body = input.Body
					assert.Equal(t, "us."+awsTestModel, *input.ModelId)
				}
				var payload map[string]json.RawMessage
				require.NoError(t, common.Unmarshal(body, &payload))
				assert.Equal(t, `"bedrock-2023-05-31"`, string(payload["anthropic_version"]))
				assert.NotContains(t, payload, "cache_control")
				assert.Contains(t, string(payload["messages"]), "cache_control")
			})
		}
	}
}

func TestMantleRejectsInvalidRequestsAndPreservesExplicitModelIDs(t *testing.T) {
	for _, test := range []struct {
		name, model, body, wantModel string
		invalid                      bool
	}{
		{name: "legacy Haiku alias", model: "claude-haiku-4-5-20251001", body: `{"messages":[]}`, wantModel: "anthropic.claude-haiku-4-5"},
		{name: "short Haiku alias", model: "claude-haiku-4-5", body: `{"messages":[]}`, wantModel: "anthropic.claude-haiku-4-5"},
		{name: "explicit Haiku model", model: "anthropic.claude-haiku-4-5", body: `{"messages":[]}`},
		{name: "explicit Haiku inference profile", model: "us.anthropic.claude-haiku-4-5", body: `{"messages":[]}`},
		{name: "explicit regional model", model: "us.anthropic.claude-opus-4-7", body: `{"messages":[]}`},
		{name: "explicit model", model: "anthropic.claude-opus-4-7", body: `{"messages":[]}`},
		{name: "Nova requires legacy", model: "nova-pro-v1:0", body: `{"messages":[]}`, invalid: true},
		{name: "null body", model: "claude-opus-4-7", body: `null`, invalid: true},
		{name: "malformed JSON", model: "claude-opus-4-7", body: `{`, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			info := newMantleTestInfo()
			info.UpstreamModelName = test.model
			c := newAwsTestContext(httptest.NewRecorder(), context.Background())
			req, err := (&Adaptor{}).newMantleRequest(c, info, strings.NewReader(test.body))
			if test.invalid {
				require.Error(t, err)
				assert.Nil(t, req)
				return
			}
			require.NoError(t, err)
			defer req.Body.Close()
			var payload map[string]any
			require.NoError(t, common.DecodeJson(req.Body, &payload))
			wantModel := test.wantModel
			if wantModel == "" {
				wantModel = test.model
			}
			assert.Equal(t, wantModel, payload["model"])
			assert.NotContains(t, payload, "cache_control")
		})
	}
}

func TestMantleSignatureMatchesWireHeaders(t *testing.T) {
	for _, test := range []struct {
		name          string
		http2         bool
		allHopHeaders bool
	}{
		{name: "HTTP2 keep-alive", http2: true},
		{name: "HTTP1 hop-by-hop headers", allHopHeaders: true},
		{name: "HTTP2 hop-by-hop headers", http2: true, allHopHeaders: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			type receivedRequest struct {
				request *http.Request
				body    []byte
				err     error
			}
			received := make(chan receivedRequest, 1)
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				received <- receivedRequest{request: r.Clone(context.Background()), body: body, err: err}
				w.WriteHeader(http.StatusOK)
			}))
			server.EnableHTTP2 = test.http2
			server.StartTLS()
			defer server.Close()
			originalClient := http.DefaultClient
			http.DefaultClient = server.Client()
			defer func() { http.DefaultClient = originalClient }()

			info := newMantleTestInfo()
			info.ChannelBaseUrl = server.URL
			info.ApiKey = "access-key|secret-key|us-east-1"
			info.UseRuntimeHeadersOverride = true
			info.RuntimeHeadersOverride = map[string]any{
				"Connection":     "keep-alive",
				"anthropic-beta": "test-beta",
				"X-Test":         "end-to-end",
			}
			if test.allHopHeaders {
				maps.Copy(info.RuntimeHeadersOverride, map[string]any{
					"Connection":          "keep-alive, x-hop-token",
					"X-Hop-Token":         "hop-only",
					"Keep-Alive":          "timeout=5",
					"Proxy-Connection":    "keep-alive",
					"Proxy-Authenticate":  "Basic",
					"Proxy-Authorization": "Basic test-proxy-token",
					"Te":                  "trailers",
					"Trailer":             "X-Checksum",
					"Transfer-Encoding":   "chunked",
					"Upgrade":             "websocket",
				})
			}
			c := newAwsTestContext(httptest.NewRecorder(), context.Background())
			response, err := (&Adaptor{}).DoRequest(c, info, strings.NewReader(`{"messages":[],"max_tokens":128,"cache_control":{"type":"ephemeral","ttl":"1h"}}`))
			require.NoError(t, err)
			httpResponse, ok := response.(*http.Response)
			require.True(t, ok)
			defer httpResponse.Body.Close()
			wire := <-received
			require.NoError(t, wire.err)
			wantProtocol := 1
			if test.http2 {
				wantProtocol = 2
			}
			require.Equal(t, wantProtocol, wire.request.ProtoMajor)
			for _, name := range []string{"Connection", "X-Hop-Token", "Keep-Alive", "Proxy-Connection", "Proxy-Authenticate", "Proxy-Authorization", "Te", "Trailer", "Transfer-Encoding", "Upgrade"} {
				assert.Empty(t, wire.request.Header.Values(name), "%s must not reach the upstream", name)
			}
			assert.Equal(t, "test-beta", wire.request.Header.Get("anthropic-beta"))
			assert.Equal(t, "end-to-end", wire.request.Header.Get("X-Test"))

			// Recompute the signature from only the bytes and signed headers that
			// reached the server, not from the pre-transport request object.
			authorization := wire.request.Header.Get("Authorization")
			_, signedHeaders, found := strings.Cut(authorization, "SignedHeaders=")
			require.True(t, found)
			signedHeaders, _, found = strings.Cut(signedHeaders, ",")
			require.True(t, found)
			verification := wire.request.Clone(context.Background())
			verification.URL.Scheme = "https"
			verification.URL.Host = wire.request.Host
			verification.Header = make(http.Header)
			for name := range strings.SplitSeq(signedHeaders, ";") {
				if values, ok := wire.request.Header[http.CanonicalHeaderKey(name)]; ok {
					verification.Header[http.CanonicalHeaderKey(name)] = values
				}
			}
			signedAt, err := time.Parse("20060102T150405Z", wire.request.Header.Get("X-Amz-Date"))
			require.NoError(t, err)
			hash := sha256.Sum256(wire.body)
			err = v4.NewSigner().SignHTTP(context.Background(), aws.Credentials{AccessKeyID: "access-key", SecretAccessKey: "secret-key"}, verification, hex.EncodeToString(hash[:]), "bedrock-mantle", "us-east-1", signedAt)
			require.NoError(t, err)
			assert.Equal(t, authorization, verification.Header.Get("Authorization"))
		})
	}
}

func TestAWSLegacyHaikuMappingPreservesVersionAndRegion(t *testing.T) {
	for _, test := range []struct {
		name   string
		mode   dto.AwsApiType
		stream bool
		key    string
	}{
		{name: "default API key", key: "test-api-key|us-east-1"},
		{name: "default API key stream", stream: true, key: "test-api-key|us-east-1"},
		{name: "default AK/SK", key: "access-key|secret-key|us-east-1"},
		{name: "default AK/SK stream", stream: true, key: "access-key|secret-key|us-east-1"},
		{name: "explicit API key", mode: dto.AwsApiTypeInvokeModel, key: "test-api-key|us-east-1"},
		{name: "explicit API key stream", mode: dto.AwsApiTypeInvokeModel, stream: true, key: "test-api-key|us-east-1"},
		{name: "explicit AK/SK", mode: dto.AwsApiTypeInvokeModel, key: "access-key|secret-key|us-east-1"},
		{name: "explicit AK/SK stream", mode: dto.AwsApiTypeInvokeModel, stream: true, key: "access-key|secret-key|us-east-1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			info := newMantleTestInfo()
			info.ChannelOtherSettings.AwsApiType = test.mode
			info.ApiKey, info.IsStream = test.key, test.stream
			info.UpstreamModelName = "claude-haiku-4-5-20251001"
			c := newAwsTestContext(httptest.NewRecorder(), context.Background())
			a := &Adaptor{}
			a.Init(info)
			response, err := a.DoRequest(c, info, strings.NewReader(`{"messages":[{"role":"user","content":"hello"}],"max_tokens":128}`))
			require.NoError(t, err)
			assert.Nil(t, response)
			const wantModel = "us.anthropic.claude-haiku-4-5-20251001-v1:0"
			if test.stream {
				request, ok := a.AwsReq.(*bedrockruntime.InvokeModelWithResponseStreamInput)
				require.True(t, ok)
				assert.Equal(t, wantModel, *request.ModelId)
			} else {
				request, ok := a.AwsReq.(*bedrockruntime.InvokeModelInput)
				require.True(t, ok)
				assert.Equal(t, wantModel, *request.ModelId)
			}
		})
	}
}

func TestMantleCanceledRequestSkipsRetry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c := newAwsTestContext(httptest.NewRecorder(), ctx)
	response, err := (&Adaptor{}).DoRequest(c, newMantleTestInfo(), strings.NewReader(`{"messages":[]}`))
	require.Error(t, err)
	assert.Nil(t, response)
	var apiError *relaytypes.NewAPIError
	require.ErrorAs(t, err, &apiError)
	assert.True(t, relaytypes.IsSkipRetryError(apiError))
}

// eflow's relay client follows SSRF-validated redirects, so only the
// non-redirect half of upstream's test applies: an auth or rate-limit
// failure is returned as-is and never retried through InvokeModel.
func TestMantleDoesNotFallbackOnAuthFailure(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				assert.Equal(t, "/anthropic/v1/messages", r.URL.Path)
				w.WriteHeader(status)
			}))
			defer server.Close()
			original := http.DefaultClient
			http.DefaultClient = server.Client()
			defer func() { http.DefaultClient = original }()
			info := newMantleTestInfo()
			info.ChannelBaseUrl = server.URL
			c := newAwsTestContext(httptest.NewRecorder(), context.Background())
			a := &Adaptor{}
			response, err := a.DoRequest(c, info, strings.NewReader(`{"messages":[]}`))
			require.NoError(t, err)
			httpResponse := response.(*http.Response)
			defer httpResponse.Body.Close()
			assert.Equal(t, status, httpResponse.StatusCode)
			assert.Equal(t, 1, calls)
			assert.Nil(t, a.AwsReq)
		})
	}
}
