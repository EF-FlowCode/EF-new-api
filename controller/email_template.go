package controller

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/gin-gonic/gin"
)

// EmailTemplateItem 是 GET /api/option/email_templates 返回的每项
type EmailTemplateItem struct {
	Key            string                           `json:"key"`
	Name           string                           `json:"name"`
	Description    string                           `json:"description"`
	Variables      []constant.EmailTemplateVariable `json:"variables"`
	DefaultSubject string                           `json:"default_subject"`
	DefaultBody    string                           `json:"default_body"`
	CurrentSubject string                           `json:"current_subject"`
	CurrentBody    string                           `json:"current_body"`
	Customized     bool                             `json:"customized"`
}

// ListEmailTemplates 返回所有可配置邮件模板 + 当前保存值。Root only。
func ListEmailTemplates(c *gin.Context) {
	specs := constant.EmailTemplateSpecs()
	items := make([]EmailTemplateItem, 0, len(specs))

	common.OptionMapRWMutex.RLock()
	saved := make(map[string]string, len(specs)*2)
	for _, spec := range specs {
		subjectKey := constant.EmailTemplateSubjectKey(spec.Key)
		bodyKey := constant.EmailTemplateBodyKey(spec.Key)
		saved[subjectKey] = common.OptionMap[subjectKey]
		saved[bodyKey] = common.OptionMap[bodyKey]
	}
	common.OptionMapRWMutex.RUnlock()

	for _, spec := range specs {
		savedSubject := saved[constant.EmailTemplateSubjectKey(spec.Key)]
		savedBody := saved[constant.EmailTemplateBodyKey(spec.Key)]
		current := EmailTemplateItem{
			Key:            spec.Key,
			Name:           spec.Name,
			Description:    spec.Description,
			Variables:      spec.Variables,
			DefaultSubject: spec.DefaultSubject,
			DefaultBody:    spec.DefaultBody,
			CurrentSubject: savedSubject,
			CurrentBody:    savedBody,
			Customized:     savedSubject != "" || savedBody != "",
		}
		if current.CurrentSubject == "" {
			current.CurrentSubject = spec.DefaultSubject
		}
		if current.CurrentBody == "" {
			current.CurrentBody = spec.DefaultBody
		}
		items = append(items, current)
	}

	common.ApiSuccess(c, items)
}

type previewEmailTemplateRequest struct {
	Key     string `json:"key"`
	Subject string `json:"subject"`
	Body    string `json:"body"`
}

// PreviewEmailTemplate 使用示例变量渲染给定模板。Root only。
//
// 请求 {key, subject?, body?}：subject/body 为空时使用已保存（或默认）值，方便"未保存先预览"。
func PreviewEmailTemplate(c *gin.Context) {
	var req previewEmailTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	if req.Key == "" {
		common.ApiErrorMsg(c, "缺少 key")
		return
	}
	subject, body, err := service.PreviewEmailTemplate(req.Key, req.Subject, req.Body)
	if err != nil {
		common.ApiErrorMsg(c, err.Error())
		return
	}
	common.ApiSuccess(c, gin.H{
		"subject": subject,
		"body":    body,
	})
}

type resetEmailTemplateRequest struct {
	Key string `json:"key"`
}

// ResetEmailTemplate 清空某个模板的自定义值（让系统回落到默认）。Root only。
func ResetEmailTemplate(c *gin.Context) {
	var req resetEmailTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiErrorMsg(c, "参数错误")
		return
	}
	spec, ok := constant.FindEmailTemplateSpec(req.Key)
	if !ok {
		common.ApiErrorMsg(c, "未知的模板")
		return
	}
	if err := model.UpdateOption(constant.EmailTemplateSubjectKey(spec.Key), ""); err != nil {
		common.ApiError(c, err)
		return
	}
	if err := model.UpdateOption(constant.EmailTemplateBodyKey(spec.Key), ""); err != nil {
		common.ApiError(c, err)
		return
	}
	common.ApiSuccess(c, gin.H{
		"key": spec.Key,
	})
}
