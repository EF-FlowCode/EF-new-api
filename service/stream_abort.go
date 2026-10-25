package service

import (
	"fmt"
	"net/http"

	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/types"

	"github.com/gin-gonic/gin"
)

// StreamAbortRetryError returns a retryable 503 error when the stream ended
// abnormally due to server-side causes before any data was sent to the client.
// The HTTP response has not been committed, so the retry loop can try another
// channel transparently.
//
// Returns nil when:
//   - the request is not a stream
//   - the stream ended normally
//   - data was already sent (retry impossible — HTTP response committed; the
//     caller settles what was delivered instead of refunding it)
//   - the client disconnected (not a server fault)
func StreamAbortRetryError(c *gin.Context, info *relaycommon.RelayInfo) *types.NewAPIError {
	if !info.IsStream || info.StreamStatus == nil {
		return nil
	}
	if info.StreamStatus.IsServerSideError() && !info.ClientReceivedOutput(c) {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("stream failed before sending data: %s", info.StreamStatus.Summary()),
			types.ErrorCodeBadResponse,
			http.StatusServiceUnavailable,
		)
	}
	return nil
}
