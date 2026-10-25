package helper

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/dto"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type failingResponsesWriter struct{ gin.ResponseWriter }

func (w failingResponsesWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestResponseChunkDataWriteFailure(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Writer = failingResponsesWriter{c.Writer}
	err := ResponseChunkData(c, dto.ResponsesStreamResponse{Type: "response.created"}, `{"type":"response.created"}`)
	require.ErrorIs(t, err, io.ErrClosedPipe)
}
