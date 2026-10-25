package common

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"mime"
	"net/mail"
	"net/smtp"
	"slices"
	"strings"
	"time"
)

func generateMessageID(settings SMTPSettings) (string, error) {
	split := strings.Split(settings.From, "@")
	if len(split) < 2 {
		return "", fmt.Errorf("invalid SMTP account")
	}
	domain := split[1]
	return fmt.Sprintf("<%d.%s@%s>", time.Now().UnixNano(), GetRandomString(12), domain), nil
}

func shouldUseSMTPLoginAuth(account, server string, forceAuthLogin bool) bool {
	if forceAuthLogin {
		return true
	}
	return isOutlookServer(account) || slices.Contains(emailLoginAuthServerList, server)
}

var emailLoginAuthServerList = []string{
	"smtp.sendcloud.net",
	"smtp.azurecomm.net",
}

func getSMTPAuth(settings SMTPSettings) smtp.Auth {
	return AutoSMTPAuth(settings.Account, settings.Token, settings.Server, settings.ForceAuthLogin)
}

func shouldAuthenticateSMTP(settings SMTPSettings) bool {
	return settings.Account != "" && settings.Token != ""
}

func smtpTLSConfig(settings SMTPSettings) *tls.Config {
	return &tls.Config{
		ServerName:         settings.Server,
		InsecureSkipVerify: settings.InsecureSkipVerify, // #nosec G402 -- admin-controlled SMTP compatibility option.
	}
}

func newSMTPClient(addr string, settings SMTPSettings) (*smtp.Client, error) {
	if settings.SSLEnabled || (settings.Port == 465 && !settings.StartTLSEnabled) {
		conn, err := tls.Dial("tcp", addr, smtpTLSConfig(settings))
		if err != nil {
			return nil, err
		}
		client, err := smtp.NewClient(conn, settings.Server)
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		return client, nil
	}

	client, err := smtp.Dial(addr)
	if err != nil {
		return nil, err
	}
	if settings.StartTLSEnabled {
		startTLSSupported, _ := client.Extension("STARTTLS")
		if !startTLSSupported {
			_ = client.Close()
			return nil, fmt.Errorf("SMTP server does not support STARTTLS")
		}
		if err := client.StartTLS(smtpTLSConfig(settings)); err != nil {
			_ = client.Close()
			return nil, err
		}
	}
	return client, nil
}

// splitAddrs 把 ";" 分隔的地址串拆成去空白、去空项、去重的切片。
func splitAddrs(raw string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, 4)
	for part := range strings.SplitSeq(raw, ";") {
		addr := strings.TrimSpace(part)
		if addr == "" {
			continue
		}
		if _, dup := seen[addr]; dup {
			continue
		}
		seen[addr] = struct{}{}
		out = append(out, addr)
	}
	return out
}

// archiveBccList 返回归档盲送地址（已剔除与显式收件人重复的，避免给同一地址投两份）。
//
// eflow: 归档地址只进 SMTP RCPT，绝不写进 To/Cc/Bcc 邮件头，故对真实收件人不可见。
// **best-effort**：归档投递失败只记日志，绝不影响真实收件人那一份（实现见两处发送分支）。
func archiveBccList(primary []string, archiveBcc string) []string {
	if strings.TrimSpace(archiveBcc) == "" {
		return nil
	}
	inPrimary := make(map[string]struct{}, len(primary))
	for _, p := range primary {
		inPrimary[p] = struct{}{}
	}
	out := make([]string, 0, 2)
	for _, a := range splitAddrs(archiveBcc) {
		if _, dup := inPrimary[a]; dup {
			continue
		}
		out = append(out, a)
	}
	return out
}

// EmailAttachment 描述附在邮件中的二进制文件。
type EmailAttachment struct {
	FileName    string // 用户看到的文件名
	ContentType string // 例如 "image/png"；为空时回落到 application/octet-stream
	Data        []byte // 文件内容（未编码）
}

func SendEmail(subject string, receiver string, content string) error {
	return SendEmailWithAttachments(subject, receiver, content, nil)
}

