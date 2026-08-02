package attachment

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting"
	"github.com/aliyun/aliyun-oss-go-sdk/oss"
)

// OSSStorage 对接阿里云 OSS。
// 设计取舍：
//   - 每次调用 Put/Get 时才获取 Client，而不是在进程启动时初始化——便于管理员改配置立即生效；
//   - SignedURL 使用 OSS 的 SignURL，生效期由 setting.TicketAttachmentSignedURLTTL 控制；
//   - 下载时通过 response-content-disposition 参数把前端显示文件名带进去，避免二次代理。
type OSSStorage struct {
	client *oss.Client
	bucket *oss.Bucket

	customDomain string
}

// NewOSSStorage 按当前 setting 构造 OSS 客户端；任何必填项缺失都返回 ErrStorageNotConfigured。
func NewOSSStorage() (Storage, error) {
	endpoint := strings.TrimSpace(setting.TicketAttachmentOSSEndpoint)
	bucketName := strings.TrimSpace(setting.TicketAttachmentOSSBucket)
	ak := strings.TrimSpace(setting.TicketAttachmentOSSAccessKeyId)
	sk := strings.TrimSpace(setting.TicketAttachmentOSSAccessKeySecret)
	if endpoint == "" || bucketName == "" || ak == "" || sk == "" {
		return nil, ErrStorageNotConfigured
	}
	client, err := oss.New(endpoint, ak, sk)
	if err != nil {
		return nil, fmt.Errorf("init oss client: %w", err)
	}
	bucket, err := client.Bucket(bucketName)
	if err != nil {
		return nil, fmt.Errorf("open oss bucket: %w", err)
	}
	return &OSSStorage{
		client:       client,
		bucket:       bucket,
		customDomain: strings.TrimRight(strings.TrimSpace(setting.TicketAttachmentOSSCustomDomain), "/"),
	}, nil
}

func (s *OSSStorage) Kind() string { return "oss" }

func (s *OSSStorage) Put(ctx context.Context, key string, r io.Reader, size int64, mime string) error {
	if key == "" {
		return errors.New("empty key")
	}
	opts := []oss.Option{}
	if mime != "" {
		opts = append(opts, oss.ContentType(mime))
	}
	if size > 0 {
		opts = append(opts, oss.ContentLength(size))
	}
	return s.bucket.PutObject(key, r, opts...)
}

func (s *OSSStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	rc, err := s.bucket.GetObject(key)
	if err != nil {
		if isOSSNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return rc, nil
}

func (s *OSSStorage) Delete(ctx context.Context, key string) error {
	if err := s.bucket.DeleteObject(key); err != nil {
		if isOSSNotFound(err) {
			return nil
		}
		return err
	}
	return nil
}

func (s *OSSStorage) Exists(ctx context.Context, key string) (bool, error) {
	ok, err := s.bucket.IsObjectExist(key)
	if err != nil {
		return false, err
	}
	return ok, nil
}

func (s *OSSStorage) SignedURL(ctx context.Context, key string, ttl time.Duration, filename string) (string, error) {
	if ttl <= 0 {
		ttl = time.Duration(setting.TicketAttachmentSignedURLTTL) * time.Second
	}
	var opts []oss.Option
	if filename != "" {
		// RFC 5987 友好的 UTF-8 文件名
		disp := fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(filename))
		opts = append(opts, oss.ResponseContentDisposition(disp))
	}
	signed, err := s.bucket.SignURL(key, oss.HTTPGet, int64(ttl.Seconds()), opts...)
	if err != nil {
		return "", err
	}
	if s.customDomain == "" {
		return signed, nil
	}
	// 替换为自定义域名：保留路径与 querystring，仅改 scheme+host。
	u, perr := url.Parse(signed)
	if perr != nil {
		return signed, nil
	}
	cd, cerr := url.Parse(s.customDomain)
	if cerr != nil || cd.Host == "" {
		return signed, nil
	}
	u.Scheme = cd.Scheme
	u.Host = cd.Host
	return u.String(), nil
}

func isOSSNotFound(err error) bool {
	if err == nil {
		return false
	}
	var srvErr oss.ServiceError
	if errors.As(err, &srvErr) {
		return srvErr.StatusCode == 404
	}
	return false
}
