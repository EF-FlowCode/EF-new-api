package setting

import (
	"strings"
)

// 工单附件功能相关的运行时配置变量。
// 值由 model.InitOptionMap 初始化，并通过 updateOptionMap 热更新。
// 所有对白名单的判断都应走 Is*Allowed 帮助函数，保证大小写与通配符逻辑一致。
var (
	TicketAttachmentEnabled  = true
	TicketAttachmentMaxSize  int64 = 50 * 1024 * 1024 // 单文件 50 MB
	TicketAttachmentMaxCount       = 5                // 单条消息附件数量上限

	// 白名单以小写、逗号分隔存储；svg 被产品需求明确禁用，不应出现在默认值里。
	TicketAttachmentAllowedExts  = "jpg,jpeg,png,gif,webp,bmp,json,xml,txt,log,md,csv,pdf"
	TicketAttachmentAllowedMimes = "image/*,application/json,application/xml,text/*,application/pdf"

	// 可选的存储后端：local / oss / s3 / cos。当前仅 local 实际可用，其它作为占位。
	TicketAttachmentStorage   = "local"
	TicketAttachmentLocalPath = "data/ticket_attachments"

	// OSS / S3 / COS 连接参数；仅在对应 storage 选中时生效。
	TicketAttachmentOSSEndpoint        = ""
	TicketAttachmentOSSBucket          = ""
	TicketAttachmentOSSRegion          = ""
	TicketAttachmentOSSAccessKeyId     = ""
	TicketAttachmentOSSAccessKeySecret = ""
	TicketAttachmentOSSCustomDomain    = ""

	TicketAttachmentS3Endpoint        = ""
	TicketAttachmentS3Bucket          = ""
	TicketAttachmentS3Region          = ""
	TicketAttachmentS3AccessKeyId     = ""
	TicketAttachmentS3AccessKeySecret = ""
	TicketAttachmentS3CustomDomain    = ""

	TicketAttachmentCOSEndpoint     = ""
	TicketAttachmentCOSBucket       = ""
	TicketAttachmentCOSRegion       = ""
	TicketAttachmentCOSSecretId     = ""
	TicketAttachmentCOSSecretKey    = ""
	TicketAttachmentCOSCustomDomain = ""

	TicketAttachmentSignedURLTTL int64 = 900 // 云端签名 URL 有效期（秒）
)

// TicketAttachmentAllowedExtList 按逗号拆分白名单后缀，返回小写、无空项的切片。
func TicketAttachmentAllowedExtList() []string {
	raw := strings.Split(TicketAttachmentAllowedExts, ",")
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(strings.ToLower(item))
		item = strings.TrimPrefix(item, ".")
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

// TicketAttachmentAllowedMimeList 拆分 MIME 白名单，保留通配符（如 image/*）。
func TicketAttachmentAllowedMimeList() []string {
	raw := strings.Split(TicketAttachmentAllowedMimes, ",")
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(strings.ToLower(item))
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

// IsTicketAttachmentExtAllowed 判断单个后缀是否命中白名单（小写、无点前缀）。
func IsTicketAttachmentExtAllowed(ext string) bool {
	ext = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
	if ext == "" {
		return false
	}
	for _, allowed := range TicketAttachmentAllowedExtList() {
		if allowed == ext {
			return true
		}
	}
	return false
}

// IsTicketAttachmentMimeAllowed 判断 MIME 是否命中白名单，支持 "type/*" 通配符。
func IsTicketAttachmentMimeAllowed(mime string) bool {
	mime = strings.ToLower(strings.TrimSpace(mime))
	if mime == "" {
		return false
	}
	// 去掉 "text/plain; charset=utf-8" 这类参数
	if idx := strings.Index(mime, ";"); idx >= 0 {
		mime = strings.TrimSpace(mime[:idx])
	}
	for _, allowed := range TicketAttachmentAllowedMimeList() {
		if allowed == mime {
			return true
		}
		if strings.HasSuffix(allowed, "/*") {
			prefix := strings.TrimSuffix(allowed, "/*") + "/"
			if strings.HasPrefix(mime, prefix) {
				return true
			}
		}
	}
	return false
}
