package service

import (
	"fmt"
	"net/http"

	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
)

// StreamAbortRetryError returns a retryable 503 error when the stream ended
// abnormally due to server-side causes before any data was sent to the client.
// The HTTP response has not been committed, so the retry loop can try another
// channel transparently.
//
// Returns nil when:
//   - the request is not a stream
//   - the stream ended normally
//   - data was already sent (retry impossible — HTTP response committed)
//   - the client disconnected (not a server fault)
func StreamAbortRetryError(info *relaycommon.RelayInfo) *types.NewAPIError {
	if !info.IsStream || info.StreamStatus == nil {
		return nil
	}
	if info.StreamStatus.IsServerSideError() && info.SendResponseCount == 0 {
		return types.NewErrorWithStatusCode(
			fmt.Errorf("stream failed before sending data: %s", info.StreamStatus.Summary()),
			types.ErrorCodeBadResponse,
			http.StatusServiceUnavailable,
		)
	}
	return nil
}
