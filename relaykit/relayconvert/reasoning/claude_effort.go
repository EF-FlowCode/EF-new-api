package reasoning

import "strings"

// claudeEffortLevels lists which of the upper effort levels a Claude model
// accepts in output_config.effort
// (https://platform.claude.com/docs/en/build-with-claude/effort).
type claudeEffortLevels struct {
	known         bool
	supportsXHigh bool
	supportsMax   bool
}

func claudeEffortLevelsFor(model string) claudeEffortLevels {
	model = strings.ToLower(lastModelPathSegment(model))
	switch {
	case strings.HasPrefix(model, "claude-fable-5"),
		strings.HasPrefix(model, "claude-mythos-5"),
		strings.HasPrefix(model, "claude-opus-5"),
		strings.HasPrefix(model, "claude-sonnet-5"),
		strings.HasPrefix(model, "claude-haiku-5"),
		strings.HasPrefix(model, "claude-opus-4-8"),
		strings.HasPrefix(model, "claude-opus-4-7"):
		return claudeEffortLevels{known: true, supportsXHigh: true, supportsMax: true}
	case strings.HasPrefix(model, "claude-mythos-preview"),
		strings.HasPrefix(model, "claude-opus-4-6"),
		strings.HasPrefix(model, "claude-sonnet-4-6"):
		return claudeEffortLevels{known: true, supportsMax: true}
	case strings.HasPrefix(model, "claude-opus-4-5"):
		return claudeEffortLevels{known: true}
	}
	return claudeEffortLevels{}
}

// ClaudeEffort returns the output_config.effort level a known Claude model
// accepts in place of the requested one: minimal becomes low, xhigh becomes
// max (or high) where the model lacks xhigh, and max becomes high where the
// model lacks max. Models missing from the table keep the requested value.
func ClaudeEffort(model string, effort string) string {
	levels := claudeEffortLevelsFor(model)
	if !levels.known {
		return effort
	}
	switch Effort(strings.ToLower(strings.TrimSpace(effort))) {
	case EffortMinimal:
		return string(EffortLow)
	case EffortXHigh:
		if levels.supportsXHigh {
			return string(EffortXHigh)
		}
		if levels.supportsMax {
			return string(EffortMax)
		}
		return string(EffortHigh)
	case EffortMax:
		if levels.supportsMax {
			return string(EffortMax)
		}
		return string(EffortHigh)
	}
	return effort
}
