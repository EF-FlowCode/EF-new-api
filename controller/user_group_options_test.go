package controller

import (
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
)

func TestListUserGroupOptionsIncludesDefaultFallback(t *testing.T) {
	originalTopupRatios := common.TopupGroupRatio2JSONString()
	t.Cleanup(func() { _ = common.UpdateTopupGroupRatioByJSONString(originalTopupRatios) })
	_ = common.UpdateTopupGroupRatioByJSONString(`{"SVIP":1}`)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ListUserGroupOptions(ctx)

	var response struct {
		Data []string `json:"data"`
	}
	if err := common.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if want := []string{"SVIP", "default"}; !reflect.DeepEqual(response.Data, want) {
		t.Fatalf("ListUserGroupOptions() = %v, want %v", response.Data, want)
	}
}

func TestValidateAdditionalCustomerTiersUsesCustomerTierSource(t *testing.T) {
	originalTopupRatios := common.TopupGroupRatio2JSONString()
	t.Cleanup(func() { _ = common.UpdateTopupGroupRatioByJSONString(originalTopupRatios) })
	_ = common.UpdateTopupGroupRatioByJSONString(`{"SVIP":1}`)

	if err := validateAdditionalCustomerTiers(model.NewGroupList([]string{"SVIP", "default"})); err != nil {
		t.Fatalf("configured customer tiers should be accepted: %v", err)
	}
	if err := validateAdditionalCustomerTiers(model.NewGroupList([]string{"internal_token_group"})); err == nil {
		t.Fatal("token groups must not be accepted as additional customer tiers")
	}
}

func TestValidateCustomerTierAllowsDefaultFallbackAndRejectsTokenGroup(t *testing.T) {
	originalTopupRatios := common.TopupGroupRatio2JSONString()
	t.Cleanup(func() { _ = common.UpdateTopupGroupRatioByJSONString(originalTopupRatios) })
	_ = common.UpdateTopupGroupRatioByJSONString(`{"SVIP":1}`)

	if err := validateCustomerTier("SVIP"); err != nil {
		t.Fatalf("configured customer tier should be accepted: %v", err)
	}
	if err := validateCustomerTier("default"); err != nil {
		t.Fatalf("built-in default tier should be accepted: %v", err)
	}
	if err := validateCustomerTier("internal_token_group"); err == nil {
		t.Fatal("token group should be rejected as a customer tier")
	}
}
