package router

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/internal/testdb"
	"eflow.local/new-api/model"
	"eflow.local/new-api/setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// Polling a background response must not spend model request allowance; it
// only uses the separate per-user task query bucket.
func TestTaskPollingUsesQueryLimitNotModelLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testdb.OpenMySQL8(t, &model.User{}, &model.Token{}, &model.Ability{}, &model.Task{})
	originalDB, originalLogDB, originalRedis := model.DB, model.LOG_DB, common.RedisEnabled
	originalQuery := []any{common.TaskQueryRateLimitEnable, common.TaskQueryRateLimitNum, common.TaskQueryRateLimitDuration}
	previous := setting.GetModelRequestRateLimitSettings()
	model.DB, model.LOG_DB, common.RedisEnabled = db, db, false
	common.TaskQueryRateLimitEnable, common.TaskQueryRateLimitNum, common.TaskQueryRateLimitDuration = true, 3, 60
	t.Cleanup(func() {
		model.DB, model.LOG_DB, common.RedisEnabled = originalDB, originalLogDB, originalRedis
		common.TaskQueryRateLimitEnable = originalQuery[0].(bool)
		common.TaskQueryRateLimitNum = originalQuery[1].(int)
		common.TaskQueryRateLimitDuration = originalQuery[2].(int64)
		require.NoError(t, setting.UpdateModelRequestRateLimitSettings(map[string]string{
			"ModelRequestRateLimitEnabled":         strconv.FormatBool(previous.Enabled),
			"ModelRequestRateLimitDurationMinutes": strconv.Itoa(previous.DurationMinutes),
			"ModelRequestRateLimitCount":           strconv.Itoa(previous.TotalCount),
			"ModelRequestRateLimitSuccessCount":    strconv.Itoa(max(previous.SuccessCount, 1)),
		}))
	})
	// A model limit of one request would reject the second poll if it applied.
	require.NoError(t, setting.UpdateModelRequestRateLimitSettings(map[string]string{
		"ModelRequestRateLimitEnabled":         "true",
		"ModelRequestRateLimitDurationMinutes": "1",
		"ModelRequestRateLimitCount":           "1",
		"ModelRequestRateLimitSuccessCount":    "1",
	}))

	user := model.User{Username: "task-poll-user", Status: common.UserStatusEnabled, Group: "default", Quota: 100}
	require.NoError(t, model.DB.Create(&user).Error)
	require.NoError(t, model.DB.Create(&model.Token{UserId: user.Id, Key: "taskpolltestkey", Status: common.TokenStatusEnabled, ExpiredTime: -1, UnlimitedQuota: true}).Error)

	engine := gin.New()
	SetRelayRouter(engine)
	get := func(path string) int {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer taskpolltestkey")
		engine.ServeHTTP(rec, req)
		return rec.Code
	}
	// Ownership is still checked: another user's or a missing task is 404.
	require.Equal(t, http.StatusNotFound, get("/v1/responses/resp_missing"))
	require.Equal(t, http.StatusNotFound, get("/v1/responses/resp_missing/content/audio"))
	require.Equal(t, http.StatusNotFound, get("/v1/tasks/task_missing"))
	require.Equal(t, http.StatusTooManyRequests, get("/v1/responses/resp_missing"))
}
