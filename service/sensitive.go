package service

import (
	"errors"
	"strings"

	"eflow.local/new-api/dto"
	"eflow.local/new-api/setting"
)

func CheckSensitiveMessages(messages []dto.Message) ([]string, error) {
	if len(messages) == 0 {
		return nil, nil
	}
	settings := setting.GetSensitiveSettings()

	for _, message := range messages {
		arrayContent := message.ParseContent()
		for _, m := range arrayContent {
			if m.Type == "image_url" {
				// TODO: check image url
				continue
			}
			// 检查 text 是否为空
			if m.Text == "" {
				continue
			}
			if ok, words := sensitiveWordContains(m.Text, settings.Words); ok {
				return words, errors.New("sensitive words detected")
			}
		}
	}
	return nil, nil
}

func CheckSensitiveText(text string) (bool, []string) {
	return CheckSensitiveTextWithSettings(text, setting.GetSensitiveSettings())
}

// CheckSensitiveTextWithSettings keeps the enablement decision and dictionary
// on the same runtime version for request paths that already hold a snapshot.
func CheckSensitiveTextWithSettings(text string, settings setting.SensitiveSettings) (bool, []string) {
	if !settings.ShouldCheckPrompt() {
		return false, nil
	}
	return sensitiveWordContains(text, settings.Words)
}

// SensitiveWordContains 是否包含敏感词，返回是否包含敏感词和敏感词列表
func SensitiveWordContains(text string) (bool, []string) {
	return sensitiveWordContains(text, setting.GetSensitiveSettings().Words)
}

func sensitiveWordContains(text string, words []string) (bool, []string) {
	if len(words) == 0 {
		return false, nil
	}
	if len(text) == 0 {
		return false, nil
	}
	checkText := strings.ToLower(text)
	return AcSearch(checkText, words, true)
}

// SensitiveWordReplace 敏感词替换，返回是否包含敏感词和替换后的文本
func SensitiveWordReplace(text string, returnImmediately bool) (bool, []string, string) {
	settings := setting.GetSensitiveSettings()
	if len(settings.Words) == 0 {
		return false, nil, text
	}
	checkText := strings.ToLower(text)
	m := getOrBuildAC(settings.Words)
	hits := m.MultiPatternSearch([]rune(checkText), returnImmediately)
	if len(hits) > 0 {
		words := make([]string, 0, len(hits))
		var builder strings.Builder
		builder.Grow(len(text))
		lastPos := 0

		for _, hit := range hits {
			pos := hit.Pos
			word := string(hit.Word)
			builder.WriteString(text[lastPos:pos])
			builder.WriteString("**###**")
			lastPos = pos + len(word)
			words = append(words, word)
		}
		builder.WriteString(text[lastPos:])
		return true, words, builder.String()
	}
	return false, nil, text
}
