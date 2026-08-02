package controller

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

// TestUnblockRiskSubjectRequiresGroupQuery pins the v4 contract: the
// /api/risk/subjects/:scope/:id/unblock endpoint must reject calls that omit
// the ?group= query (DEV_GUIDE §12.2).
func TestUnblockRiskSubjectRequiresGroupQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/api/risk/subjects/token/123/unblock", nil)
	c.Params = gin.Params{
		gin.Param{Key: "scope", Value: "token"},
		gin.Param{Key: "id", Value: "123"},
	}
	UnblockRiskSubject(c)
	if w.Code >= 300 {
		t.Fatalf("ApiError uses HTTP 200 with success=false; got status=%d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "解封必须指定分组") {
		t.Fatalf("expected message to mention required group, got %q", body)
	}
}

// TestSortRiskGroupsEnabledFirst documents the listing order for
// GET /api/risk/groups: enabled groups float above disabled ones, then
// alphabetic order tiebreaker. This stabilizes the matrix UX.
func TestSortRiskGroupsEnabledFirst(t *testing.T) {
	names := []string{"zeta", "alpha", "beta", "gamma"}
	whitelist := map[string]struct{}{"gamma": {}, "alpha": {}}
	sortRiskGroups(names, whitelist)
	want := []string{"alpha", "gamma", "beta", "zeta"}
	for i, n := range names {
		if n != want[i] {
			t.Fatalf("position %d got %q want %q (full=%v)", i, n, want[i], names)
		}
	}
}

// TestNormalizeFiltersAutoFromControllerInputs is a smoke test that ties the
// controller path to operation_setting.Normalize: posting a config with auto
// in EnabledGroups must drop it.
func TestNormalizeFiltersAutoFromControllerInputs(t *testing.T) {
	cfg := &operation_setting.RiskControlSetting{
		EnabledGroups: []string{"vip", operation_setting.RiskControlAutoGroup, "free"},
		GroupModes: map[string]string{
			"vip":  operation_setting.RiskControlModeEnforce,
			"auto": operation_setting.RiskControlModeEnforce,
		},
	}
	operation_setting.NormalizeRiskControlSetting(cfg)
	for _, g := range cfg.EnabledGroups {
		if g == operation_setting.RiskControlAutoGroup {
			t.Fatalf("auto leaked into EnabledGroups: %v", cfg.EnabledGroups)
		}
	}
	if _, ok := cfg.GroupModes[operation_setting.RiskControlAutoGroup]; ok {
		t.Fatal("auto leaked into GroupModes")
	}
}
