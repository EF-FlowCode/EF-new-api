package attachment

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting"
)

// 测试辅助：临时改 setting 包级变量，测试结束自动回滚。
func withAttachmentSettings(t *testing.T, enable bool, maxSize int64, exts, mimes string) {
	t.Helper()
	prevEnabled := setting.TicketAttachmentEnabled
	prevSize := setting.TicketAttachmentMaxSize
	prevExts := setting.TicketAttachmentAllowedExts
	prevMimes := setting.TicketAttachmentAllowedMimes
	setting.TicketAttachmentEnabled = enable
	if maxSize > 0 {
		setting.TicketAttachmentMaxSize = maxSize
	}
	if exts != "" {
		setting.TicketAttachmentAllowedExts = exts
	}
	if mimes != "" {
		setting.TicketAttachmentAllowedMimes = mimes
	}
	t.Cleanup(func() {
		setting.TicketAttachmentEnabled = prevEnabled
		setting.TicketAttachmentMaxSize = prevSize
		setting.TicketAttachmentAllowedExts = prevExts
		setting.TicketAttachmentAllowedMimes = prevMimes
	})
}

func TestSanitizeFileName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		err  bool
	}{
		{"trim-and-keep", "  report.pdf ", "report.pdf", false},
		{"strip-path", "../../etc/passwd.txt", "passwd.txt", false},
		{"strip-null", "bad\x00name.png", "badname.png", false},
		{"illegal-chars", `a|b*c?<d>.png`, "a_b_c__d_.png", false},
		{"control-chars", "a\x01b\x02c.txt", "abc.txt", false},
		{"only-dots", "..", "", true},
		{"empty", "", "", true},
		{"utf8-truncate", strings.Repeat("中", 300) + ".txt", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := SanitizeFileName(c.in)
			if c.err {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if c.want != "" && got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
			// 长度不超过 200 rune
			if got != "" && len([]rune(got)) > 200 {
				t.Errorf("name too long after sanitize: %d runes", len([]rune(got)))
			}
		})
	}
}

func TestIsSVGContent(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"plain-svg", `<svg xmlns="..."></svg>`, true},
		{"whitespace-svg", "\n  \t<SVG>", true},
		{"xml-decl-svg", `<?xml version="1.0"?><svg></svg>`, true},
		{"png", "\x89PNG\r\n\x1a\n", false},
		{"json", `{"k":1}`, false},
		{"plain-xml", `<?xml version="1.0"?><root/>`, false},
		{"bom-svg", "\xef\xbb\xbf<svg>", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsSVGContent([]byte(c.body)); got != c.want {
				t.Errorf("got %v, want %v", got, c.want)
			}
		})
	}
}

func TestValidate_HappyPath(t *testing.T) {
	withAttachmentSettings(t, true, 10*1024*1024,
		"png,txt,json,pdf",
		"image/*,text/*,application/json,application/pdf")

	head := []byte("\x89PNG\r\n\x1a\nabcdef")
	res, err := Validate("hello.png", "image/png", int64(len(head)), head)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.SafeName != "hello.png" || res.Ext != "png" || !strings.HasPrefix(res.DetectedMime, "image/") {
		t.Fatalf("bad result: %+v", res)
	}
}

func TestValidate_SVGAlwaysBlocked(t *testing.T) {
	withAttachmentSettings(t, true, 10*1024*1024,
		"svg,png", // 即使管理员误加 svg 到白名单
		"image/*")
	head := []byte(`<svg xmlns="..."></svg>`)
	_, err := Validate("pic.svg", "image/svg+xml", int64(len(head)), head)
	if !errors.Is(err, ErrAttachmentSVGForbidden) {
		t.Fatalf("want ErrAttachmentSVGForbidden, got %v", err)
	}
	// 也要拦截 xxx.png 内容实际是 svg 的伪装
	_, err = Validate("pic.png", "image/png", int64(len(head)), head)
	if !errors.Is(err, ErrAttachmentSVGForbidden) {
		t.Fatalf("want ErrAttachmentSVGForbidden for disguised png, got %v", err)
	}
}

func TestValidate_ExtNotAllowed(t *testing.T) {
	withAttachmentSettings(t, true, 10*1024*1024, "png", "image/*")
	_, err := Validate("a.txt", "text/plain", 10, []byte("hello"))
	if !errors.Is(err, ErrAttachmentExtNotAllowed) {
		t.Fatalf("want ErrAttachmentExtNotAllowed, got %v", err)
	}
}

