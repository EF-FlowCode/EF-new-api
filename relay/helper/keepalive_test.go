package helper

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	relaycommon "eflow.local/new-api/relay/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newKeepAliveContext(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
	return c, recorder
}

func waitForPing(t *testing.T, c *gin.Context) {
	t.Helper()
	require.Eventually(t, c.Writer.Written, time.Second, time.Millisecond)
}

func TestStreamKeepAlivePingIsNotClientOutput(t *testing.T) {
	c, recorder := newKeepAliveContext(t)
	stop := StartStreamKeepAlive(c, time.Millisecond)
	defer stop()

	waitForPing(t, c)
	stop()
	assert.True(t, strings.HasPrefix(recorder.Body.String(), ": PING\n\n"))
	assert.Equal(t, "text/event-stream", recorder.Header().Get("Content-Type"))
	assert.False(t, relaycommon.ResponseOutputWritten(c), "pings commit headers but are not output")

	require.NoError(t, StringData(c, "chunk"))
	assert.True(t, relaycommon.ResponseOutputWritten(c))
	assert.True(t, strings.HasSuffix(recorder.Body.String(), "data: chunk\n\n"))
}

func TestStreamKeepAliveWaitsOneIntervalBeforeFirstPing(t *testing.T) {
	c, recorder := newKeepAliveContext(t)
	stop := StartStreamKeepAlive(c, time.Hour)
	stop()
	assert.False(t, c.Writer.Written())
	assert.Empty(t, recorder.Body.String())
}

func TestStreamKeepAliveBusinessWriteStopsPings(t *testing.T) {
	c, recorder := newKeepAliveContext(t)
	stop := StartStreamKeepAlive(c, time.Millisecond)
	defer stop()

	waitForPing(t, c)
	require.NoError(t, StringData(c, "chunk"))
	body := recorder.Body.String()
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, body, recorder.Body.String(), "no ping may follow the first business write")
}

func TestStreamKeepAliveInnerStartReusesOuter(t *testing.T) {
	c, _ := newKeepAliveContext(t)
	stopOuter := StartStreamKeepAlive(c, time.Millisecond)
	defer stopOuter()
	outer := c.Writer.(*streamKeepAliveWriter)

	StartStreamKeepAlive(c, time.Millisecond)()
	assert.Same(t, outer, c.Writer)
	select {
	case <-outer.done:
		t.Fatal("inner cleanup must not end the outer keepalive")
	default:
	}
}

func TestStreamKeepAliveSkipsCancelledRequest(t *testing.T) {
	c, _ := newKeepAliveContext(t)
	ctx, cancel := context.WithCancel(c.Request.Context())
	cancel()
	c.Request = c.Request.WithContext(ctx)

	StartStreamKeepAlive(c, time.Millisecond)()
	_, wrapped := c.Writer.(*streamKeepAliveWriter)
	assert.False(t, wrapped, "a cancelled request starts no keepalive")
}
