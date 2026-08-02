package common

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"mime"
	"net/smtp"
	"slices"
	"strings"
	"time"
)

func generateMessageID() (string, error) {
	split := strings.Split(SMTPFrom, "@")
	if len(split) < 2 {
		return "", fmt.Errorf("invalid SMTP account")
	}
	domain := strings.Split(SMTPFrom, "@")[1]
	return fmt.Sprintf("<%d.%s@%s>", time.Now().UnixNano(), GetRandomString(12), domain), nil
}

func shouldUseSMTPLoginAuth() bool {
	if SMTPForceAuthLogin {
		return true
	}
	return isOutlookServer(SMTPAccount) || slices.Contains(EmailLoginAuthServerList, SMTPServer)
}

func getSMTPAuth() smtp.Auth {
	if shouldUseSMTPLoginAuth() {
		return LoginAuth(SMTPAccount, SMTPToken)
	}
	return smtp.PlainAuth("", SMTPAccount, SMTPToken, SMTPServer)
}

// splitAddrs 把 ";" 分隔的地址串拆成去空白、去空项、去重的切片。
func splitAddrs(raw string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, 4)
	for _, part := range strings.Split(raw, ";") {
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
func archiveBccList(primary []string) []string {
	if strings.TrimSpace(SMTPArchiveBcc) == "" {
		return nil
	}
	inPrimary := make(map[string]struct{}, len(primary))
	for _, p := range primary {
		inPrimary[p] = struct{}{}
	}
	out := make([]string, 0, 2)
	for _, a := range splitAddrs(SMTPArchiveBcc) {
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
// attachments == nil 时与 SendEmail 完全等价：仍然走 text/html 单段 body，对历史调用方零行为差异。
func SendEmailWithAttachments(subject string, receiver string, content string, attachments []EmailAttachment) error {
	if SMTPFrom == "" { // for compatibility
		SMTPFrom = SMTPAccount
	}
	id, err2 := generateMessageID()
	if err2 != nil {
		return err2
	}
	if SMTPServer == "" && SMTPAccount == "" {
		return fmt.Errorf("SMTP 服务器未配置")
	}
	encodedSubject := fmt.Sprintf("=?UTF-8?B?%s?=", base64.StdEncoding.EncodeToString([]byte(subject)))
	headers := fmt.Sprintf("To: %s\r\n"+
		"From: %s <%s>\r\n"+
		"Subject: %s\r\n"+
		"Date: %s\r\n"+
		"Message-ID: %s\r\n",
		receiver, SystemName, SMTPFrom, encodedSubject, time.Now().Format(time.RFC1123Z), id)

	var mail []byte
	if len(attachments) == 0 {
		// 兼容旧路径：不带附件时仍发单段 text/html 邮件。
		mail = []byte(fmt.Sprintf("%sContent-Type: text/html; charset=UTF-8\r\n\r\n%s\r\n",
			headers, content))
	} else {
		boundary := fmt.Sprintf("=_eflow_%d", time.Now().UnixNano())
		var buf bytes.Buffer
		buf.WriteString(headers)
		buf.WriteString("MIME-Version: 1.0\r\n")
		fmt.Fprintf(&buf, "Content-Type: multipart/mixed; boundary=\"%s\"\r\n\r\n", boundary)
		// 第一段：HTML 正文
		fmt.Fprintf(&buf, "--%s\r\n", boundary)
		buf.WriteString("Content-Type: text/html; charset=UTF-8\r\n")
		buf.WriteString("Content-Transfer-Encoding: 7bit\r\n\r\n")
		buf.WriteString(content)
		buf.WriteString("\r\n")
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
			// 76 列折行，符合 RFC 2045
			b64 := base64.StdEncoding.EncodeToString(a.Data)
			for i := 0; i < len(b64); i += 76 {
				end := i + 76
				if end > len(b64) {
					end = len(b64)
				}
				buf.WriteString(b64[i:end])
				buf.WriteString("\r\n")
			}
		}
		fmt.Fprintf(&buf, "--%s--\r\n", boundary)
		mail = buf.Bytes()
	}

	auth := getSMTPAuth()
	addr := fmt.Sprintf("%s:%d", SMTPServer, SMTPPort)
	// eflow: 显式收件人 + 归档盲送。归档地址只进 RCPT、不进邮件头，对收件人不可见；
	// 且 best-effort —— 归档投递失败绝不影响真实收件人那一份。
	to := splitAddrs(receiver)
	archive := archiveBccList(to)
	var err error
	if SMTPPort == 465 || SMTPSSLEnabled {
		tlsConfig := &tls.Config{
			InsecureSkipVerify: true,
			ServerName:         SMTPServer,
		}
		conn, err := tls.Dial("tcp", fmt.Sprintf("%s:%d", SMTPServer, SMTPPort), tlsConfig)
		if err != nil {
			return err
		}
		client, err := smtp.NewClient(conn, SMTPServer)
		if err != nil {
			return err
		}
		defer client.Close()
		if err = client.Auth(auth); err != nil {
			return err
		}
		if err = client.Mail(SMTPFrom); err != nil {
			return err
		}
		for _, rcpt := range to {
			if err = client.Rcpt(rcpt); err != nil {
				return err
			}
		}
		// eflow: 归档盲送 best-effort —— RCPT 被拒只记日志、继续，不让坏的归档地址
		// 连累真实收件人（DATA 仍会发给上面已接受的真实收件人）。
		for _, rcpt := range archive {
			if e := client.Rcpt(rcpt); e != nil {
				SysError(fmt.Sprintf("archive bcc rcpt %s rejected (mail still sent to real recipients): %v", rcpt, e))
			}
		}
		w, err := client.Data()
		if err != nil {
			return err
		}
		_, err = w.Write(mail)
		if err != nil {
			return err
		}
		err = w.Close()
		if err != nil {
			return err
		}
	} else {
		err = smtp.SendMail(addr, auth, SMTPFrom, to, mail)
		// eflow: 非 SSL 路径 smtp.SendMail 是一次性的、无法按收件人容错，
		// 故归档盲送单独再发一封 best-effort，失败只记日志，不覆盖主发送结果。
		if err == nil && len(archive) > 0 {
			if e := smtp.SendMail(addr, auth, SMTPFrom, archive, mail); e != nil {
				SysError(fmt.Sprintf("archive bcc send failed (real recipients unaffected): %v", e))
			}
		}
	}
	if err != nil {
		SysError(fmt.Sprintf("failed to send email to %s: %v", receiver, err))
	}
	return err
}
