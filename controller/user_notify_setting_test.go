package controller

import (
	"net/http"
	"testing"

	"eflow.local/new-api/dto"
	"eflow.local/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUpdateUserSettingKeepsUnrelatedSettingsAndWebhookSecret(t *testing.T) {
	db := useAccountSecurityTestDB(t)
	user := model.User{Username: "notify-keep", Password: "password-hash", AffCode: "ntfk01"}
	user.SetSetting(dto.UserSetting{
		NotifyType:     dto.NotifyTypeWebhook,
		WebhookUrl:     "https://hook.example/old",
		WebhookSecret:  "old-secret",
		SidebarModules: `{"chat":false}`,
		Language:       "en",
	})
	require.NoError(t, db.Create(&user).Error)
	asUser := func(c *gin.Context) { c.Set("id", user.Id) }

	_, result := callAccountHandler(t, UpdateUserSetting, http.MethodPut,
		`{"notify_type":"webhook","quota_warning_threshold":10,"webhook_url":"https://hook.example/new"}`, asUser)
	require.Equal(t, true, result["success"], result)

	var stored model.User
	require.NoError(t, db.First(&stored, user.Id).Error)
	setting := stored.GetSetting()
	require.Equal(t, `{"chat":false}`, setting.SidebarModules)
	require.Equal(t, "en", setting.Language)
	require.Equal(t, "https://hook.example/new", setting.WebhookUrl)
	require.Equal(t, "old-secret", setting.WebhookSecret)

	_, result = callAccountHandler(t, UpdateUserSetting, http.MethodPut,
		`{"notify_type":"email","quota_warning_threshold":10}`, asUser)
	require.Equal(t, true, result["success"], result)
	require.NoError(t, db.First(&stored, user.Id).Error)
	setting = stored.GetSetting()
	require.Equal(t, `{"chat":false}`, setting.SidebarModules)
	require.Empty(t, setting.WebhookUrl)
	require.Empty(t, setting.WebhookSecret)
}

func TestNotificationEmailMustBeSingleAddress(t *testing.T) {
	require.True(t, isSingleNotificationEmail("user@example.com"))
	for _, value := range []string{
		"a@example.com;b@example.com",
		"a@example.com,b@example.com",
		"Alice <a@example.com>",
		"a@example.com\r\nBcc: b@example.com",
		"not-an-email@",
	} {
		require.False(t, isSingleNotificationEmail(value), value)
	}
}
