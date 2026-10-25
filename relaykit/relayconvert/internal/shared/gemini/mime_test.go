package gemini

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestIsSupportedMimeTypeFollowsGeminiBlobList covers the inline-data MIME
// allowlist taken from the Gemini API Blob.mimeType reference.
func TestIsSupportedMimeTypeFollowsGeminiBlobList(t *testing.T) {
	for _, mimeType := range []string{"image/gif", "image/avif", "IMAGE/PNG", "audio/ogg", "video/webm", "text/markdown", "application/json", "application/pdf"} {
		assert.True(t, IsSupportedMimeType(mimeType), mimeType)
	}
	for _, mimeType := range []string{"application/zip", "image/svg+xml", ""} {
		assert.False(t, IsSupportedMimeType(mimeType), mimeType)
	}
	assert.Contains(t, SupportedMimeTypesList(), "audio/*")
}
