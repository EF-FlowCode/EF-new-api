package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
)

func TestFormatUserLogsPreservesStreamStatus(t *testing.T) {
	logs := []*Log{{Other: `{"admin_info":{"secret":true},"stream_status":{"status":"interrupted"}}`}}

	formatUserLogs(logs, 3)

	var other map[string]any
	if err := common.UnmarshalJsonStr(logs[0].Other, &other); err != nil {
		t.Fatal(err)
	}
	if _, ok := other["admin_info"]; ok {
		t.Fatal("admin_info must remain hidden from log owners")
	}
	if _, ok := other["stream_status"]; !ok {
		t.Fatal("stream_status should be visible to the log owner")
	}
	if logs[0].Id != 4 {
		t.Fatalf("unexpected display log id: %d", logs[0].Id)
	}
}
