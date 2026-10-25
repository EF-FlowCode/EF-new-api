package service

import (
	"testing"

	"eflow.local/new-api/setting"
	"github.com/stretchr/testify/assert"
)

func TestCheckSensitiveTextWithSettingsUsesHeldVersion(t *testing.T) {
	held := setting.SensitiveSettings{
		CheckEnabled:         true,
		CheckOnPromptEnabled: true,
		Words:                []string{"alpha"},
	}
	replacement := setting.SensitiveSettings{
		CheckEnabled:         true,
		CheckOnPromptEnabled: true,
		Words:                []string{"beta"},
	}

	matched, _ := CheckSensitiveTextWithSettings("contains alpha", held)
	assert.True(t, matched)
	matched, _ = CheckSensitiveTextWithSettings("contains beta", held)
	assert.False(t, matched)
	matched, _ = CheckSensitiveTextWithSettings("contains beta", replacement)
	assert.True(t, matched)

	held.CheckEnabled = false
	matched, _ = CheckSensitiveTextWithSettings("contains alpha", held)
	assert.False(t, matched)
}
