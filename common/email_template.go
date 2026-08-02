package common

import (
	"fmt"
	"html"
	"strings"
)

// EmailTemplateRow 表示邮件正文里的一条"键：值"行
type EmailTemplateRow struct {
	Label string
	Value string
}

// SystemNameOrDefault 返回配置的系统名，为空时回落到 "New API"
func SystemNameOrDefault() string {
	if SystemName != "" {
		return SystemName
	}
	return "New API"
}

// EscapeAndBreak 先 HTML 转义再把 \n 换成 <br/>，常用于用户输入的预览
func EscapeAndBreak(s string) string {
	escaped := html.EscapeString(s)
	return strings.ReplaceAll(escaped, "\n", "<br/>")
}

// RenderInfoTableHTML 把一组键值对渲染成邮件里使用的信息表 HTML。Value 视为已转义过的安全 HTML。
//
// 样式参考 Apple 账单表格：左侧 label #8e8e93，右侧 value #1d1d1f 靠右对齐，行间细分割线。
func RenderInfoTableHTML(rows []EmailTemplateRow) string {
	if len(rows) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(`<table style="width:100%;border-collapse:collapse;font-size:14px;margin:0 0 8px;">`)
	for i, r := range rows {
		border := "border-top:1px solid #f0f0f0;"
		if i == 0 {
			border = ""
		}
		fmt.Fprintf(&sb,
			`<tr><td style="padding:12px 0;color:#8e8e93;vertical-align:top;white-space:nowrap;%s">%s</td><td style="padding:12px 0;color:#1d1d1f;text-align:right;word-break:break-all;%s">%s</td></tr>`,
			border, html.EscapeString(r.Label),
			border, r.Value,
		)
	}
	sb.WriteString(`</table>`)
	return sb.String()
}

// RenderPreviewBlockHTML 将预览正文包装成带标题的预览块；若 previewHTML 为空，返回空串。
func RenderPreviewBlockHTML(title, previewHTML string) string {
	if previewHTML == "" {
		return ""
	}
	if title == "" {
		title = "内容预览"
	}
	return fmt.Sprintf(
		`<p style="margin:24px 0 8px;color:#8e8e93;font-size:13px;">%s</p><div style="padding:16px;background-color:#f5f5f7;border-radius:10px;line-height:1.6;color:#1d1d1f;font-size:14px;">%s</div>`,
		html.EscapeString(title), previewHTML,
	)
}

// RenderPlaceholders 将模板里的 {{key}} 替换为 vars[key] 对应的值。
//
// 说明：
//   - key 支持字母、数字、下划线、点；两侧允许空白，如 {{ user_name }}
//   - 未命中的占位符保持原样，便于管理员在自定义模板里识别错别字
//   - 变量值**不做 HTML 转义**。调用方在准备 vars 时需要自行处理 (EscapeAndBreak / html.EscapeString)，
//     以便模板作者可以故意插入 HTML 片段（例如预览块）
func RenderPlaceholders(tpl string, vars map[string]string) string {
	if tpl == "" || len(vars) == 0 {
		return tpl
	}
	var sb strings.Builder
	sb.Grow(len(tpl))
	i := 0
	for i < len(tpl) {
		// 查找 {{
		idx := strings.Index(tpl[i:], "{{")
		if idx < 0 {
			sb.WriteString(tpl[i:])
			break
		}
		sb.WriteString(tpl[i : i+idx])
		start := i + idx
		end := strings.Index(tpl[start+2:], "}}")
		if end < 0 {
			// 没有闭合，原样保留
			sb.WriteString(tpl[start:])
			break
		}
		rawKey := tpl[start+2 : start+2+end]
		key := strings.TrimSpace(rawKey)
		if val, ok := vars[key]; ok {
			sb.WriteString(val)
		} else {
			// 原样保留整个 {{...}}
			sb.WriteString(tpl[start : start+2+end+2])
		}
		i = start + 2 + end + 2
	}
	return sb.String()
}
