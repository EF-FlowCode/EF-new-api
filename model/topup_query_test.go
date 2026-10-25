package model

import (
	"testing"
	"time"

	"eflow.local/new-api/common"
	"github.com/stretchr/testify/require"
)

func TestGetUserTopUpsIncludesHistoricalOrders(t *testing.T) {
	db := useModelIntegrationTestDB(t, &User{}, &TopUp{})
	user := &User{Username: "topup-history-user", Password: "password"}
	require.NoError(t, db.Create(user).Error)

	oldTime := common.GetTimestamp() - 31*24*60*60
	require.NoError(t, db.Create(&TopUp{
		UserId:        user.Id,
		Amount:        10,
		Money:         10,
		TradeNo:       "HISTORICAL-TOPUP",
		PaymentMethod: "admin",
		CreateTime:    oldTime,
		CompleteTime:  oldTime,
		Status:        common.TopUpStatusSuccess,
	}).Error)
	require.NoError(t, db.Create(&TopUp{
		UserId:        user.Id,
		Amount:        20,
		Money:         20,
		TradeNo:       "RECENT-TOPUP",
		PaymentMethod: "admin",
		CreateTime:    time.Now().Unix(),
		CompleteTime:  time.Now().Unix(),
		Status:        common.TopUpStatusSuccess,
	}).Error)

	orders, total, err := GetUserTopUps(user.Id, TopUpFilter{}, &common.PageInfo{Page: 1, PageSize: 10})
	require.NoError(t, err)
	require.Equal(t, int64(2), total)
	require.Len(t, orders, 2)
	assertTopUpTradeNos(t, orders, "RECENT-TOPUP", "HISTORICAL-TOPUP")
}

func assertTopUpTradeNos(t *testing.T, orders []*TopUp, want ...string) {
	t.Helper()
	got := make([]string, 0, len(orders))
	for _, order := range orders {
		got = append(got, order.TradeNo)
	}
	require.Equal(t, want, got)
}
