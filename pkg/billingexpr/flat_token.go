package billingexpr

import (
	"math"
	"strings"

	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

var flatTokenBillingVars = map[string]struct{}{
	"p":     {},
	"c":     {},
	"cr":    {},
	"cc":    {},
	"cc1h":  {},
	"img":   {},
	"img_o": {},
	"ai":    {},
	"ao":    {},
}

// FlatTokenBillingExpr is the strict single-tier expression shape used by
// flat_price. Coefficients are absolute USD prices per one million tokens.
type FlatTokenBillingExpr struct {
	Label        string
	Coefficients map[string]float64
}

// ParseFlatTokenBillingExpr accepts only a complete expression shaped as:
//
//	tier("label", p * 1 + c * 2 + cr * 0.1 + cc * 1.25 + cc1h * 2)
//
// It intentionally rejects conditions, request probes, extra arithmetic,
// duplicate variables, and expressions missing p or c.
func ParseFlatTokenBillingExpr(exprStr string) (FlatTokenBillingExpr, bool) {
	trimmed := strings.TrimSpace(exprStr)
	if trimmed == "" {
		return FlatTokenBillingExpr{}, false
	}
	version, body := ParseExprVersion(trimmed)
	tree, err := parser.Parse(strings.TrimSpace(body))
	if err != nil {
		return FlatTokenBillingExpr{}, false
	}

	node := tree.Node
	scale := 1.0
	if version == 2 {
		scale = 1_000_000
		// Preserve the exact legacy coefficient representation after migration.
		if divide, ok := node.(*ast.BinaryNode); ok && divide.Operator == "/" {
			denominator, valid := nonNegativeNumericLiteral(divide.Right)
			if !valid || denominator != 1_000_000 {
				return FlatTokenBillingExpr{}, false
			}
			node, scale = divide.Left, 1
		}
	}
	call, ok := node.(*ast.CallNode)
	if !ok || len(call.Arguments) != 2 {
		return FlatTokenBillingExpr{}, false
	}
	callee, ok := call.Callee.(*ast.IdentifierNode)
	if !ok || callee.Value != "tier" {
		return FlatTokenBillingExpr{}, false
	}
	label, ok := call.Arguments[0].(*ast.StringNode)
	if !ok {
		return FlatTokenBillingExpr{}, false
	}

	coefficients := make(map[string]float64, len(flatTokenBillingVars))
	if !parseFlatTokenTerms(call.Arguments[1], coefficients) {
		return FlatTokenBillingExpr{}, false
	}
	if _, ok := coefficients["p"]; !ok {
		return FlatTokenBillingExpr{}, false
	}
	if _, ok := coefficients["c"]; !ok {
		return FlatTokenBillingExpr{}, false
	}
	for key, value := range coefficients {
		coefficients[key] = value * scale
		if math.IsInf(coefficients[key], 0) {
			return FlatTokenBillingExpr{}, false
		}
	}
	return FlatTokenBillingExpr{Label: label.Value, Coefficients: coefficients}, true
}

func parseFlatTokenTerms(node ast.Node, coefficients map[string]float64) bool {
	if binary, ok := node.(*ast.BinaryNode); ok && binary.Operator == "+" {
		return parseFlatTokenTerms(binary.Left, coefficients) &&
			parseFlatTokenTerms(binary.Right, coefficients)
	}

	term, ok := node.(*ast.BinaryNode)
	if !ok || term.Operator != "*" {
		return false
	}
	variable, ok := term.Left.(*ast.IdentifierNode)
	if !ok {
		return false
	}
	if _, allowed := flatTokenBillingVars[variable.Value]; !allowed {
		return false
	}
	if _, duplicate := coefficients[variable.Value]; duplicate {
		return false
	}

	coefficient, ok := nonNegativeNumericLiteral(term.Right)
	if !ok {
		return false
	}
	coefficients[variable.Value] = coefficient
	return true
}

func nonNegativeNumericLiteral(node ast.Node) (float64, bool) {
	var value float64
	switch number := node.(type) {
	case *ast.IntegerNode:
		value = float64(number.Value)
	case *ast.FloatNode:
		value = number.Value
	default:
		return 0, false
	}
	if value < 0 || math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	return value, true
}
