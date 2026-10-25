package controller

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func readExportCSV(t *testing.T, zr *zip.Reader, name string) [][]string {
	t.Helper()
	f, err := zr.Open(name)
	require.NoError(t, err)
	defer f.Close()
	raw, err := io.ReadAll(f)
	require.NoError(t, err)
	rows, err := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(raw, []byte{0xEF, 0xBB, 0xBF}))).ReadAll()
	require.NoError(t, err)
	return rows
}

func TestUserLogsExportZipShowsRefundAndNetQuota(t *testing.T) {
	unit := int(common.GetQuotaPerUnit())
	logs := []*model.Log{
		{Type: model.LogTypeConsume, ModelName: "gpt", Quota: 3 * unit, UsageRefund: &model.UsageRefundLogMark{Quota: 2 * unit}},
		{Type: model.LogTypeConsume, ModelName: "gpt", Quota: unit},
	}
	zipBytes, err := buildUserLogsExportZip(logs, false)
	require.NoError(t, err)
	zr, err := zip.NewReader(bytes.NewReader(zipBytes), int64(len(zipBytes)))
	require.NoError(t, err)

	detail := readExportCSV(t, zr, "使用明细.csv")
	require.Equal(t, "已返还额度(元)", detail[0][10])
	require.Equal(t, "2.000000", detail[1][10])
	require.Equal(t, "0.000000", detail[2][10])

	summary := readExportCSV(t, zr, "消费汇总.csv")
	require.Equal(t, []string{"已返还(元)合计", "净消费(元)合计"}, summary[0][5:])
	total := summary[len(summary)-1]
	require.Equal(t, "总计", total[0])
	require.Equal(t, []string{"4.000000", "2.000000", "2.000000"}, total[4:])
}

func TestAdminLogExportRequiresUserID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, query := range []string{"", "?user_id=0", "?user_id=-3", "?user_id=abc"} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodGet, "/api/log/export"+query, nil)
		c.Set("id", 1)

		_, ok := adminLogExportFilterFromQuery(c)
		require.False(t, ok, query)
		require.Contains(t, w.Body.String(), "用户 ID", query)
	}
}
