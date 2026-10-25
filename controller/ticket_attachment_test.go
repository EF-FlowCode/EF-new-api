package controller

import "testing"

func TestIsPreviewableMime(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"image/png", true},
		{"image/jpeg; charset=binary", true},
		{"image/svg+xml", false}, // 硬性禁 svg 预览
		{"text/plain", true},
		{"text/html", false}, // 同源 inline 会执行脚本
		{"application/xml", false},
		{"application/json", false},
		{"text/csv", false},
		{"application/pdf", false}, // pdf 下载而非 inline
		{"", false},
		{"application/octet-stream", false},
	}
	for _, c := range cases {
		if got := isPreviewableMime(c.in); got != c.want {
			t.Errorf("isPreviewableMime(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestAttachmentResponseContentTypeNeverServesActiveDocuments(t *testing.T) {
	cases := map[string]string{
		"image/png":             "image/png",
		"application/pdf":       "application/pdf",
		"text/plain":            "text/plain; charset=utf-8",
		"text/html":             "text/plain; charset=utf-8",
		"application/json":      "text/plain; charset=utf-8",
		"application/xml":       "application/octet-stream",
		"application/xhtml+xml": "application/octet-stream",
		"image/svg+xml":         "application/octet-stream",
	}
	for in, want := range cases {
		if got := attachmentResponseContentType(in); got != want {
			t.Errorf("attachmentResponseContentType(%q) = %q, want %q", in, got, want)
		}
	}
}
