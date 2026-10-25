package model

import (
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"sync"
	"time"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
	"eflow.local/new-api/setting/ratio_setting"

	"gorm.io/gorm"
)

var group2model2channels map[string]map[string][]int // enabled channel
var channelsIDM map[int]*Channel                     // all channels include disabled
var channelSyncLock sync.RWMutex

func InitChannelCache() {
	if !common.MemoryCacheEnabled {
		// Pricing endpoint capabilities are derived from channel settings even
		// when channel selection uses the database directly.
		InvalidatePricingCache()
		return
	}
	if err := refreshChannelCache(); err != nil {
		common.SysError("failed to sync channels from database: " + err.Error())
		return
	}
	common.SysLog("channels synced from database")
}

func refreshChannelCache() error {
	newChannelId2channel := make(map[int]*Channel)
	var channels []*Channel
	var abilities []*Ability
	// Channels and abilities are written by separate commits; read both from
	// one snapshot so a refresh never pairs new channels with old abilities.
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Find(&channels).Error; err != nil {
			return fmt.Errorf("load channels: %w", err)
		}
		if err := tx.Find(&abilities).Error; err != nil {
			return fmt.Errorf("load abilities: %w", err)
		}
		return nil
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return err
	}
	for _, channel := range channels {
		newChannelId2channel[channel.Id] = channel
	}
	groups := make(map[string]bool)
	for _, ability := range abilities {
		groups[ability.Group] = true
	}
	newGroup2model2channels := make(map[string]map[string][]int)
	for group := range groups {
		newGroup2model2channels[group] = make(map[string][]int)
	}
	for _, channel := range channels {
		if channel.Status != common.ChannelStatusEnabled {
			continue // skip disabled channels
		}
		groups := strings.SplitSeq(channel.Group, ",")
		for group := range groups {
			if newGroup2model2channels[group] == nil {
				// A channel group without abilities (edit in flight or a failed
				// ability rebuild) must not panic the whole process.
				newGroup2model2channels[group] = make(map[string][]int)
			}
			models := strings.SplitSeq(channel.Models, ",")
			for model := range models {
				if _, ok := newGroup2model2channels[group][model]; !ok {
					newGroup2model2channels[group][model] = make([]int, 0)
				}
				newGroup2model2channels[group][model] = append(newGroup2model2channels[group][model], channel.Id)
			}
		}
	}

	// sort by priority
	for group, model2channels := range newGroup2model2channels {
		for model, channels := range model2channels {
			sort.Slice(channels, func(i, j int) bool {
				return newChannelId2channel[channels[i]].GetPriority() > newChannelId2channel[channels[j]].GetPriority()
			})
			newGroup2model2channels[group][model] = channels
		}
	}

	channelSyncLock.Lock()
	group2model2channels = newGroup2model2channels
	//channelsIDM = newChannelId2channel
	for i, channel := range newChannelId2channel {
		if channel.ChannelInfo.IsMultiKey {
			channel.Keys = channel.GetKeys()
			if channel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
				if oldChannel, ok := channelsIDM[i]; ok {
					// 存在旧的渠道，如果是多key且轮询，保留轮询索引信息
					if oldChannel.ChannelInfo.IsMultiKey && oldChannel.ChannelInfo.MultiKeyMode == constant.MultiKeyModePolling {
						channel.ChannelInfo.MultiKeyPollingIndex = oldChannel.ChannelInfo.MultiKeyPollingIndex
					}
				}
			}
		}
	}
	channelsIDM = newChannelId2channel
	channelSyncLock.Unlock()
	// Channel settings include Advanced Custom route capabilities. Invalidate
	// the derived pricing/endpoint cache only after releasing the channel lock;
	// pricing refresh may take a read lock on this cache.
	InvalidatePricingCache()
	return nil
}

func SyncChannelCache(frequency int) {
	for {
		time.Sleep(time.Duration(frequency) * time.Second)
		common.SysLog("syncing channels from database")
		syncChannelCacheOnce()
	}
}

// syncChannelCacheOnce keeps the background loop alive: a panic here used to
// terminate the process because the loop runs in a bare goroutine.
func syncChannelCacheOnce() {
	defer func() {
		if r := recover(); r != nil {
			common.SysError(fmt.Sprintf("channel cache sync panic: %v", r))
		}
	}()
	InitChannelCache()
}

// GetRandomSatisfiedChannel keeps the legacy three-argument call shape while
// accepting the optional request path used by the upstream API.
func GetRandomSatisfiedChannel(group string, model string, retry int, requestPath ...string) (*Channel, error) {
	path := ""
	if len(requestPath) > 0 {
		path = requestPath[0]
	}
	return GetRandomSatisfiedChannelForRequest(group, model, retry, path)
}

