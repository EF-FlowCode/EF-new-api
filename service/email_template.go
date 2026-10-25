package service

import (
	"fmt"
	"html"
	"strconv"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
)

// GetEmailTemplate 读取保存的自定义主题/正文；若 OptionMap 中对应 key 为空，回落到 spec 默认值。
//
// 返回 subject / body 都是未渲染的模板字符串（仍含 {{var}} 占位）。
func GetEmailTemplate(key string) (subject string, body string, spec constant.EmailTemplateSpec, ok bool) {
	spec, ok = constant.FindEmailTemplateSpec(key)
	if !ok {
		return "", "", spec, false
	}

	common.OptionMapRWMutex.RLock()
	savedSubject := common.OptionMap[constant.EmailTemplateSubjectKey(key)]
	savedBody := common.OptionMap[constant.EmailTemplateBodyKey(key)]
	common.OptionMapRWMutex.RUnlock()

	subject = savedSubject
	if subject == "" {
		subject = spec.DefaultSubject
	}
	body = savedBody
	if body == "" {
		body = spec.DefaultBody
	}
	return subject, body, spec, true
}

// RenderEmailByKey 读取模板并用 vars 渲染。返回 (subject, body)。
// 若 key 不存在，返回两个空串 —— 调用方应先判断。
func RenderEmailByKey(key string, vars map[string]string) (string, string) {
	subject, body, spec, ok := GetEmailTemplate(key)
	if !ok {
		return "", ""
	}
	return renderEmailTemplate(spec, subject, body, vars)
}

func renderEmailTemplate(spec constant.EmailTemplateSpec, subject, body string, vars map[string]string) (string, string) {
	bodyVars := vars
	if spec.PlainTextVariables {
		bodyVars = make(map[string]string, len(vars))
		for name, value := range vars {
			bodyVars[name] = html.EscapeString(value)
		}
	}
	// 旧模板的调用方已按正文把 vars 做了 HTML 转义；主题是纯文本，先还原再渲染，
	// 否则主题里出现 &amp; 等实体。
	subjectVars := vars
	if !spec.PlainTextVariables {
		subjectVars = make(map[string]string, len(vars))
		for name, value := range vars {
			subjectVars[name] = html.UnescapeString(value)
		}
	}
	return common.RenderPlaceholders(subject, subjectVars), common.RenderPlaceholders(body, bodyVars)
}

func RenderPasswordResetEmail(systemName, link string, validMinutes int) (string, string) {
	return RenderEmailByKey(constant.EmailTemplateKeyPasswordReset, map[string]string{
		"system_name":   systemName,
		"reset_url":     link,
		"valid_minutes": strconv.Itoa(validMinutes),
	})
}

func RenderQuotaWarningEmail(systemName, remainingQuota, topupURL string) (string, string) {
	return RenderEmailByKey(constant.EmailTemplateKeyQuotaWarning, map[string]string{
		"system_name":     systemName,
		"remaining_quota": remainingQuota,
		"topup_url":       topupURL,
	})
}

// PreviewEmailTemplate 用 spec.Variables 中的 Sample 作为占位变量，渲染一份预览。
//
// 新模板空字段与保存一致，回落默认；旧模板保留空字段预览已保存值的行为。
func PreviewEmailTemplate(key, subject, body string) (renderedSubject, renderedBody string, err error) {
	spec, ok := constant.FindEmailTemplateSpec(key)
	if !ok {
		return "", "", fmt.Errorf("unknown email template key: %s", key)
	}

	if subject == "" || body == "" {
		savedSubject, savedBody, _, _ := GetEmailTemplate(key)
		if spec.PlainTextVariables {
			savedSubject, savedBody = spec.DefaultSubject, spec.DefaultBody
		}
		if subject == "" {
			subject = savedSubject
		}
		if body == "" {
			body = savedBody
		}
	}
	if err := constant.ValidateEmailTemplateField(spec, "subject", subject); err != nil {
		return "", "", err
	}
	if err := constant.ValidateEmailTemplateField(spec, "body", body); err != nil {
		return "", "", err
	}

	vars := sampleVarsFromSpec(spec)
	renderedSubject, renderedBody = renderEmailTemplate(spec, subject, body, vars)
	return renderedSubject, renderedBody, nil
}

func sampleVarsFromSpec(spec constant.EmailTemplateSpec) map[string]string {
	vars := make(map[string]string, len(spec.Variables))
	for _, v := range spec.Variables {
		vars[v.Name] = v.Sample
	}
	return vars
}
