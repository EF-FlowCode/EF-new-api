package setting

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// SensitiveSettings is one immutable runtime version of sensitive-word
// filtering. Words is caller-owned in values returned by GetSensitiveSettings.
type SensitiveSettings struct {
	CheckEnabled           bool
	CheckOnPromptEnabled   bool
	StopOnSensitive        bool
	StreamCacheQueueLength int
	Words                  []string
}

var (
	sensitiveSettings   atomic.Pointer[SensitiveSettings]
	sensitiveSettingsMu sync.Mutex
)

func init() {
	publishSensitiveSettings(SensitiveSettings{
		CheckEnabled:         true,
		CheckOnPromptEnabled: true,
		StopOnSensitive:      true,
		Words:                []string{"test_sensitive"},
	})
}

// GetSensitiveSettings returns an isolated copy of one published version.
func GetSensitiveSettings() SensitiveSettings {
	current := sensitiveSettings.Load()
	if current == nil {
		return SensitiveSettings{}
	}
	return cloneSensitiveSettings(*current)
}

// UpdateSensitiveSettings parses all supplied options before publishing one
// complete replacement. Unknown keys are ignored.
func UpdateSensitiveSettings(values map[string]string) error {
	sensitiveSettingsMu.Lock()
	defer sensitiveSettingsMu.Unlock()

	next := GetSensitiveSettings()
	boolOptions := []struct {
		key    string
		target *bool
	}{
		{key: "CheckSensitiveEnabled", target: &next.CheckEnabled},
		{key: "CheckSensitiveOnPromptEnabled", target: &next.CheckOnPromptEnabled},
		{key: "StopOnSensitiveEnabled", target: &next.StopOnSensitive},
	}
	for _, option := range boolOptions {
		value, ok := values[option.key]
		if !ok {
			continue
		}
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("%s must be a boolean: %w", option.key, err)
		}
		*option.target = parsed
	}
	if value, ok := values["StreamCacheQueueLength"]; ok {
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("StreamCacheQueueLength must be an integer: %w", err)
		}
		next.StreamCacheQueueLength = parsed
	}
	if value, ok := values["SensitiveWords"]; ok {
		next.Words = parseSensitiveWords(value)
	}

	publishSensitiveSettings(next)
	return nil
}

func (settings SensitiveSettings) ShouldCheckPrompt() bool {
	return settings.CheckEnabled && settings.CheckOnPromptEnabled
}

func (settings SensitiveSettings) WordsString() string {
	return strings.Join(settings.Words, "\n")
}

func parseSensitiveWords(value string) []string {
	words := make([]string, 0)
	for word := range strings.SplitSeq(value, "\n") {
		word = strings.TrimSpace(word)
		if word != "" {
			words = append(words, word)
		}
	}
	return words
}

func cloneSensitiveSettings(settings SensitiveSettings) SensitiveSettings {
	settings.Words = append([]string(nil), settings.Words...)
	return settings
}

func publishSensitiveSettings(next SensitiveSettings) {
	owned := cloneSensitiveSettings(next)
	sensitiveSettings.Store(&owned)
}
