package service

import (
	"fmt"
	"strings"
	"testing"

	"golang.org/x/net/html"
)

func TestRenderPasswordResetEmail(t *testing.T) {
	setEmailTemplateOptions(t, nil)
	for _, link := range []string{
		"https://example.com/user/reset?email=reader@example.com&token=6f31a9c482de47b6a150e83d29c4057f",
		"https://example.com/user/reset?email=reader+tag@example.com&token=quote\"'<>&",
	} {
		for _, minutes := range []int{10, 15} {
			t.Run(fmt.Sprintf("%d/%s", minutes, link), func(t *testing.T) {
				subject, body := RenderPasswordResetEmail("New API", link, minutes)
				if subject != "New API密码重置" {
					t.Fatalf("unexpected subject: %q", subject)
				}
				doc, err := html.Parse(strings.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				var visible strings.Builder
				var anchors []*html.Node
				var visit func(*html.Node)
				visit = func(n *html.Node) {
					if n.Type == html.ElementNode && n.Data == "a" {
						anchors = append(anchors, n)
					}
					if n.Type == html.TextNode {
						visible.WriteString(n.Data)
					}
					for child := n.FirstChild; child != nil; child = child.NextSibling {
						visit(child)
					}
				}
				visit(doc)
				if len(anchors) != 1 {
					t.Fatalf("expected one reset link, got %d", len(anchors))
				}
				a := anchors[0]
				if len(a.Attr) != 1 || a.Attr[0].Key != "href" || a.Attr[0].Val != link {
					t.Fatalf("reset link attributes changed: %#v", a.Attr)
				}
				if a.FirstChild == nil || a.FirstChild.Type != html.TextNode || a.FirstChild.Data != link || a.FirstChild.NextSibling != nil {
					t.Fatal("visible link must exactly match its target as plain text")
				}
				want := fmt.Sprintf("您好，我们收到了您的账户密码重置请求。请在 %d 分钟内打开以下页面继续操作：%s如果您未发起此请求，请忽略本邮件。", minutes, link)
				if visible.String() != want {
					t.Fatalf("unexpected visible content: %q", visible.String())
				}
			})
		}
	}
}
