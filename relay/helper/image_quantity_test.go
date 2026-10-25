package helper

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/types"
	"github.com/stretchr/testify/require"
)

func TestImageQuantityValidationBeforePricing(t *testing.T) {
	for _, multipartRequest := range []bool{false, true} {
		for _, tc := range []struct {
			n       string
			want    uint
			invalid bool
		}{
			{"", 1, false}, {"0", 1, false}, {"1", 1, false}, {"128", 128, false},
			{"129", 0, true}, {"-1", 0, true}, {"1.5", 0, true}, {"nope", 0, true}, {"18446744073709551616", 0, true},
		} {
			t.Run(fmt.Sprintf("multipart=%v/n=%s", multipartRequest, tc.n), func(t *testing.T) {
				body := `{"model":"test-image","prompt":"test"`
				if tc.n != "" {
					body += `,"n":` + tc.n
				}
				body += "}"
				c := newValidationContext(t, "/v1/images/generations", strings.NewReader(body))
				if multipartRequest {
					var data bytes.Buffer
					w := multipart.NewWriter(&data)
					require.NoError(t, w.WriteField("model", "test-image"))
					if tc.n != "" {
						require.NoError(t, w.WriteField("n", tc.n))
					}
					require.NoError(t, w.Close())
					c = newValidationContext(t, "/v1/images/edits", &data)
					c.Request.Header.Set("Content-Type", w.FormDataContentType())
				}
				request, err := GetAndValidateRequest(c, types.RelayFormatOpenAIImage)
				if tc.invalid {
					require.Error(t, err)
					require.True(t, IsRequestValidationError(err))
					return
				}
				require.NoError(t, err)
				require.Equal(t, float64(tc.want), request.GetTokenCountMeta().BillingRatios["n"])
			})
		}
	}
	for _, tc := range []struct {
		parameters string
		want       float64
		invalid    bool
	}{
		{`{"n":3}`, 3, false}, {`{}`, 1, false}, {`{"n":0}`, 0, true},
		{`{"n":-1}`, 0, true}, {`{"n":129}`, 0, true}, {`{"n":1.5}`, 0, true}, {`"wrong"`, 0, true},
	} {
		c := newValidationContext(t, "/v1/images/generations", strings.NewReader(`{"model":"ali-image","parameters":`+tc.parameters+`}`))
		common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeAli)
		request, err := GetAndValidateRequest(c, types.RelayFormatOpenAIImage)
		if tc.invalid {
			require.Error(t, err)
			require.True(t, IsRequestValidationError(err))
			continue
		}
		require.NoError(t, err)
		require.Equal(t, tc.want, request.GetTokenCountMeta().BillingRatios["n"])
	}
}

// The reservation must not depend on which channel type was selected first:
// a retry can move between Ali (parameters.n) and other adapters (top-level n).
func TestImageQuantityReservationIsChannelIndependent(t *testing.T) {
	for _, tc := range []struct {
		body string
		want float64
	}{
		{`{"model":"m","prompt":"p","n":1,"parameters":{"n":4}}`, 4},
		{`{"model":"m","prompt":"p","n":5,"parameters":{"n":2}}`, 5},
		{`{"model":"m","prompt":"p","n":3,"parameters":{}}`, 3},
		{`{"model":"m","prompt":"p","parameters":{"n":2}}`, 2},
	} {
		for _, channelType := range []int{constant.ChannelTypeOpenAI, constant.ChannelTypeAli} {
			c := newValidationContext(t, "/v1/images/generations", strings.NewReader(tc.body))
			common.SetContextKey(c, constant.ContextKeyChannelType, channelType)
			request, err := GetAndValidateRequest(c, types.RelayFormatOpenAIImage)
			require.NoError(t, err, tc.body)
			require.Equal(t, tc.want, request.GetTokenCountMeta().BillingRatios["n"], "%s channel=%d", tc.body, channelType)
		}
	}
	// Ali-specific parameters stay validated for Ali but do not break others.
	body := `{"model":"m","prompt":"p","n":2,"parameters":"wrong"}`
	c := newValidationContext(t, "/v1/images/generations", strings.NewReader(body))
	common.SetContextKey(c, constant.ContextKeyChannelType, constant.ChannelTypeOpenAI)
	request, err := GetAndValidateRequest(c, types.RelayFormatOpenAIImage)
	require.NoError(t, err)
	require.Equal(t, float64(2), request.GetTokenCountMeta().BillingRatios["n"])
}
