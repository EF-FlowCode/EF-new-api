package setting

import "testing"

func withExts(t *testing.T, value string, fn func()) {
	t.Helper()
	prev := GetTicketAttachmentSettings().AllowedExts
	if err := UpdateTicketAttachmentSettings(map[string]string{"TicketAttachmentAllowedExts": value}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = UpdateTicketAttachmentSettings(map[string]string{"TicketAttachmentAllowedExts": prev}) })
	fn()
}

func withMimes(t *testing.T, value string, fn func()) {
	t.Helper()
	prev := GetTicketAttachmentSettings().AllowedMimes
	if err := UpdateTicketAttachmentSettings(map[string]string{"TicketAttachmentAllowedMimes": value}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = UpdateTicketAttachmentSettings(map[string]string{"TicketAttachmentAllowedMimes": prev}) })
	fn()
}

func TestAllowedExtList_Normalizes(t *testing.T) {
	withExts(t, "JPG, .PNG,  ,txt", func() {
		got := TicketAttachmentAllowedExtList()
		want := []string{"jpg", "png", "txt"}
		if len(got) != len(want) {
			t.Fatalf("len = %d, want %d, got %v", len(got), len(want), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("index %d = %q, want %q", i, got[i], want[i])
			}
		}
	})
}

func TestIsExtAllowed(t *testing.T) {
	withExts(t, "jpg,png,json,pdf", func() {
		cases := []struct {
			in   string
			want bool
		}{
			{"jpg", true},
			{".JPG", true},
			{"JpG", true},
			{"svg", false},
			{"", false},
			{"../etc/passwd", false}, // 整体不匹配
		}
		for _, c := range cases {
			if got := IsTicketAttachmentExtAllowed(c.in); got != c.want {
				t.Errorf("IsTicketAttachmentExtAllowed(%q) = %v, want %v", c.in, got, c.want)
			}
		}
	})
}

func TestIsMimeAllowed_Wildcard(t *testing.T) {
	withMimes(t, "image/*,application/json,text/*", func() {
		cases := []struct {
			in   string
			want bool
		}{
			{"image/png", true},
			{"image/jpeg", true},
			{"application/json", true},
			{"application/json; charset=utf-8", true}, // 参数应被裁掉
			{"text/plain", true},
			{"application/pdf", false}, // 未在通配符里
			{"", false},
			{"IMAGE/PNG", true}, // 大小写不敏感
		}
		for _, c := range cases {
			if got := IsTicketAttachmentMimeAllowed(c.in); got != c.want {
				t.Errorf("IsTicketAttachmentMimeAllowed(%q) = %v, want %v", c.in, got, c.want)
			}
		}
	})
}
