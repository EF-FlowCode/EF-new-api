package service

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/model"
)

// ResolveUserNotificationEmail 返回用户收件邮箱：优先用 UserSetting.NotificationEmail，其次 user.Email。
// 两个都空则返回空串。
func ResolveUserNotificationEmail(user *model.User, userSetting dto.UserSetting) string {
	if override := strings.TrimSpace(userSetting.NotificationEmail); override != "" {
		return override
	}
	if user == nil {
		return ""
	}
	return strings.TrimSpace(user.Email)
}

func NotifyRootUser(t string, subject string, content string) {
	user := model.GetRootUser().ToBaseUser()
	err := NotifyUser(user.Id, user.Email, user.GetSetting(), dto.NewNotify(t, subject, content, nil))
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to notify root user: %s", err.Error()))
	}
}

func NotifyUpstreamModelUpdateWatchers(subject string, content string) {
	var users []model.User
	if err := model.DB.
		Select("id", "email", "role", "status", "setting").
		Where("status = ? AND role >= ?", common.UserStatusEnabled, common.RoleAdminUser).
		Find(&users).Error; err != nil {
		common.SysLog(fmt.Sprintf("failed to query upstream update notification users: %s", err.Error()))
		return
	}

	notification := dto.NewNotify(dto.NotifyTypeChannelUpdate, subject, content, nil)
	sentCount := 0
	for _, user := range users {
		userSetting := user.GetSetting()
		if !userSetting.UpstreamModelUpdateNotifyEnabled {
			continue
		}
		if err := NotifyUser(user.Id, user.Email, userSetting, notification); err != nil {
			common.SysLog(fmt.Sprintf("failed to notify user %d for upstream model update: %s", user.Id, err.Error()))
			continue
		}
		sentCount++
	}
	common.SysLog(fmt.Sprintf("upstream model update notifications sent: %d", sentCount))
}

func NotifyUser(userId int, userEmail string, userSetting dto.UserSetting, data dto.Notify) error {
	notifyType := userSetting.NotifyType
	if notifyType == "" {
		notifyType = dto.NotifyTypeEmail
	}

	// Check notification limit
	canSend, err := CheckNotificationLimit(userId, data.Type)
	if err != nil {
		common.SysLog(fmt.Sprintf("failed to check notification limit: %s", err.Error()))
		return err
	}
	if !canSend {
		return fmt.Errorf("notification limit exceeded for user %d with type %s", userId, notifyType)
	}

	switch notifyType {
	case dto.NotifyTypeEmail:
		// 优先使用设置中的通知邮箱，如果为空则使用用户的默认邮箱
		emailToUse := userSetting.NotificationEmail
		if emailToUse == "" {
			emailToUse = userEmail
		}
		if emailToUse == "" {
			common.SysLog(fmt.Sprintf("user %d has no email, skip sending email", userId))
			return nil
		}
		return sendEmailNotify(emailToUse, data)
	case dto.NotifyTypeWebhook:
		webhookURLStr := userSetting.WebhookUrl
		if webhookURLStr == "" {
			common.SysLog(fmt.Sprintf("user %d has no webhook url, skip sending webhook", userId))
			return nil
		}

		// 获取 webhook secret
		webhookSecret := userSetting.WebhookSecret
		return SendWebhookNotify(webhookURLStr, webhookSecret, data)
	case dto.NotifyTypeBark:
		barkURL := userSetting.BarkUrl
		if barkURL == "" {
			common.SysLog(fmt.Sprintf("user %d has no bark url, skip sending bark", userId))
			return nil
		}
		return sendBarkNotify(barkURL, data)
	case dto.NotifyTypeGotify:
		gotifyUrl := userSetting.GotifyUrl
		gotifyToken := userSetting.GotifyToken
		if gotifyUrl == "" || gotifyToken == "" {
			common.SysLog(fmt.Sprintf("user %d has no gotify url or token, skip sending gotify", userId))
			return nil
		}
		return sendGotifyNotify(gotifyUrl, gotifyToken, userSetting.GotifyPriority, data)
	}
	return nil
}

func sendEmailNotify(userEmail string, data dto.Notify) error {
	// make email content
	content := data.Content
	// 处理占位符
	for _, value := range data.Values {
		content = strings.Replace(content, dto.ContentValueParam, fmt.Sprintf("%v", value), 1)
	}
	return common.SendEmail(data.Title, userEmail, content)
}

// notifyOutboundTimeout 限定单次用户通知外呼（webhook/bark/gotify）的总时长，
// RelayTimeout 为 0 时 HTTP client 本身不设超时，目标不响应会永久挂住。
var notifyOutboundTimeout = 10 * time.Second

const (
	barkNotifyUserAgent   = "OneAPI-Bark-Notify/1.0"
	gotifyNotifyUserAgent = "NewAPI-Gotify-Notify/1.0"
)

func buildBarkOutboundRequest(barkURL string, title string, content string) outboundRequest {
	finalURL := strings.ReplaceAll(barkURL, "{{title}}", url.QueryEscape(title))
	finalURL = strings.ReplaceAll(finalURL, "{{content}}", url.QueryEscape(content))
	return outboundRequest{
		URL:    finalURL,
		Method: http.MethodGet,
		Headers: map[string]string{
			"User-Agent": barkNotifyUserAgent,
		},
	}
}

func sendBarkNotify(barkURL string, data dto.Notify) error {
	content := data.Content
	for _, value := range data.Values {
		content = strings.Replace(content, dto.ContentValueParam, fmt.Sprintf("%v", value), 1)
	}

	adapter, throughWorker := configuredOutboundHTTPAdapter()
	request := buildBarkOutboundRequest(barkURL, data.Title, content)
	ctx, cancel := context.WithTimeout(context.Background(), notifyOutboundTimeout)
	defer cancel()
	return sendOutboundNotification(ctx, adapter, throughWorker, "bark", request)
}

func buildGotifyOutboundRequest(gotifyURL string, gotifyToken string, priority int, title string, message string) (outboundRequest, error) {
	finalURL := strings.TrimSuffix(gotifyURL, "/") + "/message?token=" + url.QueryEscape(gotifyToken)
	if priority < 0 || priority > 10 {
		priority = 5
	}
	payloadBytes, err := common.Marshal(struct {
		Title    string `json:"title"`
		Message  string `json:"message"`
		Priority int    `json:"priority"`
	}{
		Title:    title,
		Message:  message,
		Priority: priority,
	})
	if err != nil {
		return outboundRequest{}, err
	}
	return outboundRequest{
		URL:    finalURL,
		Method: http.MethodPost,
		Headers: map[string]string{
			"Content-Type": "application/json; charset=utf-8",
			"User-Agent":   gotifyNotifyUserAgent,
		},
		Body: payloadBytes,
	}, nil
}

func sendGotifyNotify(gotifyURL string, gotifyToken string, priority int, data dto.Notify) error {
	content := data.Content
	for _, value := range data.Values {
		content = strings.Replace(content, dto.ContentValueParam, fmt.Sprintf("%v", value), 1)
	}

	request, err := buildGotifyOutboundRequest(gotifyURL, gotifyToken, priority, data.Title, content)
	if err != nil {
		return fmt.Errorf("failed to marshal gotify payload: %v", err)
	}
	adapter, throughWorker := configuredOutboundHTTPAdapter()
	ctx, cancel := context.WithTimeout(context.Background(), notifyOutboundTimeout)
	defer cancel()
	return sendOutboundNotification(ctx, adapter, throughWorker, "gotify", request)
}
