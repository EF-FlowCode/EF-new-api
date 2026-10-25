package constant

import (
	"fmt"
	"regexp"
	"strings"
)

var emailTemplatePlaceholder = regexp.MustCompile(`\{\{\s*([a-z_][a-z0-9_]*)\s*\}\}`)

// ValidateEmailTemplateField 只校验使用纯文本变量的新模板；空值表示恢复默认。
// 旧模板的 HTML 片段和未知占位符语义保持不变。
func ValidateEmailTemplateField(spec EmailTemplateSpec, field, value string) error {
	if !spec.PlainTextVariables || value == "" {
		return nil
	}
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s 的 %s 不能只包含空白", spec.Name, field)
	}
	if field == "subject" && strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%s 的主题不能包含换行", spec.Name)
	}
	allowed := make(map[string]bool, len(spec.Variables))
	for _, variable := range spec.Variables {
		allowed[variable.Name] = true
	}
	seen := make(map[string]bool)
	for _, match := range emailTemplatePlaceholder.FindAllStringSubmatch(value, -1) {
		if !allowed[match[1]] {
			return fmt.Errorf("%s 包含未知变量 {{%s}}", spec.Name, match[1])
		}
		seen[match[1]] = true
	}
	// 单个花括号允许用于 CSS，未匹配的双花括号及嵌套花括号拒绝。
	if strings.Contains(value, "{{{") || strings.Contains(value, "}}}") {
		return fmt.Errorf("%s 的变量格式错误，请使用 {{变量名}}", spec.Name)
	}
	rest := emailTemplatePlaceholder.ReplaceAllString(value, "")
	if strings.Contains(rest, "{{") || strings.Contains(rest, "}}") {
		return fmt.Errorf("%s 的变量格式错误，请使用 {{变量名}}", spec.Name)
	}
	if field == "body" {
		for _, name := range spec.RequiredBodyVariables {
			if !seen[name] {
				return fmt.Errorf("%s 正文必须包含 {{%s}}", spec.Name, name)
			}
		}
	}
	return nil
}

func ValidateEmailTemplateOption(key, value string) error {
	if !strings.HasPrefix(key, EmailTemplateOptionPrefix) {
		return nil
	}
	templateKey, field, ok := strings.Cut(strings.TrimPrefix(key, EmailTemplateOptionPrefix), ".")
	if !ok || (field != "subject" && field != "body") {
		return nil
	}
	if spec, found := FindEmailTemplateSpec(templateKey); found {
		return ValidateEmailTemplateField(spec, field, value)
	}
	return nil
}
