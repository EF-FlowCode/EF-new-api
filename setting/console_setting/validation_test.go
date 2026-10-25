package console_setting

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExceedsMaxCharactersUsesUTF16CodeUnits(t *testing.T) {
	// A supplementary-plane character occupies two UTF-16 code units. This
	// mirrors the browser-side length limit used by the console settings UI.
	require.False(t, exceedsMaxCharacters("😀", 2))
	require.True(t, exceedsMaxCharacters("😀", 1))
	require.True(t, exceedsMaxCharacters(strings.Repeat("😀", 2), 3))
}

func TestValidateAnnouncementsUsesUTF16Length(t *testing.T) {
	content := strings.Repeat("😀", 251) // 502 UTF-16 code units
	settings := `[{"content":"` + content + `","publishDate":"2026-01-01T00:00:00Z"}]`
	require.ErrorContains(t, validateAnnouncements(settings), "内容长度不能超过500")
}