// SendEmailWithAttachments 发送 HTML 邮件，并可选携带 N 个附件（multipart/mixed 编码）。
// HTML 正文和附件均使用按 76 列折行的 Base64；无附件时使用单段 text/html。
func SendEmailWithAttachments(subject string, receiver string, content string, attachments []EmailAttachment) error {
	settings := GetSMTPSettings()
	branding := GetBrandingSettings()
	if settings.From == "" { // for compatibility
		settings.From = settings.Account
	}
	id, err2 := generateMessageID(settings)
	if err2 != nil {
		return err2
	}
	if settings.Server == "" && settings.Account == "" {
		return fmt.Errorf("SMTP 服务器未配置")
	}
	encodedSubject := mime.BEncoding.Encode("UTF-8", subject)
	from := (&mail.Address{Name: branding.SystemName, Address: settings.From}).String()
	headers := fmt.Sprintf("To: %s\r\n"+
		"From: %s\r\n"+
		"Subject: %s\r\n"+
		"Date: %s\r\n"+
		"Message-ID: %s\r\n"+
		"MIME-Version: 1.0\r\n",
		strings.Join(splitAddrs(receiver), ", "), from, encodedSubject, time.Now().Format(time.RFC1123Z), id)

	var buf bytes.Buffer
	buf.WriteString(headers)
	if len(attachments) == 0 {
		buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
		writeEmailBase64(&buf, []byte(content))
	} else {
		boundary := fmt.Sprintf("=_eflow_%d", time.Now().UnixNano())
		fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", boundary)
		// 第一段：HTML 正文
		fmt.Fprintf(&buf, "--%s\r\n", boundary)
		buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
		writeEmailBase64(&buf, []byte(content))
		// 后续 N 段：附件（base64 + RFC 2047 文件名）
		for _, a := range attachments {
			ct := strings.TrimSpace(a.ContentType)
			if ct == "" {
				ct = "application/octet-stream"
			}
			encodedName := mime.BEncoding.Encode("UTF-8", a.FileName)
			fmt.Fprintf(&buf, "--%s\r\n", boundary)
			fmt.Fprintf(&buf, "Content-Type: %s; name=\"%s\"\r\n", ct, encodedName)
			fmt.Fprintf(&buf, "Content-Disposition: attachment; filename=\"%s\"\r\n", encodedName)
			buf.WriteString("Content-Transfer-Encoding: base64\r\n\r\n")
			writeEmailBase64(&buf, a.Data)
		}
		fmt.Fprintf(&buf, "--%s--\r\n", boundary)
	}

	auth := getSMTPAuth(settings)
	addr := fmt.Sprintf("%s:%d", settings.Server, settings.Port)
	// eflow: 显式收件人 + 归档盲送。归档地址只进 RCPT、不进邮件头，对收件人不可见；
	// 且 best-effort —— 归档投递失败绝不影响真实收件人那一份。
	to := splitAddrs(receiver)
	archive := archiveBccList(to, settings.ArchiveBcc)
	client, err := newSMTPClient(addr, settings)
	if err != nil {
		return err
	}
	defer client.Close()
	if shouldAuthenticateSMTP(settings) {
		if err = client.Auth(auth); err != nil {
			return err
		}
	}
	if err = client.Mail(settings.From); err != nil {
		return err
	}
	for _, rcpt := range to {
		if err = client.Rcpt(rcpt); err != nil {
			return err
		}
	}
	// eflow: 归档盲送 best-effort。归档地址只进 RCPT，不影响真实收件人。
	for _, rcpt := range archive {
		if archiveErr := client.Rcpt(rcpt); archiveErr != nil {
			SysError(fmt.Sprintf("archive bcc rcpt %s rejected (mail still sent to real recipients): %v", rcpt, archiveErr))
		}
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err = w.Write(buf.Bytes()); err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	err = client.Quit()
	if err != nil {
		SysError(fmt.Sprintf("failed to send email to %s: %v", receiver, err))
	}
	return err
}

// writeEmailBase64 保留正文原始字节，使用 RFC 2045 的 76 列上限和 CRLF 换行。
func writeEmailBase64(buf *bytes.Buffer, data []byte) {
	encoded := base64.StdEncoding.EncodeToString(data)
	for len(encoded) > 76 {
		buf.WriteString(encoded[:76])
		buf.WriteString("\r\n")
		encoded = encoded[76:]
	}
	buf.WriteString(encoded)
	buf.WriteString("\r\n")
}
