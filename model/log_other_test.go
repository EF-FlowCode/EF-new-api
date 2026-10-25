package model

import (
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/internal/testdb"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestLogOtherWritersWithUnmigratedHistory(t *testing.T) {
	db := testdb.OpenMySQL8(t, &Log{}, &User{})
	oldDB, oldLogDB, oldRedis := DB, LOG_DB, common.RedisEnabled
	settings := common.GetFeatureSettings()
	DB, LOG_DB, common.RedisEnabled = db, db, false
	require.NoError(t, common.UpdateFeatureSettings(map[string]string{"LogConsumeEnabled": "true", "DataExportEnabled": "false"}))
	t.Cleanup(func() {
		DB, LOG_DB, common.RedisEnabled = oldDB, oldLogDB, oldRedis
		require.NoError(t, common.UpdateFeatureSettings(map[string]string{
			"LogConsumeEnabled": strconv.FormatBool(settings.LogConsumeEnabled),
			"DataExportEnabled": strconv.FormatBool(settings.DataExportEnabled),
		}))
	})
	user := User{Username: "log-format-fixture", Setting: "{}"}
	require.NoError(t, db.Create(&user).Error)
	legacy := Log{UserId: user.Id, Type: LogTypeConsume, Quota: 73,
		Other: `{"channel_id":9,"channel_name":"private","channel_type":17,"reject_reason":"legacy","reasoning_effort":"high","large":9007199254740993}`}
	require.NoError(t, db.Create(&legacy).Error)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Set("username", user.Username)
	other := map[string]any{"channel_id": 9, "channel_name": "private", "channel_type": 17, "reject_reason": "legacy", "reasoning_effort": "high"}
	RecordErrorLog(c, user.Id, 9, "fixture", "", "error", 0, 1, false, "default", other)
	RecordConsumeLog(c, user.Id, RecordConsumeLogParams{ChannelId: 9, ModelName: "fixture", Quota: 73, Other: other})
	RecordTaskBillingLog(RecordTaskBillingLogParams{UserId: user.Id, LogType: LogTypeConsume, ChannelId: 9, Quota: 73, Other: other})
	RecordLogWithAdminInfo(user.Id, LogTypeManage, "manage", other)
	RecordTopupLog(user.Id, "topup", "127.0.0.1", "fixture", "fixture")
	var rows []Log
	require.NoError(t, db.Order("id").Find(&rows).Error)
	require.Len(t, rows, 6, "all real writers must persist a row")
	require.Equal(t, legacy.Other, rows[0].Other, "serving new writes must not require rewriting history")
	for i, row := range rows {
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(row.Other), &fields))
		if i > 0 {
			for _, key := range privateLogOtherKeys {
				require.NotContains(t, fields, key)
			}
			require.Contains(t, fields, "admin_info")
		}
		if i <= 3 {
			admin := row
			FormatAdminLogs([]*Log{&admin}, common.RoleAdminUser)
			var response map[string]json.RawMessage
			require.NoError(t, json.Unmarshal([]byte(admin.Other), &response))
			require.Equal(t, `"legacy"`, string(response["reject_reason"]))
			require.Equal(t, `"high"`, string(response["reasoning_effort"]))
		}
		public := row
		formatUserLogs([]*Log{&public}, 0)
		var visible map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(public.Other), &visible))
		for _, key := range append(append([]string{}, privateLogOtherKeys[:]...), "admin_info", "root_info", "audit_info") {
			require.NotContains(t, visible, key)
		}
		require.Equal(t, row.Quota, public.Quota)
	}
}

func TestLogOtherRoleProjection(t *testing.T) {
	value := `{"channel_id":9,"channel_name":"secret","channel_type":17,"reject_reason":"legacy","admin_info":{"reject_reason":"current","key":3},"root_info":{"secret":true},"audit_info":{"actor":1},"reasoning_effort":"high","stream_status":{"status":"interrupted"},"large":9007199254740993}`
	for _, role := range []int{common.RoleCommonUser, common.RoleFinanceUser, common.RoleAdminUser, common.RoleRootUser} {
		var fields map[string]json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(projectLogOther(value, role)), &fields))
		require.Equal(t, "9007199254740993", string(fields["large"]))
		require.Contains(t, fields, "reasoning_effort")
		require.Contains(t, fields, "stream_status")
		for _, key := range privateLogOtherKeys {
			require.NotContains(t, fields, key)
		}
		if role < common.RoleAdminUser {
			for _, key := range []string{"admin_info", "audit_info", "root_info"} {
				require.NotContains(t, fields, key)
			}
		} else {
			require.Contains(t, fields, "audit_info")
			require.Contains(t, string(fields["admin_info"]), `"reject_reason":"current"`)
			require.Contains(t, string(fields["admin_info"]), `"channel_id":9`)
			_, root := fields["root_info"]
			require.Equal(t, role == common.RoleRootUser, root)
		}
		require.Equal(t, "{}", projectLogOther("not-json secret", role))
	}
	logs := []*Log{{ChannelId: 9, ChannelName: "secret", BillingAnomaly: 1, Other: value}}
	formatUserLogs(logs, 0)
	require.Zero(t, logs[0].ChannelId)
	require.Empty(t, logs[0].ChannelName)
	require.Zero(t, logs[0].BillingAnomaly)
}

