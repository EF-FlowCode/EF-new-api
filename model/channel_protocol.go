package model

import (
	"errors"
	"fmt"
	"sort"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/dto"
	"eflow.local/new-api/setting/model_setting"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// eflow: protocol routing.

// UpdateChannelOtherSettings applies mutate to the stored settings JSON and
// writes only the settings column, so concurrent edits to other columns are
// not overwritten.
func UpdateChannelOtherSettings(id int, mutate func(*dto.ChannelOtherSettings)) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		var raw string
		if err := tx.Model(&Channel{}).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ?", id).Select("settings").Scan(&raw).Error; err != nil {
			return err
		}
		settings := dto.ChannelOtherSettings{}
		if raw != "" {
			if err := common.UnmarshalJsonStr(raw, &settings); err != nil {
				return fmt.Errorf("渠道 #%d 的 settings 不是有效 JSON: %w", id, err)
			}
		}
		mutate(&settings)
		data, err := common.Marshal(settings)
		if err != nil {
			return err
		}
		return tx.Model(&Channel{}).Where("id = ?", id).Update("settings", string(data)).Error
	})
}

// RequestPathSupport classifies how a channel can serve a request path.
type RequestPathSupport int

const (
	RequestPathUnsupported RequestPathSupport = iota
	// RequestPathNative means the request reaches the upstream in its own protocol.
	RequestPathNative
	// RequestPathFallback means the channel can serve the request only by
	// converting it (Chat fallback, or a legacy adaptor conversion).
	RequestPathFallback
)

// ErrChannelsExhausted reports that every candidate channel was already tried
// by this request, so a retry must keep the previous upstream error.
var ErrChannelsExhausted = errors.New("all candidate channels have been tried")

// ProtocolRoutingActive reports whether the channel's confirmed protocol set
// governs its relay behavior.
func ProtocolRoutingActive(channel *Channel) bool {
	if channel == nil || !model_setting.GetGlobalSettings().ProtocolRouting.Enabled {
		return false
	}
	if !constant.SupportsProtocolRouting(channel.Type) {
		return false
	}
	settings := channel.GetOtherSettings()
	return settings.DeclaresUpstreamProtocols()
}

// ChannelPassThroughBody reports whether the channel forwards request bodies
// unchanged, which rules out any protocol conversion.
func ChannelPassThroughBody(channel *Channel) bool {
	if model_setting.GetGlobalSettings().PassThroughRequestEnabled {
		return true
	}
	return channel != nil && channel.GetSetting().PassThroughBodyEnabled
}

// declaredRequestPathSupport applies the confirmed protocol set. A declared
// protocol is native; any other governed protocol falls back to Chat when the
// channel allows chat and does not pass bodies through, else to the type
// adaptor's own conversion.
func declaredRequestPathSupport(channel *Channel, protocol string, requestPath string, modelName string) RequestPathSupport {
	settings := channel.GetOtherSettings()
	if settings.AllowsUpstreamProtocol(protocol) {
		return RequestPathNative
	}
	if ChannelPassThroughBody(channel) {
		return RequestPathUnsupported
	}
	if settings.AllowsUpstreamProtocol(constant.UpstreamProtocolChat) {
		return RequestPathFallback
	}
	// Without Chat, the type's own adaptor conversion is the fallback when it
	// targets a declared protocol and handles this path (Anthropic: Chat to
	// Messages; it never served Responses).
	target := constant.LegacyConversionUpstreamProtocol(channel.Type)
	if target != "" && target != protocol && settings.AllowsUpstreamProtocol(target) &&
		channelTypeSupportsRequestPath(channel, requestPath, modelName) {
		return RequestPathFallback
	}
	return RequestPathUnsupported
}

