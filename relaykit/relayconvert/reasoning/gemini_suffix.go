package reasoning

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var legacyGeminiModelPattern = regexp.MustCompile(`^gemini-[a-z0-9][a-z0-9._-]*$`)

func ParseGeminiModelSuffix(modelName string, allowThinkingAlias bool) (string, Intent, bool, error) {
	prefix, bare := splitModelNamespace(modelName)
	if !strings.HasPrefix(bare, "gemini-") {
		return modelName, Intent{}, false, nil
	}
	base, intent, found, err := parseProviderModelSuffix(bare, "gemini-", allowThinkingAlias, true)
	if err != nil || !found || !legacyGeminiModelPattern.MatchString(base) {
		return modelName, Intent{}, false, err
	}
	return prefix + base, intent, true, nil
}

func splitModelNamespace(modelName string) (string, string) {
	if slash := strings.LastIndex(modelName, "/"); slash >= 0 {
		return modelName[:slash+1], modelName[slash+1:]
	}
	return "", modelName
}

func lastModelPathSegment(modelName string) string {
	_, bare := splitModelNamespace(modelName)
	return bare
}

func parseProviderModelSuffix(modelName string, requiredPrefix string, allowThinkingAlias bool, includeThoughts bool) (string, Intent, bool, error) {
	if allowThinkingAlias {
		if marker := strings.LastIndex(modelName, "-thinking-"); marker >= 0 {
			baseModel := modelName[:marker]
			if !strings.HasPrefix(baseModel, requiredPrefix) {
				return modelName, Intent{}, false, nil
			}
			budget, err := strconv.Atoi(modelName[marker+len("-thinking-"):])
			if err != nil {
				return modelName, Intent{}, false, fmt.Errorf("invalid thinking budget suffix on model %q: %w", modelName, err)
			}
			intent := Intent{BudgetTokens: &budget, Source: SourceSuffix, BudgetSource: SourceSuffix}
			if includeThoughts {
				value := true
				intent.IncludeThoughts = &value
			}
			return baseModel, intent, true, nil
		}
		if before, ok := strings.CutSuffix(modelName, "-nothinking"); ok {
			baseModel := before
			return baseModel, Intent{Mode: ModeDisabled, Effort: EffortNone, Source: SourceSuffix}, true, nil
		}
		if before, ok := strings.CutSuffix(modelName, "-thinking"); ok {
			baseModel := before
			intent := Intent{Mode: ModeEnabled, Source: SourceSuffix}
			if includeThoughts {
				value := true
				intent.IncludeThoughts = &value
			}
			return baseModel, intent, true, nil
		}
	}

	suffixes := []string{"-max", "-xhigh", "-high", "-medium", "-low", "-minimal", "-none"}
	baseModel, rawEffort, ok := TrimEffortSuffixWithSuffixes(modelName, suffixes)
	if !ok || !strings.HasPrefix(baseModel, requiredPrefix) {
		return modelName, Intent{}, false, nil
	}
	effort, err := ParseEffort(rawEffort)
	if err != nil {
		return modelName, Intent{}, false, err
	}
	intent := Intent{Effort: effort, Mode: ModeEnabled, Source: SourceSuffix}
	if effort == EffortNone {
		intent.Mode = ModeDisabled
	} else if includeThoughts {
		value := true
		intent.IncludeThoughts = &value
	}
	return baseModel, intent, true, nil
}
