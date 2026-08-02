package service

import (
	"testing"

	"github.com/QuantumNous/new-api/setting"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
)

func TestGetUserEffectiveGroupsMergesPrimaryAndAdditionalCustomerTiers(t *testing.T) {
	originalRatios := ratio_setting.GroupRatio2JSONString()
	originalUsable := setting.UserUsableGroups2JSONString()
	specialGroups := ratio_setting.GetGroupRatioSetting().GroupSpecialUsableGroup
	originalSpecial := specialGroups.ReadAll()
	t.Cleanup(func() {
		_ = ratio_setting.UpdateGroupRatioByJSONString(originalRatios)
		_ = setting.UpdateUserUsableGroupsByJSONString(originalUsable)
		specialGroups.Clear()
		specialGroups.AddAll(originalSpecial)
	})
	_ = ratio_setting.UpdateGroupRatioByJSONString(`{"base":1,"vip_only":1,"svip_only":1}`)
	_ = setting.UpdateUserUsableGroupsByJSONString(`{"base":"Base"}`)
	specialGroups.Clear()
	specialGroups.AddAll(map[string]map[string]string{
		"VIP":  {"+:vip_only": "VIP only"},
		"SVIP": {"+:svip_only": "SVIP only"},
	})

	groups := GetUserEffectiveGroups("VIP", []string{"SVIP"})
	if len(groups) != 3 {
		t.Fatalf("expected merged token groups, got %#v", groups)
	}
	if _, ok := groups["base"]; !ok {
		t.Fatal("globally usable group should remain authorized")
	}
	if _, ok := groups["vip_only"]; !ok {
		t.Fatal("primary customer tier permissions should be included")
	}
	if _, ok := groups["svip_only"]; !ok {
		t.Fatal("additional customer tier permissions should be included")
	}
}

func TestGetUserEffectiveGroupsInheritsWhenUnset(t *testing.T) {
	groups := GetUserEffectiveGroups("vip", nil)
	if _, ok := groups["vip"]; !ok {
		t.Fatal("unset allowlist should inherit the primary customer tier")
	}
}

func TestGetUserEffectiveGroupsFiltersNonUserTokenGroups(t *testing.T) {
	originalRatios := ratio_setting.GroupRatio2JSONString()
	originalUsable := setting.UserUsableGroups2JSONString()
	t.Cleanup(func() {
		_ = ratio_setting.UpdateGroupRatioByJSONString(originalRatios)
		_ = setting.UpdateUserUsableGroupsByJSONString(originalUsable)
	})

	_ = ratio_setting.UpdateGroupRatioByJSONString(`{"visible":1,"internal":1}`)
	_ = setting.UpdateUserUsableGroupsByJSONString(`{"visible":"Visible"}`)

	groups := GetUserEffectiveGroups("SVIP", []string{"visible", "internal"})
	if _, ok := groups["visible"]; !ok {
		t.Fatal("user-visible token group should remain authorized")
	}
	if _, ok := groups["internal"]; ok {
		t.Fatal("internal routing group must not be authorized to a user")
	}
}

func TestGetUserGroupRatioUsesLowestCustomerTierOverride(t *testing.T) {
	originalRatios := ratio_setting.GroupRatio2JSONString()
	originalOverrides := ratio_setting.GroupGroupRatio2JSONString()
	t.Cleanup(func() {
		_ = ratio_setting.UpdateGroupRatioByJSONString(originalRatios)
		_ = ratio_setting.UpdateGroupGroupRatioByJSONString(originalOverrides)
	})
	_ = ratio_setting.UpdateGroupRatioByJSONString(`{"token_group":1.2}`)
	_ = ratio_setting.UpdateGroupGroupRatioByJSONString(`{"VIP":{"token_group":0.9},"SVIP":{"token_group":0.8}}`)

	if got, overridden := GetUserGroupRatio("VIP", []string{"SVIP"}, "token_group"); got != 0.8 || !overridden {
		t.Fatalf("GetUserGroupRatio() = %v, want lowest tier override 0.8", got)
	}
}
