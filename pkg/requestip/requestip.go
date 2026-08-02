package requestip

import (
	"net"
	"net/http"
	"slices"
	"strings"

	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/gin-gonic/gin"
)

var diagnosisHeaderOrder = []string{
	"X-Real-IP",
	"CF-Connecting-IP",
	"True-Client-IP",
	"X-Forwarded-For",
	"Forwarded",
	"X-Client-IP",
	"X-Original-Forwarded-For",
	"X-Cluster-Client-IP",
	"Fly-Client-IP",
	"Fastly-Client-IP",
}

var recommendationHeaderOrder = []string{
	"X-Real-IP",
	"CF-Connecting-IP",
	"True-Client-IP",
	"X-Forwarded-For",
	"Forwarded",
	"X-Client-IP",
	"X-Original-Forwarded-For",
	"X-Cluster-Client-IP",
	"Fly-Client-IP",
	"Fastly-Client-IP",
}

type DiagnosisItem struct {
	Name           string `json:"name"`
	Source         string `json:"source"`
	RawValue       string `json:"raw_value"`
	ParsedIP       string `json:"parsed_ip"`
	Present        bool   `json:"present"`
	Valid          bool   `json:"valid"`
	Classification string `json:"classification"`
	IsCurrent      bool   `json:"is_current"`
}

type Diagnosis struct {
	CurrentMode           string          `json:"current_mode"`
	CurrentHeader         string          `json:"current_header"`
	EffectiveClientIP     string          `json:"effective_client_ip"`
	EffectiveSource       string          `json:"effective_source"`
	RecommendedMode       string          `json:"recommended_mode"`
	RecommendedHeader     string          `json:"recommended_header"`
	RecommendationMessage string          `json:"recommendation_message"`
	Items                 []DiagnosisItem `json:"items"`
}

// GetClientIP returns the client IP used by security-sensitive modules.
// Behavior:
//  1. If admin explicitly enabled TrustedIPHeader, use that header first (strict mode,
//     only the configured header is trusted to prevent spoofing via other headers).
//  2. Otherwise use the TCP peer from RemoteAddr. Security-sensitive callers
//     must not trust spoofable forwarding headers until an admin opts in.
func GetClientIP(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	cfg := operation_setting.GetRiskControlSetting()
	if cfg != nil && cfg.TrustedIPHeaderEnabled {
		headerName := strings.TrimSpace(cfg.TrustedIPHeader)
		if headerName != "" {
			if ip := extractHeaderIP(headerName, c.GetHeader(headerName)); ip != "" {
				return ip
			}
		}
		// Strict mode enabled but configured header missing/invalid — fall back to
		// RemoteAddr rather than other proxy headers to keep the spoofing protection.
		return parseRemoteAddrIP(c.Request.RemoteAddr)
	}
	return parseRemoteAddrIP(c.Request.RemoteAddr)
}

// DiagnoseRequest inspects the current request and recommends how to derive the
// client IP for security-sensitive modules. It is intended for admin-facing
// diagnostics so deployment-specific proxy behavior can be verified safely.
func DiagnoseRequest(c *gin.Context) Diagnosis {
	diag := Diagnosis{
		CurrentMode:     "remote_addr",
		RecommendedMode: "remote_addr",
	}
	if c == nil || c.Request == nil {
		diag.RecommendationMessage = "当前请求上下文不可用，建议保持关闭信任上游 IP 头。"
		return diag
	}

	cfg := operation_setting.GetRiskControlSetting()
	if cfg != nil {
		diag.CurrentHeader = strings.TrimSpace(cfg.TrustedIPHeader)
		if cfg.TrustedIPHeaderEnabled {
			diag.CurrentMode = "trusted_header"
		}
	}

	items := make([]DiagnosisItem, 0, len(diagnosisHeaderOrder)+1)
	itemIndex := make(map[string]int, len(diagnosisHeaderOrder)+1)

	appendItem := func(name, source, rawValue string) {
		key := strings.ToLower(name)
		if _, exists := itemIndex[key]; exists {
			return
		}
		itemIndex[key] = len(items)
		items = append(items, buildDiagnosisItem(name, source, rawValue))
	}

	appendItem("RemoteAddr", "remote_addr", c.Request.RemoteAddr)
	if diag.CurrentHeader != "" {
		appendItem(diag.CurrentHeader, "header", c.GetHeader(diag.CurrentHeader))
	}
	for _, headerName := range diagnosisHeaderOrder {
		appendItem(headerName, "header", c.GetHeader(headerName))
	}
	for headerName, values := range c.Request.Header {
		if !isLikelyIPHeader(headerName) {
			continue
		}
		appendItem(headerName, "header", strings.Join(values, ", "))
	}

	if diag.CurrentMode == "trusted_header" && diag.CurrentHeader != "" {
		if idx, ok := itemIndex[strings.ToLower(diag.CurrentHeader)]; ok {
			current := items[idx]
			if current.Valid {
				current.IsCurrent = true
				items[idx] = current
				diag.EffectiveClientIP = current.ParsedIP
				diag.EffectiveSource = "header"
			}
		}
	}
	if diag.EffectiveClientIP == "" {
		if idx, ok := itemIndex["remoteaddr"]; ok {
			current := items[idx]
			current.IsCurrent = true
			items[idx] = current
			diag.EffectiveClientIP = current.ParsedIP
		}
		diag.EffectiveSource = "remote_addr"
	}

	for _, headerName := range recommendationHeaderOrder {
		idx, ok := itemIndex[strings.ToLower(headerName)]
		if !ok {
			continue
		}
		item := items[idx]
		if item.Classification == "public" {
			diag.RecommendedMode = "trusted_header"
			diag.RecommendedHeader = item.Name
			diag.RecommendationMessage = "检测到可信候选请求头 " + item.Name + "，其值可解析为公网 IP，建议开启“信任上游 IP 头”并使用该头。"
			diag.Items = items
			return diag
		}
	}

	if idx, ok := itemIndex["remoteaddr"]; ok && items[idx].Classification == "public" {
		diag.RecommendationMessage = "RemoteAddr 已直接表现为公网 IP，建议关闭“信任上游 IP 头”，直接使用 TCP RemoteAddr。"
	} else {
		diag.RecommendationMessage = "未检测到可靠的公网 IP 请求头，建议保持关闭“信任上游 IP 头”，并检查上游代理是否已正确覆盖写入真实客户端 IP。"
	}
	diag.Items = items
	return diag
}