func TestLogOtherStorageMovesPrivateFields(t *testing.T) {
	original := map[string]any{"channel_id": 9, "reject_reason": "private", "reasoning_effort": "high"}
	stored := marshalLogOther(original)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(stored), &fields))
	require.NotContains(t, fields, "channel_id")
	require.NotContains(t, fields, "reject_reason")
	require.Contains(t, string(fields["admin_info"]), "private")
	require.Contains(t, original, "channel_id", "writer input must not be mutated")
}

func TestLogOtherAdminResponseSupportsOldPages(t *testing.T) {
	for _, tc := range []struct {
		name, value, want string
	}{
		{"legacy", `{"reject_reason":"legacy"}`, `"legacy"`},
		{"nested", `{"admin_info":{"reject_reason":"current"}}`, `"current"`},
		{"nested wins", `{"reject_reason":"legacy","admin_info":{"reject_reason":"current"}}`, `"current"`},
		{"missing", `{"reasoning_effort":"high"}`, ""},
		{"invalid", `not-json`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, role := range []int{common.RoleCommonUser, common.RoleFinanceUser, common.RoleAdminUser, common.RoleRootUser} {
				logs := []*Log{{Other: tc.value}}
				FormatAdminLogs(logs, role)
				var fields map[string]json.RawMessage
				require.NoError(t, json.Unmarshal([]byte(logs[0].Other), &fields))
				if role >= common.RoleAdminUser && tc.want != "" {
					require.Equal(t, tc.want, string(fields["reject_reason"]), "old pages read the top-level field")
					var admin map[string]json.RawMessage
					require.NoError(t, json.Unmarshal(fields["admin_info"], &admin))
					require.Equal(t, fields["reject_reason"], admin["reject_reason"])
				} else {
					require.NotContains(t, fields, "reject_reason")
				}
				formatted := logs[0].Other
				FormatAdminLogs(logs, role)
				require.Equal(t, formatted, logs[0].Other)
				formatUserLogs(logs, 0)
				var public map[string]json.RawMessage
				require.NoError(t, json.Unmarshal([]byte(logs[0].Other), &public))
				require.NotContains(t, public, "reject_reason")
				require.NotContains(t, public, "admin_info")
			}
		})
	}
}

func TestLogOtherResponseAliasIsNotStored(t *testing.T) {
	logs := []*Log{{Other: `{"admin_info":{"reject_reason":"private"},"reasoning_effort":"high","large":9007199254740993}`}}
	FormatAdminLogs(logs, common.RoleRootUser)
	var response map[string]any
	require.NoError(t, json.Unmarshal([]byte(logs[0].Other), &response))
	require.Equal(t, "private", response["reject_reason"])
	stored := marshalLogOther(response)
	var fields map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(stored), &fields))
	require.NotContains(t, fields, "reject_reason", "response-only aliases must not be persisted")
	// The old backend's user projection only removes admin_info. New records
	// must remain private even when read by that older implementation.
	delete(fields, "admin_info")
	require.NotContains(t, fields, "reject_reason")
	require.Equal(t, `"high"`, string(fields["reasoning_effort"]))
	var rawResponse map[string]json.RawMessage
	require.NoError(t, json.Unmarshal([]byte(logs[0].Other), &rawResponse))
	require.Equal(t, "9007199254740993", string(rawResponse["large"]))
}

func TestSumUsedQuotaPreservesQuotaAcrossRateQuery(t *testing.T) {
	db := testdb.OpenMySQL8(t, &Log{})
	old := LOG_DB
	LOG_DB = db
	t.Cleanup(func() { LOG_DB = old })
	now := time.Now().Unix()
	require.NoError(t, db.Create(&[]Log{
		{UserId: 7319, Type: LogTypeConsume, CreatedAt: now - 120, Quota: 100},
		{UserId: 7319, Type: LogTypeConsume, CreatedAt: now, Quota: 25, PromptTokens: 8, CompletionTokens: 2},
	}).Error)
	for range 2 { // cold query, then cached quota with a fresh rate query
		stat, err := SumUsedQuota(LogTypeConsume, now-300, now+1, "", "", "", 0, "", 7319)
		require.NoError(t, err)
		require.Equal(t, Stat{Quota: 125, Rpm: 1, Tpm: 10}, stat)
	}
	stat, err := SumUsedQuota(LogTypeConsume, now-300, now+1, "", "", "", 0, "", 7320)
	require.NoError(t, err)
	require.Equal(t, Stat{}, stat)
}
