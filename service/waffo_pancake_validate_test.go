package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateWaffoPancakePaidOrder(t *testing.T) {
	valid := func() *WaffoPancakeWebhookEvent {
		return &WaffoPancakeWebhookEvent{StoreID: "store-a", Data: WaffoPancakeWebhookData{Currency: "USD", Amount: "10.00"}}
	}
	require.NoError(t, ValidateWaffoPancakePaidOrder(valid(), "store-a", 10))

	taxed := valid()
	taxed.Data.Amount = "11.00"
	require.NoError(t, ValidateWaffoPancakePaidOrder(taxed, "store-a", 10), "tax on top of the price is accepted")

	otherStore := valid()
	otherStore.StoreID = "attacker-store"
	require.Error(t, ValidateWaffoPancakePaidOrder(otherStore, "store-a", 10))
	require.Error(t, ValidateWaffoPancakePaidOrder(valid(), "", 10), "an unbound store never credits")

	underpaid := valid()
	underpaid.Data.Amount = "0.01"
	require.Error(t, ValidateWaffoPancakePaidOrder(underpaid, "store-a", 10))

	otherCurrency := valid()
	otherCurrency.Data.Currency = "EUR"
	require.Error(t, ValidateWaffoPancakePaidOrder(otherCurrency, "store-a", 10))
}
