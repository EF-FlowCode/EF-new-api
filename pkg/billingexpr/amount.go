package billingexpr

import (
	"fmt"
	"strings"
)

// AmountExpression upgrades a legacy expression without altering its internal
// arithmetic, conditions or request inputs. v2 always returns currency amounts.
// Untagged input remains a legacy import; new expressions must use v2 explicitly.
func AmountExpression(expression string) (string, error) {
	if strings.TrimSpace(expression) == "" {
		return expression, nil
	}
	expression = strings.TrimSpace(expression)
	if _, err := CompileFromCache(expression); err != nil {
		return "", err
	}
	version, body := ParseExprVersion(expression)
	if version == 2 {
		return expression, nil
	}
	// The newline also terminates a possible trailing line comment.
	upgraded := "v2:(" + body + "\n) / 1000000"
	if _, err := CompileFromCache(upgraded); err != nil {
		return "", fmt.Errorf("upgrade expression: %w", err)
	}
	return upgraded, nil
}

// ExpressionAmount is the only legacy-unit boundary. It is retained for old
// frozen snapshots and imports; stored and newly generated rules use v2.
func ExpressionAmount(output float64, version int) float64 {
	if version == 2 {
		return output
	}
	return output / 1_000_000
}

func ExpressionQuota(output float64, expression string, quotaPerUnit float64) float64 {
	version, _ := ParseExprVersion(expression)
	return ExpressionAmount(output, version) * quotaPerUnit
}