func TestValidate_MimeMismatchBlocksExecutableDisguise(t *testing.T) {
	withAttachmentSettings(t, true, 10*1024*1024, "txt,png", "text/*,image/*")
	// 声明 text/plain 但实际是 PNG 魔数 → 不同大类，应被拒。
	head := []byte("\x89PNG\r\n\x1a\n")
	_, err := Validate("fake.txt", "text/plain", int64(len(head)), head)
	if !errors.Is(err, ErrAttachmentMimeMismatch) {
		t.Fatalf("want ErrAttachmentMimeMismatch, got %v", err)
	}
}

func TestValidate_TextFamilyTolerance(t *testing.T) {
	// json 嗅探常常是 text/plain；应通过。
	withAttachmentSettings(t, true, 10*1024*1024, "json,txt", "application/json,text/*")
	head := []byte(`{"a":1,"b":"x"}`)
	res, err := Validate("data.json", "application/json", int64(len(head)), head)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Ext != "json" {
		t.Fatalf("bad ext %q", res.Ext)
	}
}

func TestValidate_OversizedRejected(t *testing.T) {
	withAttachmentSettings(t, true, 100, "png", "image/*")
	head := []byte("\x89PNG\r\n\x1a\n")
	_, err := Validate("x.png", "image/png", 2000, head)
	if !errors.Is(err, ErrAttachmentSizeExceeded) {
		t.Fatalf("want ErrAttachmentSizeExceeded, got %v", err)
	}
}

func TestValidate_DisabledFeature(t *testing.T) {
	withAttachmentSettings(t, false, 10*1024*1024, "png", "image/*")
	head := []byte("\x89PNG\r\n\x1a\n")
	_, err := Validate("x.png", "image/png", int64(len(head)), head)
	if !errors.Is(err, ErrAttachmentDisabled) {
		t.Fatalf("want ErrAttachmentDisabled, got %v", err)
	}
}

func TestValidate_EmptyRejected(t *testing.T) {
	withAttachmentSettings(t, true, 10*1024*1024, "png,txt", "image/*,text/*")
	_, err := Validate("a.txt", "text/plain", 0, nil)
	if !errors.Is(err, ErrAttachmentEmpty) {
		t.Fatalf("want ErrAttachmentEmpty, got %v", err)
	}
}

func TestValidate_FileNameWithoutExt(t *testing.T) {
	withAttachmentSettings(t, true, 10*1024*1024, "txt", "text/*")
	_, err := Validate("Makefile", "text/plain", 5, []byte("hello"))
	if !errors.Is(err, ErrAttachmentExtNotAllowed) {
		t.Fatalf("want ErrAttachmentExtNotAllowed, got %v", err)
	}
}

func TestReadSniff_RebuildsStream(t *testing.T) {
	full := strings.Repeat("A", 1200)
	head, combined, err := ReadSniff(strings.NewReader(full))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(head) != 512 {
		t.Fatalf("expected 512 head bytes, got %d", len(head))
	}
	got, err := io.ReadAll(combined)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(got) != full {
		t.Fatalf("combined stream mismatch: len=%d", len(got))
	}
}

func TestReadSniff_ShortStream(t *testing.T) {
	head, combined, err := ReadSniff(strings.NewReader("abc"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(head) != "abc" {
		t.Fatalf("expected 'abc', got %q", string(head))
	}
	b, _ := io.ReadAll(combined)
	if string(b) != "abc" {
		t.Fatalf("combined mismatch: %q", b)
	}
}

// 额外一道保险：sameMimeFamily 应识别 application/json ↔ text/plain。
func TestSameMimeFamily(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"application/json", "text/plain", true},
		{"image/png", "image/jpeg", true},
		{"image/png", "text/plain", false},
		{"application/pdf", "application/json", false},
		{"text/xml", "application/xml", true},
	}
	for _, c := range cases {
		if got := sameMimeFamily(c.a, c.b); got != c.want {
			t.Errorf("sameMimeFamily(%q,%q)=%v, want %v", c.a, c.b, got, c.want)
		}
	}
	// 防御性：零字节 head 不 panic
	if got := DetectMime(nil); got == "" {
		t.Errorf("DetectMime(nil) returned empty; expected fallback type")
	}
	_ = bytes.NewReader // keep import
}
