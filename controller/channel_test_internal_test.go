package controller

import (
	"context"
	"math"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/model"
	"eflow.local/new-api/pkg/billingexpr"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/setting/operation_setting"
	"eflow.local/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSettleTestQuotaSaturation(t *testing.T) {
	for _, price := range []types.PriceData{
		{UsePrice: true, ModelPrice: math.MaxFloat64},
		{ModelRatio: 1, CompletionRatio: math.MaxFloat64},
		{ModelRatio: math.MaxFloat64, CompletionRatio: 1},
	} {
		quota, _ := settleTestQuota(nil, price, &dto.Usage{PromptTokens: 10, CompletionTokens: 10})
		require.Equal(t, common.MaxQuota, quota)
	}
	quota, _ := settleTestQuota(nil, types.PriceData{ModelRatio: 2, CompletionRatio: 1.5}, &dto.Usage{PromptTokens: 10, CompletionTokens: 3})
	require.Equal(t, 30, quota)
}

func TestSelectChannelsForAutomaticTest(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}

	scheduled := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModeScheduledAll)
	require.Len(t, scheduled, 2)
	require.Equal(t, 1, scheduled[0].Id)
	require.Equal(t, 2, scheduled[1].Id)

	passive := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModePassiveRecovery)
	require.Len(t, passive, 1)
	require.Equal(t, 2, passive[0].Id)
}

func TestSelectChannelsForAutomaticTestAutoBanOnly(t *testing.T) {
	autoBanEnabled := 1
	autoBanDisabled := 0
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled, AutoBan: &autoBanEnabled},
		{Id: 2, Status: common.ChannelStatusEnabled, AutoBan: &autoBanDisabled},
		{Id: 3, Status: common.ChannelStatusAutoDisabled, AutoBan: &autoBanEnabled},
		{Id: 4, Status: common.ChannelStatusManuallyDisabled, AutoBan: &autoBanEnabled},
		{Id: 5, Status: common.ChannelStatusEnabled},
	}
	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModeAutoBanOnly)
	require.Len(t, selected, 2)
	require.Equal(t, 1, selected[0].Id)
	require.Equal(t, 3, selected[1].Id)
}

func TestRunChannelTestWorkersHonorsConcurrencyAndProgress(t *testing.T) {
	originalInterval := common.RequestInterval
	common.RequestInterval = 0
	t.Cleanup(func() { common.RequestInterval = originalInterval })

	channels := []*model.Channel{{Id: 1}, {Id: 2}, {Id: 3}, {Id: 4}}
	started := make(chan struct{}, len(channels))
	release := make(chan struct{})
	var active atomic.Int32
	var maxActive atomic.Int32
	progress := make([]int, 0, len(channels)+1)
	resultCh := make(chan channelTestSummary, 1)

	go func() {
		resultCh <- runChannelTestWorkers(context.Background(), channels, 2,
			func(_ context.Context, _ *model.Channel) channelTestSummary {
				current := active.Add(1)
				defer active.Add(-1)
				for {
					seen := maxActive.Load()
					if current <= seen || maxActive.CompareAndSwap(seen, current) {
						break
					}
				}
				started <- struct{}{}
				<-release
				return channelTestSummary{Tested: 1, Succeeded: 1}
			},
			func(processed, _ int) { progress = append(progress, processed) },
		)
	}()

	<-started
	<-started
	select {
	case <-started:
		t.Fatal("worker count exceeded configured concurrency")
	default:
	}
	close(release)
	summary := <-resultCh
	require.Equal(t, int32(2), maxActive.Load())
	require.Equal(t, channelTestSummary{Tested: 4, Succeeded: 4}, summary)
	require.Equal(t, []int{0, 1, 2, 3, 4}, progress)
}

