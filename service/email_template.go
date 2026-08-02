package service

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
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
	subject, body, _, ok := GetEmailTemplate(key)
	if !ok {
		return "", ""
	}
	return common.RenderPlaceholders(subject, vars), common.RenderPlaceholders(body, vars)
}

// PreviewEmailTemplate 用 spec.Variables 中的 Sample 作为占位变量，渲染一份预览。
//
// 传入的 subject/body 若为空则使用已保存（或默认）的模板 —— 主要用于"未保存先预览"。
func PreviewEmailTemplate(key, subject, body string) (renderedSubject, renderedBody string, err error) {
	spec, ok := constant.FindEmailTemplateSpec(key)
	if !ok {
		return "", "", fmt.Errorf("unknown email template key: %s", key)
	}

	if subject == "" || body == "" {
		savedSubject, savedBody, _, _ := GetEmailTemplate(key)
		if subject == "" {
			subject = savedSubject
		}
		if body == "" {
			body = savedBody
		}
	}

	vars := sampleVarsFromSpec(spec)
	return common.RenderPlaceholders(subject, vars), common.RenderPlaceholders(body, vars), nil
}

func sampleVarsFromSpec(spec constant.EmailTemplateSpec) map[string]string {
	vars := make(map[string]string, len(spec.Variables))
	for _, v := range spec.Variables {
		vars[v.Name] = v.Sample
	}
	return vars
}
