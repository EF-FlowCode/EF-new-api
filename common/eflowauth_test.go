package common

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"
)

func withEflowAuthSecret(t *testing.T, secret string) {
	t.Helper()
	previous := eflowAuthSecret
	eflowAuthSecret = secret
	t.Cleanup(func() { eflowAuthSecret = previous })
}

func TestIssueEflowAuthUsesMainSessionTTL(t *testing.T) {
	withEflowAuthSecret(t, "test-secret")

	before := time.Now().Unix()
	token, err := IssueEflowAuth(42)
	if err != nil {
		t.Fatalf("IssueEflowAuth() error = %v", err)
	}

	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token parts = %d, want 3", len(parts))
	}
	body, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	var claims eflowClaims
	if err := Unmarshal(body, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	if claims.Uid != 42 {
		t.Fatalf("claims.Uid = %d, want 42", claims.Uid)
	}
	if claims.Exp-claims.Iat != EflowAuthTTLSeconds {
		t.Fatalf("token TTL = %d, want %d", claims.Exp-claims.Iat, EflowAuthTTLSeconds)
	}
	if claims.Iat < before || claims.Iat > time.Now().Unix() {
		t.Fatalf("claims.Iat = %d, outside test window", claims.Iat)
	}
	if EflowAuthTTLSeconds != int64(30*24*3600) {
		t.Fatalf("EflowAuthTTLSeconds = %d, want 30 days", EflowAuthTTLSeconds)
	}

	uid, ok := VerifyEflowAuth(token)
	if !ok || uid != 42 {
		t.Fatalf("VerifyEflowAuth() = (%d, %v), want (42, true)", uid, ok)
	}
}

func TestVerifyEflowAuthRejectsInvalidSignature(t *testing.T) {
	withEflowAuthSecret(t, "test-secret")
	token, err := IssueEflowAuth(42)
	if err != nil {
		t.Fatalf("IssueEflowAuth() error = %v", err)
	}

	if uid, ok := VerifyEflowAuth(token + "tampered"); ok || uid != 0 {
		t.Fatalf("VerifyEflowAuth(tampered) = (%d, %v), want (0, false)", uid, ok)
	}
}

func TestEflowAuthIsBoundToItsSession(t *testing.T) {
	withEflowAuthSecret(t, "test-secret")
	token, err := IssueEflowAuthForSession(42, "session-a")
	if err != nil {
		t.Fatal(err)
	}
	if uid, ok := VerifyEflowAuth(token); !ok || uid != 42 {
		t.Fatalf("sub-sites keep verifying uid/exp: uid=%d ok=%v", uid, ok)
	}
	if !EflowAuthMatchesSession(token, 42, "session-a") {
		t.Fatal("token must match the session that issued it")
	}
	if EflowAuthMatchesSession(token, 42, "session-b") || EflowAuthMatchesSession(token, 7, "session-a") {
		t.Fatal("a token from another session or account must be reissued")
	}
	legacy, err := IssueEflowAuth(42)
	if err != nil {
		t.Fatal(err)
	}
	if EflowAuthMatchesSession(legacy, 42, "session-a") {
		t.Fatal("tokens without a session are reissued on the next request")
	}
	if EflowAuthMatchesSession(token[:len(token)-2]+"xx", 42, "session-a") {
		t.Fatal("tampered tokens never match")
	}
}
