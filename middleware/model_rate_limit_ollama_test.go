package middleware

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/relay/channel/ollama"
	relaycommon "eflow.local/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type ollamaReadError struct{}

func (ollamaReadError) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestModelSuccessLimitOllama(t *testing.T) {
	for _, backend := range []string{"memory", "redis"} {
		for _, failure := range []string{"truncated", "read_error", "upstream_error"} {
			t.Run(backend+"/"+failure, func(t *testing.T) {
				limit := modelLimitBackend(t, backend, 60, 0, 1)
				attempts := 0
				router := modelLimitRouter(limit, func(c *gin.Context) {
					attempts++
					var reader io.Reader = strings.NewReader(`{"done":true,"prompt_eval_count":5,"eval_count":7}`)
					if attempts == 1 {
						switch failure {
						case "truncated":
							reader = strings.NewReader(`{"message":{"content":"partial"}}`)
						case "read_error":
							reader = ollamaReadError{}
						case "upstream_error":
							// An error before any output is returned as a retryable relay
							// error; after output it is a failed 200 stream like the others.
							reader = strings.NewReader("{\"message\":{\"content\":\"partial\"}}\n{\"error\":\"provider failed\"}")
						}
					}
					info := &relaycommon.RelayInfo{IsStream: true, ChannelMeta: &relaycommon.ChannelMeta{UpstreamModelName: "model"}}
					_, apiErr := (&ollama.Adaptor{}).DoResponse(c, &http.Response{Body: io.NopCloser(reader)}, info)
					require.Nil(t, apiErr)
					require.Equal(t, attempts == 1, info.StreamStatus.ResponseFailed())
					// Settlement can succeed while delivery failed after HTTP 200.
					common.SetContextKey(c, constant.ContextKeyRelaySucceeded, true)
				})
				require.Equal(t, 200, doModelLimitRequest(router))
				require.Equal(t, 200, doModelLimitRequest(router), "failed stream releases its reservation")
				require.Equal(t, 429, doModelLimitRequest(router), "normal done consumes one success")
				require.Equal(t, 2, attempts)
			})
		}
	}
}
