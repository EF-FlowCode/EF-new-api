package requestip

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

func TestGetClientIPDefaultsToRemoteAddr(t *testing.T) {
	oldCfg := *operation_setting.GetRiskControlSetting()
	defer func() {
		*operation_setting.GetRiskControlSetting() = oldCfg
	}()

	cfg := operation_setting.GetRiskControlSetting()
	cfg.TrustedIPHeaderEnabled = false
	cfg.TrustedIPHeader = "X-Real-IP"

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "198.51.100.10:4321"
	req.Header.Set("X-Real-IP", "203.0.113.7")
	ctx.Request = req

	if got := GetClientIP(ctx); got != "198.51.100.10" {
		t.Fatalf("GetClientIP() = %q, want %q", got, "198.51.100.10")
	}
}

func TestGetClientIPUsesTrustedHeaderWhenEnabled(t *testing.T) {
	oldCfg := *operation_setting.GetRiskControlSetting()
	defer func() {
		*operation_setting.GetRiskControlSetting() = oldCfg
	}()

	cfg := operation_setting.GetRiskControlSetting()
	cfg.TrustedIPHeaderEnabled = true
	cfg.TrustedIPHeader = "CF-Connecting-IP"

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "198.51.100.10:4321"
	req.Header.Set("CF-Connecting-IP", "203.0.113.8")
	ctx.Request = req

	if got := GetClientIP(ctx); got != "203.0.113.8" {
		t.Fatalf("GetClientIP() = %q, want %q", got, "203.0.113.8")
	}
}

func TestGetClientIPParsesForwardedStyleHeader(t *testing.T) {
	oldCfg := *operation_setting.GetRiskControlSetting()
	defer func() {
		*operation_setting.GetRiskControlSetting() = oldCfg
	}()

	cfg := operation_setting.GetRiskControlSetting()
	cfg.TrustedIPHeaderEnabled = true
	cfg.TrustedIPHeader = "X-Forwarded-For"

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "198.51.100.10:4321"
	req.Header.Set("X-Forwarded-For", "203.0.113.9, 10.0.0.1")
	ctx.Request = req

	if got := GetClientIP(ctx); got != "203.0.113.9" {
		t.Fatalf("GetClientIP() = %q, want %q", got, "203.0.113.9")
	}
}

func TestGetClientIPFallsBackWhenTrustedHeaderInvalid(t *testing.T) {
	oldCfg := *operation_setting.GetRiskControlSetting()
	defer func() {
		*operation_setting.GetRiskControlSetting() = oldCfg
	}()

	cfg := operation_setting.GetRiskControlSetting()
	cfg.TrustedIPHeaderEnabled = true
	cfg.TrustedIPHeader = "X-Real-IP"

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "198.51.100.10:4321"
	req.Header.Set("X-Real-IP", "not-an-ip")
	ctx.Request = req

	if got := GetClientIP(ctx); got != "198.51.100.10" {
		t.Fatalf("GetClientIP() = %q, want %q", got, "198.51.100.10")
	}
}

func TestDiagnoseRequestRecommendsTrustedHeader(t *testing.T) {
	oldCfg := *operation_setting.GetRiskControlSetting()
	defer func() {
		*operation_setting.GetRiskControlSetting() = oldCfg
	}()

	cfg := operation_setting.GetRiskControlSetting()
	cfg.TrustedIPHeaderEnabled = false
	cfg.TrustedIPHeader = "X-Real-IP"

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "172.17.0.1:4321"
	req.Header.Set("X-Real-IP", "203.0.113.5")
	req.Header.Set("X-Forwarded-For", "203.0.113.5, 172.17.0.1")
	ctx.Request = req

	diag := DiagnoseRequest(ctx)
	if diag.RecommendedMode != "trusted_header" {
		t.Fatalf("RecommendedMode = %q, want %q", diag.RecommendedMode, "trusted_header")
	}
	if diag.RecommendedHeader != "X-Real-IP" {
		t.Fatalf("RecommendedHeader = %q, want %q", diag.RecommendedHeader, "X-Real-IP")
	}
	if diag.EffectiveClientIP != "172.17.0.1" {
		t.Fatalf("EffectiveClientIP = %q, want %q", diag.EffectiveClientIP, "172.17.0.1")
	}
}

func TestDiagnoseRequestRecommendsRemoteAddrWhenPublic(t *testing.T) {
	oldCfg := *operation_setting.GetRiskControlSetting()
	defer func() {
		*operation_setting.GetRiskControlSetting() = oldCfg
	}()

	cfg := operation_setting.GetRiskControlSetting()
	cfg.TrustedIPHeaderEnabled = false
	cfg.TrustedIPHeader = "X-Real-IP"

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "203.0.113.9:4321"
	ctx.Request = req

	diag := DiagnoseRequest(ctx)
	if diag.RecommendedMode != "remote_addr" {
		t.Fatalf("RecommendedMode = %q, want %q", diag.RecommendedMode, "remote_addr")
	}
	if diag.RecommendedHeader != "" {
		t.Fatalf("RecommendedHeader = %q, want empty", diag.RecommendedHeader)
	}
	if diag.EffectiveClientIP != "203.0.113.9" {
		t.Fatalf("EffectiveClientIP = %q, want %q", diag.EffectiveClientIP, "203.0.113.9")
	}
}