func parseHeaderIP(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, segment := range strings.Split(value, ",") {
		if ip := normalizeIPToken(segment); ip != "" {
			return ip
		}
	}
	return ""
}

func extractHeaderIP(headerName string, value string) string {
	if strings.EqualFold(headerName, "Forwarded") {
		return parseForwardedHeaderIP(value)
	}
	return parseHeaderIP(value)
}

func parseRemoteAddrIP(remoteAddr string) string {
	remoteAddr = strings.TrimSpace(remoteAddr)
	if remoteAddr == "" {
		return ""
	}
	if ip := normalizeIPToken(remoteAddr); ip != "" {
		return ip
	}
	return remoteAddr
}

func parseForwardedHeaderIP(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	for _, entry := range strings.Split(value, ",") {
		for _, part := range strings.Split(entry, ";") {
			part = strings.TrimSpace(part)
			if !strings.HasPrefix(strings.ToLower(part), "for=") {
				continue
			}
			if ip := normalizeIPToken(strings.TrimSpace(part[4:])); ip != "" {
				return ip
			}
		}
	}
	return ""
}

func buildDiagnosisItem(name string, source string, rawValue string) DiagnosisItem {
	item := DiagnosisItem{
		Name:           name,
		Source:         source,
		RawValue:       strings.TrimSpace(rawValue),
		Classification: "missing",
	}
	if item.RawValue == "" {
		return item
	}
	item.Present = true
	if source == "remote_addr" {
		item.ParsedIP = parseRemoteAddrIP(item.RawValue)
	} else {
		item.ParsedIP = extractHeaderIP(name, item.RawValue)
	}
	if item.ParsedIP == "" {
		item.Classification = "invalid"
		return item
	}
	item.Valid = true
	ip := net.ParseIP(item.ParsedIP)
	if ip == nil {
		item.Classification = "invalid"
		return item
	}
	switch {
	case isPrivateOrSpecialIP(ip):
		item.Classification = "private"
	default:
		item.Classification = "public"
	}
	return item
}

func isLikelyIPHeader(name string) bool {
	normalized := strings.ToLower(http.CanonicalHeaderKey(name))
	if slices.ContainsFunc(diagnosisHeaderOrder, func(candidate string) bool {
		return strings.EqualFold(candidate, normalized)
	}) {
		return true
	}
	return strings.Contains(normalized, "forwarded") || strings.Contains(normalized, "ip")
}

func isPrivateOrSpecialIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsPrivate() ||
		ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() ||
		ip.IsUnspecified() ||
		ip.IsInterfaceLocalMulticast()
}

func normalizeIPToken(value string) string {
	value = strings.TrimSpace(strings.Trim(value, `"'`))
	if value == "" {
		return ""
	}
	if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	value = strings.Trim(strings.TrimSpace(value), "[]")
	ip := net.ParseIP(value)
	if ip == nil {
		return ""
	}
	return ip.String()
}
