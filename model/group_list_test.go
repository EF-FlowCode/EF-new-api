package model

import (
	"testing"

	"eflow.local/new-api/common"
)

func TestGroupListNormalizesAndMarshalsAsArray(t *testing.T) {
	groups := NewGroupList([]string{" svip ", "vip", "vip", ""})
	if string(groups) != `["svip","vip"]` {
		t.Fatalf("unexpected stored value: %s", groups)
	}
	data, err := common.Marshal(groups)
	if err != nil {
		t.Fatalf("marshal group list: %v", err)
	}
	if string(data) != `["svip","vip"]` {
		t.Fatalf("unexpected API value: %s", data)
	}
}