// GetRandomSatisfiedChannelForRequest is the path-aware channel-cache lookup.
func GetRandomSatisfiedChannelForRequest(group string, model string, retry int, requestPath string, filters ...func(*Channel) bool) (*Channel, error) {
	// if memory cache is disabled, get channel directly from database
	if !common.MemoryCacheEnabled {
		return GetChannelForRequest(group, model, retry, requestPath, filters...)
	}

	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	// First, try to find channels with the exact model name.
	channels := group2model2channels[group][model]
	channels = filterChannelsByRequestPath(channels, requestPath, model, filters...)

	// If no channels found, try to find channels with the normalized model name.
	if len(channels) == 0 {
		normalizedModel := ratio_setting.FormatMatchingModelName(model)
		channels = group2model2channels[group][normalizedModel]
		channels = filterChannelsByRequestPath(channels, requestPath, model, filters...)
	}

	if len(channels) == 0 {
		return nil, nil
	}

	if len(channels) == 1 {
		if channel, ok := channelsIDM[channels[0]]; ok {
			return channel, nil
		}
		return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", channels[0])
	}

	uniquePriorities := make(map[int]bool)
	for _, channelId := range channels {
		if channel, ok := channelsIDM[channelId]; ok {
			uniquePriorities[int(channel.GetPriority())] = true
		} else {
			return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", channelId)
		}
	}
	var sortedUniquePriorities []int
	for priority := range uniquePriorities {
		sortedUniquePriorities = append(sortedUniquePriorities, priority)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(sortedUniquePriorities)))

	if retry < 0 {
		retry = 0
	}
	if retry >= len(uniquePriorities) {
		retry = len(uniquePriorities) - 1
	}
	targetPriority := int64(sortedUniquePriorities[retry])

	// get the priority for the given retry number
	var sumWeight = 0
	var targetChannels []*Channel
	for _, channelId := range channels {
		if channel, ok := channelsIDM[channelId]; ok {
			if channel.GetPriority() == targetPriority {
				sumWeight += channel.GetWeight()
				targetChannels = append(targetChannels, channel)
			}
		} else {
			return nil, fmt.Errorf("数据库一致性错误，渠道# %d 不存在，请联系管理员修复", channelId)
		}
	}

	if len(targetChannels) == 0 {
		return nil, errors.New(fmt.Sprintf("no channel found, group: %s, model: %s, priority: %d", group, model, targetPriority))
	}

	// smoothing factor and adjustment
	smoothingFactor := 1
	smoothingAdjustment := 0

	if sumWeight == 0 {
		// when all channels have weight 0, set sumWeight to the number of channels and set smoothing adjustment to 100
		// each channel's effective weight = 100
		sumWeight = len(targetChannels) * 100
		smoothingAdjustment = 100
	} else if sumWeight/len(targetChannels) < 10 {
		// when the average weight is less than 10, set smoothing factor to 100
		smoothingFactor = 100
	}

	// Calculate the total weight of all channels up to endIdx
	totalWeight := sumWeight * smoothingFactor

	// Generate a random value in the range [0, totalWeight)
	randomWeight := rand.Intn(totalWeight)

	// Find a channel based on its weight
	for _, channel := range targetChannels {
		randomWeight -= channel.GetWeight()*smoothingFactor + smoothingAdjustment
		if randomWeight < 0 {
			return channel, nil
		}
	}
	// return null if no channel is not found
	return nil, errors.New("channel not found")
}

func filterChannelsByRequestPath(channelIDs []int, requestPath string, modelName string, filters ...func(*Channel) bool) []int {
	if (requestPath == "" && len(filters) == 0) || len(channelIDs) == 0 {
		return channelIDs
	}
	filtered := make([]int, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		channel, ok := channelsIDM[channelID]
		if !ok {
			if len(filters) > 0 {
				continue
			}
			// Preserve stale IDs so the caller retains the existing database
			// consistency error instead of silently reporting no channel.
			filtered = append(filtered, channelID)
			continue
		}
		if ChannelSupportsRequestPath(channel, requestPath, modelName) && channelPassesFilters(channel, filters) {
			filtered = append(filtered, channelID)
		}
	}
	return filtered
}

func CacheGetChannel(id int) (*Channel, error) {
	if !common.MemoryCacheEnabled {
		return GetChannelById(id, true)
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	c, ok := channelsIDM[id]
	if !ok {
		return nil, fmt.Errorf("渠道# %d，已不存在", id)
	}
	return c, nil
}

// CacheSelectEnabledChannels returns, ordered by id, the enabled cached
// channels accepted by keep, projected to the routing columns task plugins read
// (id, type, status, models, model_mapping). ok is false when the memory cache
// is off or has not loaded, so callers can fall back to the database.
func CacheSelectEnabledChannels(keep func(*Channel) bool) (channels []Channel, ok bool) {
	if !common.MemoryCacheEnabled {
		return nil, false
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()
	if channelsIDM == nil {
		return nil, false
	}
	for _, ch := range channelsIDM {
		if ch.Status == common.ChannelStatusEnabled && keep(ch) {
			channels = append(channels, Channel{Id: ch.Id, Type: ch.Type, Status: ch.Status, Models: ch.Models, ModelMapping: ch.ModelMapping})
		}
	}
	sort.Slice(channels, func(i, j int) bool { return channels[i].Id < channels[j].Id })
	return channels, true
}

func CacheGetChannelInfo(id int) (*ChannelInfo, error) {
	if !common.MemoryCacheEnabled {
		channel, err := GetChannelById(id, true)
		if err != nil {
			return nil, err
		}
		return &channel.ChannelInfo, nil
	}
	channelSyncLock.RLock()
	defer channelSyncLock.RUnlock()

	c, ok := channelsIDM[id]
	if !ok {
		return nil, fmt.Errorf("渠道# %d，已不存在", id)
	}
	return &c.ChannelInfo, nil
}

func CacheUpdateChannelStatus(id int, status int) {
	if !common.MemoryCacheEnabled {
		return
	}
	channelSyncLock.Lock()
	defer channelSyncLock.Unlock()
	if channel, ok := channelsIDM[id]; ok {
		channel.Status = status
	}
	if status != common.ChannelStatusEnabled {
		// delete the channel from group2model2channels
		for group, model2channels := range group2model2channels {
			for model, channels := range model2channels {
				for i, channelId := range channels {
					if channelId == id {
						// remove the channel from the slice
						group2model2channels[group][model] = append(channels[:i], channels[i+1:]...)
						break
					}
				}
			}
		}
	}
}
