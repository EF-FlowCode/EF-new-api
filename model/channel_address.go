package model

import (
	"net"
	"net/url"
	"strings"
)

// eflow: 渠道按上游地址分类。base_url 的主机是 IP 字面量（含 IP:port）记为 ip，
// 其余记为 domain；留空的渠道走类型默认地址（官方域名），也算 domain。
const (
	ChannelAddressIP     = "ip"
	ChannelAddressDomain = "domain"
)

func ClassifyChannelAddress(baseURL string) string {
	raw := strings.TrimSpace(baseURL)
	if raw == "" {
		return ChannelAddressDomain
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ChannelAddressDomain
	}
	if net.ParseIP(u.Hostname()) != nil {
		return ChannelAddressIP
	}
	return ChannelAddressDomain
}

// NormalizeChannelAddressFilter 返回合法的分类值；空串、all 或未知值返回 ""（不过滤）。
func NormalizeChannelAddressFilter(filter string) string {
	switch strings.ToLower(strings.TrimSpace(filter)) {
	case ChannelAddressIP:
		return ChannelAddressIP
	case ChannelAddressDomain:
		return ChannelAddressDomain
	default:
		return ""
	}
}

// ChannelIDsByAddress 返回属于该分类的渠道 ID；filter 为空时返回 nil，表示不过滤。
// 分类要解析 URL，MySQL / PostgreSQL / SQLite 没有统一的 SQL 写法，所以在 Go 里算；渠道表很小。
func ChannelIDsByAddress(filter string) ([]int, error) {
	if filter == "" {
		return nil, nil
	}
	var channels []*Channel
	if err := DB.Select("id", "type", "base_url").Find(&channels).Error; err != nil {
		return nil, err
	}
	ids := make([]int, 0, len(channels))
	for _, channel := range channels {
		if ClassifyChannelAddress(channel.GetBaseURL()) == filter {
			ids = append(ids, channel.Id)
		}
	}
	return ids, nil
}
