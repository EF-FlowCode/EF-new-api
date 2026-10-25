package service

import (
	"fmt"
	"html"
	"strings"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	gopool "eflow.local/new-api/internal/drainpool"
	"eflow.local/new-api/model"
	"eflow.local/new-api/setting/system_setting"
)

func paymentMethodLabel(method string) string {
	switch strings.ToLower(strings.TrimSpace(method)) {
	case "stripe":
		return "Stripe"
	case "epay":
		return "易支付"
	case "creem":
		return "Creem"
	case "waffo":
		return "Waffo"
	case "":
		return "在线支付"
	default:
		return method
	}
}

func paymentActionURL(serverAddress string) string {
	base := strings.TrimRight(serverAddress, "/")
	if base == "" {
		return ""
	}
	return base + "/topup"
}

func buildTopUpVars(topUp *model.TopUp, user *model.User, forAdmin bool, serverAddress string) map[string]string {
	completedAt := time.Unix(topUp.CompleteTime, 0).Format("2006-01-02 15:04:05")
	if topUp.CompleteTime == 0 {
		completedAt = time.Now().Format("2006-01-02 15:04:05")
	}
	username := ""
	userId := 0
	if user != nil {
		username = strings.TrimSpace(user.Username)
		userId = user.Id
	}

	tradeNoEsc := html.EscapeString(topUp.TradeNo)
	methodEsc := html.EscapeString(paymentMethodLabel(topUp.PaymentMethod))
	moneyStr := fmt.Sprintf("%.2f", topUp.Money)
	amountStr := fmt.Sprintf("%d", topUp.Amount)
	completedEsc := html.EscapeString(completedAt)
	usernameEsc := html.EscapeString(username)

	rows := []common.EmailTemplateRow{
		{Label: "订单编号", Value: tradeNoEsc},
		{Label: "支付方式", Value: methodEsc},
		{Label: "支付金额", Value: moneyStr},
		{Label: "充值额度", Value: amountStr},
		{Label: "完成时间", Value: completedEsc},
	}
	if username != "" {
		rows = append([]common.EmailTemplateRow{
			{Label: "下单用户", Value: usernameEsc},
		}, rows...)
	}

	heading := "充值已到账"
	intro := "你的充值已经到账，感谢支持。"
	actionLabel := "查看账户"
	if forAdmin {
		heading = "一笔新的充值"
		intro = fmt.Sprintf("%s 刚完成了一笔充值。", username)
		actionLabel = "查看后台"
	}

	return map[string]string{
		"system_name":           html.EscapeString(common.SystemNameOrDefault()),
		"server_address":        html.EscapeString(strings.TrimRight(serverAddress, "/")),
		"heading":               html.EscapeString(heading),
		"intro":                 html.EscapeString(intro),
		"trade_no":              tradeNoEsc,
		"payment_method":        methodEsc,
		"money":                 moneyStr,
		"amount":                amountStr,
		"username":              usernameEsc,
		"user_id":               fmt.Sprintf("%d", userId),
		"completed_at":          completedEsc,
		"info_table":            common.RenderInfoTableHTML(rows),
		"content_preview":       "",
		"content_preview_block": "",
		"action_url":            html.EscapeString(paymentActionURL(serverAddress)),
		"action_label":          html.EscapeString(actionLabel),
	}
}

// NotifyTopUpSuccessByTradeNo 先按 tradeNo 查订单再转发到 NotifyTopUpSuccess。
// 支付回调拿不到更新后的 TopUp 对象时用此入口。
func NotifyTopUpSuccessByTradeNo(tradeNo string) {
	if tradeNo == "" {
		return
	}
	notificationSettings := common.GetPaymentNotificationSettings()
	serverAddress := system_setting.GetServerAddress()
	if !notificationSettings.UserEnabled && !notificationSettings.AdminEnabled {
		return
	}
	notifyTopUpSuccess(model.GetTopUpByTradeNo(tradeNo), notificationSettings, serverAddress)
}

// NotifyTopUpSuccess 异步在支付成功后发送邮件（用户 / 管理员 两条通路各自可开关）
func NotifyTopUpSuccess(topUp *model.TopUp) {
	notificationSettings := common.GetPaymentNotificationSettings()
	serverAddress := system_setting.GetServerAddress()
	notifyTopUpSuccess(topUp, notificationSettings, serverAddress)
}

func notifyTopUpSuccess(topUp *model.TopUp, notificationSettings common.PaymentNotificationSettings, serverAddress string) {
	if topUp == nil {
		return
	}
	if !notificationSettings.UserEnabled && !notificationSettings.AdminEnabled {
		return
	}

	gopool.Go(func() {
		user, err := model.GetUserById(topUp.UserId, false)
		if err != nil {
			common.SysLog(fmt.Sprintf("topup notify: failed to load user %d (trade_no=%s): %s", topUp.UserId, topUp.TradeNo, err.Error()))
			return
		}

		if notificationSettings.UserEnabled {
			userSetting := user.GetSetting()
			userEmail := ResolveUserNotificationEmail(user, userSetting)
			if userEmail != "" {
				vars := buildTopUpVars(topUp, user, false, serverAddress)
				subject, body := RenderEmailByKey(constant.EmailTemplateKeyPaymentSuccessUser, vars)
				if subject != "" && body != "" {
					notify := dto.NewNotify(dto.NotifyTypePaymentSuccess, subject, body, nil)
					if err := NotifyUser(user.Id, userEmail, userSetting, notify); err != nil {
						common.SysLog(fmt.Sprintf("topup notify: failed to notify user %d (trade_no=%s): %s", user.Id, topUp.TradeNo, err.Error()))
					}
				}
			}
		}

		if notificationSettings.AdminEnabled {
			recipients := parseAdminEmails(notificationSettings.AdminEmail)
			if len(recipients) == 0 {
				return
			}
			vars := buildTopUpVars(topUp, user, true, serverAddress)
			subject, body := RenderEmailByKey(constant.EmailTemplateKeyPaymentSuccessAdmin, vars)
			if subject == "" || body == "" {
				return
			}
			for _, to := range recipients {
				if err := common.SendEmail(subject, to, body); err != nil {
					common.SysLog(fmt.Sprintf("topup notify: failed to send admin email to %s (trade_no=%s): %s", to, topUp.TradeNo, err.Error()))
				}
			}
		}
	})
}
