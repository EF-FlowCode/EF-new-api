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
		{"application/json", true},
		{"application/xml", true},
		{"text/plain", true},
		{"text/csv", true},
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