func TestRunChannelTestWorkersStopsAfterCancellation(t *testing.T) {
	originalInterval := common.RequestInterval
	common.RequestInterval = 0
	t.Cleanup(func() { common.RequestInterval = originalInterval })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	channels := []*model.Channel{{Id: 1}, {Id: 2}, {Id: 3}}
	started := make(chan struct{}, len(channels))
	resultCh := make(chan channelTestSummary, 1)
	go func() {
		resultCh <- runChannelTestWorkers(ctx, channels, 2,
			func(ctx context.Context, _ *model.Channel) channelTestSummary {
				started <- struct{}{}
				<-ctx.Done()
				return channelTestSummary{Tested: 1, Succeeded: 1}
			}, nil)
	}()
	<-started
	<-started
	cancel()
	summary := <-resultCh
	select {
	case <-started:
		t.Fatal("a new test started after cancellation")
	default:
	}
	require.Equal(t, channelTestSummary{Tested: 2, Succeeded: 2}, summary)
}

func TestSettleTestQuotaUsesTieredBilling(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:   "tiered_expr",
			ExprString:    `param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`,
			ExprHash:      billingexpr.ExprHashString(`param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`),
			GroupRatio:    1,
			EstimatedTier: "stream",
			QuotaPerUnit:  common.GetQuotaPerUnit(),
			ExprVersion:   1,
		},
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{"stream":true}`),
		},
	}

	quota, result := settleTestQuota(info, types.PriceData{
		ModelRatio:      1,
		CompletionRatio: 2,
	}, &dto.Usage{
		PromptTokens: 1000,
	})

	require.Equal(t, 1500, quota)
	require.NotNil(t, result)
	require.Equal(t, "stream", result.MatchedTier)
}

func TestBuildTestLogOtherInjectsTieredInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr",
			ExprString:  `tier("base", p * 2)`,
		},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	priceData := types.PriceData{
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
	usage := &dto.Usage{
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 12,
		},
	}

	other := buildTestLogOther(ctx, info, priceData, usage, &billingexpr.TieredResult{
		MatchedTier: "base",
	})

	require.Equal(t, "tiered_expr", other["billing_mode"])
	require.Equal(t, "base", other["matched_tier"])
	require.NotEmpty(t, other["expr_b64"])
}

func TestResolveChannelTestUserIDUsesRequestUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("id", 2)

	userID, err := resolveChannelTestUserID(ctx)

	require.NoError(t, err)
	require.Equal(t, 2, userID)
}

func TestValidateChannelRequiresAdvancedCustomModelListRouteForModelSync(t *testing.T) {
	settings, err := common.Marshal(dto.ChannelOtherSettings{
		UpstreamModelUpdateCheckEnabled: true,
		AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
			IncomingPath: "/v1/chat/completions",
			UpstreamPath: "/chat",
		}}},
	})
	require.NoError(t, err)
	channel := &model.Channel{
		Type:          constant.ChannelTypeCustom,
		Name:          "advanced-custom",
		Key:           "secret",
		Models:        "gpt-test",
		Group:         "default",
		OtherSettings: string(settings),
	}

	err = validateChannel(channel, true)
	require.ErrorContains(t, err, dto.AdvancedCustomModelListPath)

	settings, err = common.Marshal(dto.ChannelOtherSettings{
		UpstreamModelUpdateCheckEnabled: true,
		AdvancedCustom: &dto.AdvancedCustomConfig{Routes: []dto.AdvancedCustomRoute{{
			IncomingPath: dto.AdvancedCustomModelListPath,
			UpstreamPath: "/models",
		}, {
			IncomingPath: "/v1/chat/completions",
			UpstreamPath: "/chat",
		}}},
	})
	require.NoError(t, err)
	channel.OtherSettings = string(settings)
	require.NoError(t, validateChannel(channel, true))
}

func TestValidateChannelAcceptsLongAggregateGroupList(t *testing.T) {
	groups := "1_vip_claude_max_nopic,work_claude_max,1_vip_claude_API2,test_cc_max"
	require.Greater(t, len(groups), 64)
	require.NoError(t, validateChannel(&model.Channel{Group: groups}, false))
}

func TestValidateChannelRejectsOverlongIndividualGroupName(t *testing.T) {
	err := validateChannel(&model.Channel{Group: strings.Repeat("a", 65)}, false)
	require.ErrorContains(t, err, "分组名称不能超过 64 个字符")
}

func TestValidateChannelAppliesDefaultGroupOnAdd(t *testing.T) {
	channel := &model.Channel{Key: "test-key"}
	require.NoError(t, validateChannel(channel, true))
	require.Equal(t, "default", channel.Group)
}
