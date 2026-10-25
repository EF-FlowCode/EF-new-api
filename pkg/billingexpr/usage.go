package billingexpr

import (
	"fmt"
	"maps"
	"sort"
	"strings"

	"eflow.local/new-api/pkg/jsplugin"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/ast"
	"github.com/expr-lang/expr/parser"
)

// Inspect before optimization so an unselected branch cannot hide an invalid key.
func expressionUsageKeys(body string) (map[string]bool, error) {
	tree, err := parser.Parse(body)
	if err != nil {
		return nil, err
	}
	keys := map[string]bool{}
	invalid, identifiers, calls := false, 0, 0
	environments, staticMembers := 0, 0
	ast.Find(tree.Node, func(node ast.Node) bool {
		if id, ok := node.(*ast.IdentifierNode); ok && id.Value == "$env" {
			environments++
		}
		if member, ok := node.(*ast.MemberNode); ok {
			if id, ok := member.Node.(*ast.IdentifierNode); ok && id.Value == "$env" {
				if key, ok := member.Property.(*ast.StringNode); ok && key.Value != "u" {
					staticMembers++
				}
			}
		}
		if id, ok := node.(*ast.IdentifierNode); ok && id.Value == "u" {
			identifiers++
		}
		call, ok := node.(*ast.CallNode)
		if !ok {
			return false
		}
		id, ok := call.Callee.(*ast.IdentifierNode)
		if !ok || id.Value != "u" {
			return false
		}
		calls++
		if len(call.Arguments) != 1 {
			invalid = true
			return false
		}
		key, ok := call.Arguments[0].(*ast.StringNode)
		if !ok || key.Value == "" || strings.TrimSpace(key.Value) != key.Value {
			invalid = true
			return false
		}
		keys[key.Value] = true
		return false
	})
	if invalid || identifiers != calls || environments != staticMembers {
		return nil, fmt.Errorf("u() requires a literal canonical usage field name")
	}
	return keys, nil
}

func UsedUsageKeys(expression string) (map[string]bool, error) {
	if strings.TrimSpace(expression) == "" {
		return nil, nil
	}
	if _, err := CompileFromCache(expression); err != nil {
		return nil, err
	}
	cacheMu.RLock()
	entry := cache[ExprHashString(expression)]
	if entry != nil {
		keys := maps.Clone(entry.usageKeys)
		cacheMu.RUnlock()
		return keys, nil
	}
	cacheMu.RUnlock()
	_, body := ParseExprVersion(expression)
	return expressionUsageKeys(body)
}

type usageTypePatch map[string]string

func (p usageTypePatch) Visit(node *ast.Node) {
	call, ok := (*node).(*ast.CallNode)
	if !ok {
		return
	}
	id, ok := call.Callee.(*ast.IdentifierNode)
	if !ok || id.Value != "u" || len(call.Arguments) != 1 {
		return
	}
	key, ok := call.Arguments[0].(*ast.StringNode)
	if !ok {
		return
	}
	if name, ok := p[key.Value]; ok {
		ast.Patch(node, &ast.IdentifierNode{Value: name})
	}
}

// Type-check every u() call against the selected model profile, including dead
// branches. The real program still reads facts, not schema examples or prices.
func ValidateUsageExpression(expression string, schema map[string]jsplugin.UsageFieldSchema) error {
	keys, err := UsedUsageKeys(expression)
	if err != nil || len(keys) == 0 {
		return err
	}
	env := maps.Clone(compileEnvPrototypeV1)
	patch := usageTypePatch{}
	names := make([]string, 0, len(keys))
	for key := range keys {
		names = append(names, key)
	}
	sort.Strings(names)
	for i, key := range names {
		field, exists := schema[key]
		if !exists {
			return fmt.Errorf("usage field %q is not declared by the bound model", key)
		}
		name := fmt.Sprintf("__billing_usage_%d", i)
		patch[key] = name
		switch {
		case len(field.Enum) > 0:
			env[name] = ""
		case field.Type == "boolean":
			env[name] = false
		case field.Type == "number":
			env[name] = float64(0)
		default:
			return fmt.Errorf("unsupported usage field type for %q", key)
		}
	}
	_, body := ParseExprVersion(expression)
	_, err = expr.Compile(body, expr.Env(env), expr.Patch(patch), expr.AsFloat64())
	return err
}

func validateRequestUsage(expression string, request RequestInput) error {
	keys, err := UsedUsageKeys(expression)
	if err != nil || len(keys) == 0 {
		return err
	}
	if err := ValidateUsageExpression(expression, request.UsageSchema); err != nil {
		return err
	}
	if err := jsplugin.ValidateUsageFacts(request.UsageSchema, request.Usage); err != nil {
		return err
	}
	for key := range keys {
		if value, ok := request.Usage[key]; !ok || value == nil {
			return fmt.Errorf("usage field %q is missing", key)
		}
	}
	return nil
}
