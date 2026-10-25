package controller

import (
	"errors"
	"net/url"
	"strings"

	"eflow.local/new-api/model"

	"gorm.io/gorm"
)

// eflow: 标签批量改 Base URL。空串视为不修改（与弹窗"留空保持不变"一致）；
// 非空必须是带 http/https 和主机名的完整地址，避免一次写坏整个供应商的渠道。
func normalizeTagBaseURL(raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	trimmed := strings.TrimSpace(*raw)
	if trimmed == "" {
		return nil, nil
	}
	u, err := url.Parse(trimmed)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, errors.New("Base URL 必须是以 http:// 或 https:// 开头的完整地址")
	}
	return &trimmed, nil
}

// applyChannelAddressIDs 把地址分类筛出的渠道 ID 套到查询上；ids 为 nil 表示不按地址过滤。
func applyChannelAddressIDs(query *gorm.DB, ids []int) *gorm.DB {
	if ids == nil {
		return query
	}
	if len(ids) == 0 {
		return query.Where("1 = 0")
	}
	return query.Where("id IN ?", ids)
}

// countChannelAddresses 统计各地址分类的渠道数，供前端筛选项显示数量。
func countChannelAddresses(channels []*model.Channel) map[string]int64 {
	counts := map[string]int64{model.ChannelAddressIP: 0, model.ChannelAddressDomain: 0}
	for _, channel := range channels {
		counts[model.ClassifyChannelAddress(channel.GetBaseURL())]++
	}
	return counts
}

func filterChannelsByAddress(channels []*model.Channel, filter string) []*model.Channel {
	if filter == "" {
		return channels
	}
	filtered := make([]*model.Channel, 0, len(channels))
	for _, channel := range channels {
		if model.ClassifyChannelAddress(channel.GetBaseURL()) == filter {
			filtered = append(filtered, channel)
		}
	}
	return filtered
}
