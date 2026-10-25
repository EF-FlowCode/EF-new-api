package controller

import (
	"net/http"
	"testing"

	"eflow.local/new-api/dto"
)

func TestApplyRatioSyncAuth(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://upstream.example/api/pricing", nil)
	if err != nil {
		t.Fatal(err)
	}
	settings := dto.ChannelOtherSettings{RatioSyncAccessToken: "test-token", RatioSyncUserID: 39}
	if err = applyRatioSyncAuth(req, "https://upstream.example", settings); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer test-token" {
		t.Fatalf("unexpected Authorization header: %q", got)
	}
	if got := req.Header.Get("New-Api-User"); got != "39" {
		t.Fatalf("unexpected New-Api-User header: %q", got)
	}

	crossOrigin, err := http.NewRequest(http.MethodGet, "https://evil.example/api/pricing", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = applyRatioSyncAuth(crossOrigin, "https://upstream.example", settings); err == nil {
		t.Fatal("expected cross-origin authenticated request to be rejected")
	}
}

func TestApplyRatioSyncAuthScopedTokenWithoutUserID(t *testing.T) {
	newRequest := func() *http.Request {
		req, err := http.NewRequest(http.MethodGet, "https://upstream.example/api/pricing", nil)
		if err != nil {
			t.Fatal(err)
		}
		return req
	}
	scoped := "nap_" + "a1B2c3D4e5F6g7H8i9J0k1L2m3N4o5P6q7R8s9T0u1V"
	req := newRequest()
	if err := applyRatioSyncAuth(req, "https://upstream.example", dto.ChannelOtherSettings{RatioSyncAccessToken: scoped}); err != nil {
		t.Fatalf("scoped token without user id must be accepted: %v", err)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer "+scoped {
		t.Fatalf("unexpected Authorization header: %q", got)
	}
	if got := req.Header.Get("New-Api-User"); got != "" {
		t.Fatalf("New-Api-User must be omitted for a scoped token without user id, got %q", got)
	}

	req = newRequest()
	if err := applyRatioSyncAuth(req, "https://upstream.example", dto.ChannelOtherSettings{RatioSyncAccessToken: "Bearer " + scoped, RatioSyncUserID: 7}); err != nil {
		t.Fatal(err)
	}
	if got := req.Header.Get("New-Api-User"); got != "7" {
		t.Fatalf("explicit user id must still be sent, got %q", got)
	}

	for _, settings := range []dto.ChannelOtherSettings{
		{RatioSyncAccessToken: "legacy-token"},
		{RatioSyncUserID: 7},
		{RatioSyncAccessToken: scoped, RatioSyncUserID: -1},
	} {
		if err := applyRatioSyncAuth(newRequest(), "https://upstream.example", settings); err == nil {
			t.Fatalf("expected incomplete configuration to be rejected: %+v", settings.RatioSyncUserID)
		}
	}
}
