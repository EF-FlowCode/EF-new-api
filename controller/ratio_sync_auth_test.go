package controller

import (
	"net/http"
	"testing"

	"github.com/QuantumNous/new-api/dto"
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
