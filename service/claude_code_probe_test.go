package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/model"
	"github.com/stretchr/testify/require"
)

var claudeCodePromptPattern = regexp.MustCompile(`Compute (\d+)\*(\d+), then write the string ([0-9a-f]+) reversed`)

func newClaudeCodeProbeServer(t *testing.T, canned bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/run", r.URL.Path)
		require.Equal(t, "probe-secret", r.Header.Get("X-Probe-Secret"))
		raw, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var req claudeCodeProbeRequest
		require.NoError(t, common.Unmarshal(raw, &req))
		require.Equal(t, "sk-upstream", req.APIKey)
		require.Equal(t, "claude-upstream", req.Model)
		require.Equal(t, "X-Team: eflow", req.CustomHeaders)
		match := claudeCodePromptPattern.FindStringSubmatch(req.Prompt)
		require.Len(t, match, 4)
		a, _ := strconv.Atoi(match[1])
		b, _ := strconv.Atoi(match[2])
		answer := fmt.Sprintf("%d %s", a*b, reverseString(match[3]))
		if canned {
			answer = "ok"
		}
		_, _ = fmt.Fprintf(w, `{"ok":true,"result":%q,"duration_ms":2251,"version":"2.1.316 (Claude Code)"}`, answer)
	}))
}

func claudeCodeTestChannel() *model.Channel {
	baseURL := "https://upstream.example"
	mapping := `{"claude-public":"claude-upstream"}`
	headers := `{"X-Team":"eflow","*":true}`
	return &model.Channel{
		Type:           constant.ChannelTypeAnthropic,
		Key:            "sk-upstream",
		BaseURL:        &baseURL,
		Models:         "claude-public",
		ModelMapping:   &mapping,
		HeaderOverride: &headers,
		Status:         1,
	}
}

func TestRunClaudeCodeChannelTestVerifiesAnswer(t *testing.T) {
	server := newClaudeCodeProbeServer(t, false)
	defer server.Close()
	t.Setenv("CC_PROBE_URL", server.URL)
	t.Setenv("CC_PROBE_SECRET", "probe-secret")

	result, err := RunClaudeCodeChannelTest(context.Background(), claudeCodeTestChannel(), "claude-public")
	require.NoError(t, err)
	require.Equal(t, "2.1.316 (Claude Code)", result.Version)
}

func TestRunClaudeCodeChannelTestRejectsCannedReply(t *testing.T) {
	server := newClaudeCodeProbeServer(t, true)
	defer server.Close()
	t.Setenv("CC_PROBE_URL", server.URL)
	t.Setenv("CC_PROBE_SECRET", "probe-secret")

	_, err := RunClaudeCodeChannelTest(context.Background(), claudeCodeTestChannel(), "claude-public")
	require.ErrorContains(t, err, "预设回复")
}

func TestRunClaudeCodeChannelTestRequiresService(t *testing.T) {
	t.Setenv("CC_PROBE_URL", "")
	t.Setenv("CC_PROBE_SECRET", "")
	_, err := RunClaudeCodeChannelTest(context.Background(), claudeCodeTestChannel(), "claude-public")
	require.ErrorContains(t, err, "CC_PROBE_URL")
}
