package service

import (
	"html"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
)

func setEmailTemplateOptions(t *testing.T, values map[string]string) {
	t.Helper()
	if values == nil {
		values = map[string]string{}
	}
	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = values
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previous
		common.OptionMapRWMutex.Unlock()
	})
}

func TestAccountEmailTemplatesConfiguredAndRestored(t *testing.T) {
	systemName := `A&B <平台> {{valid_minutes}}`
	link := `https://example.com/path?a=1&b="'<tag>`
	for _, tc := range []struct {
		key, templateBody, expectedBody string
		render                          func() (string, string)
	}{
		{constant.EmailTemplateKeyPasswordReset, `<p>{{system_name}} {{valid_minutes}} <a href="{{reset_url}}">继续</a></p>`,
			`<p>` + html.EscapeString(systemName) + ` 15 <a href="` + html.EscapeString(link) + `">继续</a></p>`,
			func() (string, string) { return RenderPasswordResetEmail(systemName, link, 15) }},
		{constant.EmailTemplateKeyQuotaWarning, `<p>{{system_name}} {{remaining_quota}} <a href="{{topup_url}}">账户</a></p>`,
			`<p>` + html.EscapeString(systemName) + ` ¥0.014234 <a href="` + html.EscapeString(link) + `">账户</a></p>`,
			func() (string, string) { return RenderQuotaWarningEmail(systemName, "¥0.014234", link) }},
	} {
		t.Run(tc.key, func(t *testing.T) {
			subjectKey, bodyKey := constant.EmailTemplateSubjectKey(tc.key), constant.EmailTemplateBodyKey(tc.key)
			setEmailTemplateOptions(t, map[string]string{subjectKey: "账户 {{system_name}}", bodyKey: tc.templateBody})
			subject, body := tc.render()
			if subject != "账户 "+systemName || body != tc.expectedBody {
				t.Fatalf("unexpected rendered mail: %q / %q", subject, body)
			}
			common.OptionMapRWMutex.Lock()
			common.OptionMap[subjectKey], common.OptionMap[bodyKey] = "", ""
			common.OptionMapRWMutex.Unlock()
			subject, body = tc.render()
			if subject == "账户 "+systemName || !strings.Contains(body, html.EscapeString(link)) || strings.Contains(body, ">继续</a>") {
				t.Fatalf("did not restore defaults: %q / %q", subject, body)
			}
		})
	}
}

func TestAccountEmailTemplatePreview(t *testing.T) {
	setEmailTemplateOptions(t, map[string]string{
		constant.EmailTemplateSubjectKey(constant.EmailTemplateKeyPasswordReset): "已保存主题",
		constant.EmailTemplateBodyKey(constant.EmailTemplateKeyPasswordReset):    "{{reset_url}} {{valid_minutes}} 已保存正文",
	})
	for _, key := range []string{constant.EmailTemplateKeyPasswordReset, constant.EmailTemplateKeyQuotaWarning} {
		spec, _ := constant.FindEmailTemplateSpec(key)
		subject, body, err := PreviewEmailTemplate(key, "", "")
		if err != nil {
			t.Fatal(err)
		}
		wantSubject, wantBody := renderEmailTemplate(spec, spec.DefaultSubject, spec.DefaultBody, sampleVarsFromSpec(spec))
		if subject != wantSubject || body != wantBody {
			t.Fatalf("empty preview must use defaults: %q / %q", subject, body)
		}
		if strings.Contains(body, "&token=") {
			t.Fatal("preview did not escape URL")
		}
		if _, _, err := PreviewEmailTemplate(key, "{{typo}}", spec.DefaultBody); err == nil {
			t.Fatal("accepted unknown preview variable")
		}
		if _, _, err := PreviewEmailTemplate(key, spec.DefaultSubject, "遗漏必要变量"); err == nil {
			t.Fatal("accepted missing body variables")
		}
		customSubject := "自定义 {{system_name}}"
		if subject, _, err := PreviewEmailTemplate(key, customSubject, spec.DefaultBody); err != nil || subject != "自定义 New API" {
			t.Fatalf("custom preview failed: %q %v", subject, err)
		}
	}
}

func TestLegacyEmailTemplateHTMLVariables(t *testing.T) {
	key := constant.EmailTemplateKeyTicketCreatedAdmin
	setEmailTemplateOptions(t, map[string]string{constant.EmailTemplateBodyKey(key): "{{info_table}} {{unknown}}"})
	_, body := RenderEmailByKey(key, map[string]string{"info_table": "<table><tr><td>A&amp;B</td></tr></table>"})
	if body != "<table><tr><td>A&amp;B</td></tr></table> {{unknown}}" {
		t.Fatalf("legacy HTML semantics changed: %q", body)
	}
}
