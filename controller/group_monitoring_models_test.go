package controller

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	perfmetrics "eflow.local/new-api/pkg/perf_metrics"
	"eflow.local/new-api/setting/config"
	"eflow.local/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

func TestMonitoringModelsIgnoresClientScope(t *testing.T) {
	previous, err := json.Marshal(operation_setting.GetGroupMonitoringSetting().MonitoringGroups)
	if err != nil {
		t.Fatal(err)
	}
	const key = "group_monitoring_setting.monitoring_groups"
	if err := config.GlobalConfig.LoadFromDB(map[string]string{key: "[]"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := config.GlobalConfig.LoadFromDB(map[string]string{key: string(previous)}); err != nil {
			t.Error(err)
		}
	})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/monitoring/public/models?group=private&model=secret&hours=720", nil)
	GetMonitoringModels(ctx)
	var response struct {
		Success bool                               `json:"success"`
		Data    perfmetrics.MonitoringModelsResult `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusOK || !response.Success || response.Data.Groups == nil || len(response.Data.Groups) != 0 {
		t.Fatalf("client parameters must not expand the empty server allowlist: %s", recorder.Body.String())
	}
	if response.Data.EndTime-response.Data.StartTime != 24*3600 || response.Data.BucketSeconds != 3600 {
		t.Fatalf("client parameters must not expand the window: %+v", response.Data)
	}
}
