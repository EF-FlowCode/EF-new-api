package controller

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"
	"eflow.local/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestDownloadHistoricalHTMLAttachmentNeverRenders replays the reported stored
// XSS: an attachment recorded as text/html before uploads rejected active
// content, opened by an administrator with ?inline=1. The response must be a
// sandboxed plain-text download, never a same-origin HTML document.
func TestDownloadHistoricalHTMLAttachmentNeverRenders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testdb.OpenMySQL8(t, &model.TicketAttachment{})
	previous := model.DB
	model.DB = db
	t.Cleanup(func() { model.DB = previous })

	root := t.TempDir()
	original := setting.GetTicketAttachmentSettings()
	require.NoError(t, setting.UpdateTicketAttachmentSettings(map[string]string{"TicketAttachmentStorage": "local", "TicketAttachmentLocalPath": root}))
	t.Cleanup(func() {
		require.NoError(t, setting.UpdateTicketAttachmentSettings(map[string]string{
			"TicketAttachmentStorage": original.Storage, "TicketAttachmentLocalPath": original.LocalPath}))
	})

	payload := []byte("<script>fetch('/api/user/self')</script>")
	require.NoError(t, os.WriteFile(filepath.Join(root, "poc.txt"), payload, 0o600))
	cases := []struct {
		mime, file, contentType, disposition string
	}{
		{"text/html", "poc.txt", "text/plain; charset=utf-8", "attachment"},
		{"application/xhtml+xml", "poc.txt", "application/octet-stream", "attachment"},
		{"image/svg+xml", "poc.txt", "application/octet-stream", "attachment"},
		{"text/plain", "poc.txt", "text/plain; charset=utf-8", "inline"},
	}
	for _, tc := range cases {
		record := &model.TicketAttachment{UserId: 7, TicketId: 3, MessageId: 4, FileName: tc.file, StoredName: tc.file,
			MimeType: tc.mime, Size: int64(len(payload)), StorageType: "local", StorageKey: "poc.txt", Sha256: "00"}
		require.NoError(t, model.CreateTicketAttachment(record))

		engine := gin.New()
		engine.GET("/api/ticket/attachment/:id", func(c *gin.Context) {
			c.Set("id", 1)
			c.Set("role", common.RoleRootUser)
			DownloadTicketAttachment(c)
		})
		recorder := httptest.NewRecorder()
		engine.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/ticket/attachment/"+strconv.Itoa(record.Id)+"?inline=1", nil))

		require.Equal(t, http.StatusOK, recorder.Code, tc.mime)
		header := recorder.Header()
		require.Equal(t, tc.contentType, header.Get("Content-Type"), tc.mime)
		require.Contains(t, header.Get("Content-Disposition"), tc.disposition+";", tc.mime)
		require.Equal(t, "sandbox", header.Get("Content-Security-Policy"), tc.mime)
		require.Equal(t, "nosniff", header.Get("X-Content-Type-Options"), tc.mime)
	}
}
