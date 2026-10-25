// Package attachment 提供工单附件的存储抽象与具体实现。
//
// 设计原则：
//   - 业务层（controller/service）只依赖 Storage 接口，不关心底层是本地磁盘还是对象存储；
//   - 对象存储实现返回签名 URL 供前端直接访问；本地实现走鉴权下载接口，文件不暴露到静态路径；
//   - 所有实现必须保证 key 仅来自服务端生成（uuid.ext），拒绝任何来自客户端的路径片段。
package attachment

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"eflow.local/new-api/setting"
)

// Storage 抽象附件读写操作。所有方法必须是并发安全的。
type Storage interface {
	// Kind 返回存储类型标识（local / oss / s3 / cos），用于在 DB 中登记。
	Kind() string
	// Put 以给定 key 写入数据。实现需要对 key 做最终的安全检查，避免目录穿越。
	Put(ctx context.Context, key string, r io.Reader, size int64, mime string) error
	// Get 以只读方式返回文件内容。调用方负责关闭 reader。
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// Delete 幂等删除，不存在不应报错。
	Delete(ctx context.Context, key string) error
	// Exists 检查 key 是否存在。
	Exists(ctx context.Context, key string) (bool, error)
	// SignedURL 为对象存储生成短时签名地址；本地存储可返回 ("", ErrSignedURLNotSupported)。
	// filename 会作为下载时的显示文件名（Content-Disposition）。
	SignedURL(ctx context.Context, key string, ttl time.Duration, filename string) (string, error)
}

// ErrSignedURLNotSupported 标识当前存储后端不支持生成签名 URL。
// 本地存储返回此错误时，controller 应退回到鉴权代理下载流程。
var ErrSignedURLNotSupported = errors.New("signed url not supported for this storage")

// ErrStorageNotConfigured 当对应云厂商的凭证/桶等必填项未配置时返回。
var ErrStorageNotConfigured = errors.New("storage backend is not fully configured")

// ErrNotFound 对象不存在时的通用错误。
var ErrNotFound = errors.New("object not found")

// Current 根据当前附件快照返回启用的存储实现。
// 每次调用都构造新的实例：实现都是廉价的结构体包装，且设置可能在运行时被热更新。
func Current() (Storage, error) {
	return CurrentWithSettings(setting.GetTicketAttachmentSettings())
}

// CurrentWithSettings constructs a storage client from one complete snapshot,
// keeping backend credentials and related fields from the same version.
func CurrentWithSettings(settings setting.TicketAttachmentSettings) (Storage, error) {
	switch settings.Storage {
	case "", "local":
		return NewLocalStorage(settings.LocalPath)
	case "oss":
		return newOSSStorage(settings)
	case "s3":
		return newS3Storage(settings)
	case "cos":
		return newCOSStorage(settings)
	default:
		return nil, fmt.Errorf("unknown ticket attachment storage: %s", settings.Storage)
	}
}

// ForStorageType 按附件记录保存时的后端构造存储客户端，切换后端后旧附件仍能读取和删除。
// 其余凭证/桶等字段取同一设置快照；目标后端未配置时返回错误。
func ForStorageType(settings setting.TicketAttachmentSettings, storageType string) (Storage, error) {
	settings.Storage = storageType
	return CurrentWithSettings(settings)
}