// legacyRequestPathSupport orders an undeclared channel that already accepts
// the path: protocols its adaptor forwards unchanged are native, the rest are
// conversions. Types without a known table entry are never reordered.
func legacyRequestPathSupport(channel *Channel, protocol string) RequestPathSupport {
	if protocol == "" || !model_setting.GetGlobalSettings().ProtocolRouting.Enabled {
		return RequestPathNative
	}
	native, known := constant.LegacyNativeUpstreamProtocol(channel.Type, protocol)
	if !known || native {
		return RequestPathNative
	}
	return RequestPathFallback
}

// advancedCustomRequestPathSupport classifies an explicit Advanced Custom
// route: a converter-free route is native, a converting route is a fallback.
func advancedCustomRequestPathSupport(channel *Channel, requestPath string, modelName string) RequestPathSupport {
	config := channel.GetOtherSettings().AdvancedCustom
	if config == nil || !config.HasRelayRoutes() {
		return RequestPathNative
	}
	route, ok := config.MatchPathForModel(requestPath, modelName)
	if !ok {
		return RequestPathUnsupported
	}
	if route.SupportsPassThroughBody() {
		return RequestPathNative
	}
	return RequestPathFallback
}

type supportedChannel struct {
	channel *Channel
	support RequestPathSupport
}

// orderChannelTiers groups candidates into priority tiers: every native tier
// (priority descending) precedes every fallback tier, so a lower-priority
// native channel is tried before a higher-priority converting one.
func orderChannelTiers(candidates []supportedChannel) [][]*Channel {
	tiers := make([][]*Channel, 0)
	for _, support := range []RequestPathSupport{RequestPathNative, RequestPathFallback} {
		byPriority := make(map[int64][]*Channel)
		priorities := make([]int64, 0)
		for _, candidate := range candidates {
			if candidate.support != support {
				continue
			}
			priority := candidate.channel.GetPriority()
			if _, ok := byPriority[priority]; !ok {
				priorities = append(priorities, priority)
			}
			byPriority[priority] = append(byPriority[priority], candidate.channel)
		}
		sort.Slice(priorities, func(i, j int) bool { return priorities[i] > priorities[j] })
		for _, priority := range priorities {
			tiers = append(tiers, byPriority[priority])
		}
	}
	return tiers
}

// selectRetryTier returns the untried channels of the tier for retry. Each
// retry still moves one tier down (clamped to the last tier); channels the
// request already tried are skipped. When the target tier has nothing left,
// the next lower tiers and then the higher ones are searched. ok is false when
// every candidate was tried.
func selectRetryTier(tiers [][]*Channel, retry int, exclude map[int]struct{}) ([]*Channel, bool) {
	if len(tiers) == 0 {
		return nil, false
	}
	if retry < 0 {
		retry = 0
	}
	if retry >= len(tiers) {
		retry = len(tiers) - 1
	}
	untried := func(tier []*Channel) []*Channel {
		if len(exclude) == 0 {
			return tier
		}
		remaining := make([]*Channel, 0, len(tier))
		for _, channel := range tier {
			if _, tried := exclude[channel.Id]; !tried {
				remaining = append(remaining, channel)
			}
		}
		return remaining
	}
	order := make([]int, 0, len(tiers))
	for i := retry; i < len(tiers); i++ {
		order = append(order, i)
	}
	for i := retry - 1; i >= 0; i-- {
		order = append(order, i)
	}
	for _, index := range order {
		if remaining := untried(tiers[index]); len(remaining) > 0 {
			return remaining, true
		}
	}
	return nil, false
}

// orderingSupport is the support level used for ordering. With protocol
// routing switched off every accepted channel orders as native, which restores
// the legacy priority-only behavior.
func orderingSupport(channel *Channel, requestPath string, modelName string) RequestPathSupport {
	if requestPath == "" || !model_setting.GetGlobalSettings().ProtocolRouting.Enabled {
		return RequestPathNative
	}
	if ChannelRequestPathSupport(channel, requestPath, modelName) == RequestPathFallback {
		return RequestPathFallback
	}
	return RequestPathNative
}
