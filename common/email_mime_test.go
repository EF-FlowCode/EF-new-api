package common

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/textproto"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Capture the public sender's actual SMTP envelope and DATA without external mail.
func captureMIMEEmail(t *testing.T, send func() error) ([]byte, []string) {
	t.Helper()
	preserveSMTPSettings(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })

	smtpSettingsMu.Lock()
	publishSMTPSettings(SMTPSettings{
		Server: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port,
		From: "sender@example.com", ArchiveBcc: "archive@example.com",
	})
	smtpSettingsMu.Unlock()

	type capture struct {
		data       []byte
		recipients []string
		err        error
	}
	result := make(chan capture, 1)
	go func() {
		var got capture
		defer func() { result <- got }()
		conn, err := listener.Accept()
		if err != nil {
			got.err = err
			return
		}
		defer conn.Close()
		if got.err = conn.SetDeadline(time.Now().Add(5 * time.Second)); got.err != nil {
			return
		}
		client := textproto.NewConn(conn)
		got.err = client.PrintfLine("220 localhost test SMTP")
		for got.err == nil {
			line, err := client.ReadLine()
			if err != nil {
				got.err = err
				return
			}
			switch {
			case strings.HasPrefix(line, "EHLO "), strings.HasPrefix(line, "HELO "), strings.HasPrefix(line, "MAIL FROM:"):
				got.err = client.PrintfLine("250 OK")
			case strings.HasPrefix(line, "RCPT TO:"):
				got.recipients = append(got.recipients, strings.TrimPrefix(line, "RCPT TO:"))
				got.err = client.PrintfLine("250 OK")
			case line == "DATA":
				if got.err = client.PrintfLine("354 Send message"); got.err != nil {
					return
				}
				got.data, got.err = client.ReadDotBytes()
				if got.err != nil {
					return
				}
				got.err = client.PrintfLine("250 Queued")
			case line == "QUIT":
				got.err = client.PrintfLine("221 Bye")
				return
			default:
				got.err = fmt.Errorf("unexpected SMTP command: %s", line)
			}
		}
	}()

	require.NoError(t, send())
	select {
	case got := <-result:
		require.NoError(t, got.err)
		return got.data, got.recipients
	case <-time.After(6 * time.Second):
		t.Fatal("SMTP capture did not finish")
		return nil, nil
	}
}

func decodeMIMEBase64(t *testing.T, encoding string, body io.Reader) []byte {
	t.Helper()
	require.Equal(t, "base64", encoding)
	raw, err := io.ReadAll(body)
	require.NoError(t, err)
	for line := range bytes.SplitSeq(raw, []byte("\n")) {
		assert.LessOrEqual(t, len(bytes.TrimSuffix(line, []byte("\r"))), 76)
	}
	decoded, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, bytes.NewReader(raw)))
	require.NoError(t, err)
	return decoded
}

func TestSendEmailMIME(t *testing.T) {
	for _, withAttachments := range []bool{false, true} {
		t.Run("attachments="+strconv.FormatBool(withAttachments), func(t *testing.T) {
			preserveBrandingSettings(t)
			brand := "电子流码, 邮件服务"
			require.NoError(t, UpdateBrandingSettings(map[string]string{"SystemName": brand}))
			subject := strings.Repeat("电子流码密码重置", 8)
			link := "https://example.com/user/reset?email=user@example.com&token=invalid-test-token"
			content := "<p>您好，正在进行密码重置。</p>\r\n<p><a href='" + link + "'>此处</a></p>\n" +
				"<p>" + strings.Repeat("中文内容😀", 200) + "</p>"
			attachments := []EmailAttachment{
				{FileName: "账单.pdf", ContentType: "application/pdf", Data: bytes.Repeat([]byte{0, 1, 127, 128, 255}, 101)},
				{FileName: "empty.bin", Data: []byte{}},
			}
			raw, recipients := captureMIMEEmail(t, func() error {
				if withAttachments {
					return SendEmailWithAttachments(subject, "user@example.com; second@example.com", content, attachments)
				}
				return SendEmail(subject, "user@example.com; second@example.com", content)
			})
			assert.Equal(t, []string{"<user@example.com>", "<second@example.com>", "<archive@example.com>"}, recipients)
			message, err := mail.ReadMessage(bytes.NewReader(raw))
			require.NoError(t, err)
			assert.Equal(t, "1.0", message.Header.Get("MIME-Version"))
			assert.Empty(t, message.Header.Get("Bcc"))
			assert.NotContains(t, string(raw), "archive@example.com")
			decodedSubject, err := new(mime.WordDecoder).DecodeHeader(message.Header.Get("Subject"))
			require.NoError(t, err)
			assert.Equal(t, subject, decodedSubject)
			for word := range strings.FieldsSeq(message.Header.Get("Subject")) {
				assert.LessOrEqual(t, len(word), 75, "RFC 2047 encoded-word limit")
			}
			from, err := message.Header.AddressList("From")
			require.NoError(t, err)
			require.Len(t, from, 1)
			assert.Equal(t, brand, from[0].Name)
			assert.Equal(t, "sender@example.com", from[0].Address)
			to, err := message.Header.AddressList("To")
			require.NoError(t, err)
			require.Len(t, to, 2)
			assert.Equal(t, "user@example.com", to[0].Address)
			assert.Equal(t, "second@example.com", to[1].Address)
			_, err = message.Header.Date()
			assert.NoError(t, err)
			assert.NotEmpty(t, message.Header.Get("Message-ID"))

			mediaType, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
			require.NoError(t, err)
			if !withAttachments {
				assert.Equal(t, "text/html", mediaType)
				assert.Equal(t, "UTF-8", params["charset"])
				assert.Equal(t, content, string(decodeMIMEBase64(t, message.Header.Get("Content-Transfer-Encoding"), message.Body)))
				return
			}

			require.Equal(t, "multipart/mixed", mediaType)
			reader := multipart.NewReader(message.Body, params["boundary"])
			part, err := reader.NextPart()
			require.NoError(t, err)
			assert.Equal(t, "text/html; charset=UTF-8", part.Header.Get("Content-Type"))
			assert.Equal(t, content, string(decodeMIMEBase64(t, part.Header.Get("Content-Transfer-Encoding"), part)))
			for _, attachment := range attachments {
				part, err = reader.NextPart()
				require.NoError(t, err)
				kind, params, err := mime.ParseMediaType(part.Header.Get("Content-Disposition"))
				require.NoError(t, err)
				assert.Equal(t, "attachment", kind)
				filename, err := new(mime.WordDecoder).DecodeHeader(params["filename"])
				require.NoError(t, err)
				assert.Equal(t, attachment.FileName, filename)
				kind, _, err = mime.ParseMediaType(part.Header.Get("Content-Type"))
				require.NoError(t, err)
				wantType := attachment.ContentType
				if wantType == "" {
					wantType = "application/octet-stream"
				}
				assert.Equal(t, wantType, kind)
				assert.Equal(t, attachment.Data, decodeMIMEBase64(t, part.Header.Get("Content-Transfer-Encoding"), part))
			}
			_, err = reader.NextPart()
			require.ErrorIs(t, err, io.EOF)
		})
	}
}
