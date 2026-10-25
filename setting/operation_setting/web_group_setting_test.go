package operation_setting

import "testing"

func TestWebGroupSettingRegionRule(t *testing.T) {
	setting := WebGroupSetting{
		Enabled: true,
		Rules: map[string]WebGroupRegionRule{
			"CN": {AllowedGroups: []string{"mainland"}},
			"*":  {AllowedGroups: []string{"global"}},
		},
	}

	if rule, ok := setting.GetRegionRule("cn"); !ok || len(rule.AllowedGroups) != 1 || rule.AllowedGroups[0] != "mainland" {
		t.Fatalf("expected case-insensitive CN rule, got %#v, %v", rule, ok)
	}
	if rule, ok := setting.GetRegionRule("US"); !ok || rule.AllowedGroups[0] != "global" {
		t.Fatalf("expected wildcard rule, got %#v, %v", rule, ok)
	}
	if !setting.IsGroupAvailable("CN", "mainland") {
		t.Fatal("expected mainland group to be available in CN")
	}
	if setting.IsGroupAvailable("CN", "global") {
		t.Fatal("expected global group to be unavailable in CN")
	}
}

func TestWebGroupSettingDisabledAllowsAllGroups(t *testing.T) {
	setting := WebGroupSetting{
		Enabled: false,
		Rules: map[string]WebGroupRegionRule{
			"CN": {AllowedGroups: []string{}},
		},
	}
	if !setting.IsGroupAvailable("CN", "any") {
		t.Fatal("disabled web group setting must not restrict groups")
	}
}
