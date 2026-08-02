package attachment

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/setting"
	cos "github.com/tencentyun/cos-go-sdk-v5"
)

// COSStorage 对接腾讯云 COS。
// 端点约定：
//   - 如果管理员填了 TicketAttachmentCOSEndpoint，就作为完整 BucketURL；
//   - 否则按 bucket + region 拼接 https://<bucket>.cos.<region>.myqcloud.com。
type COSStorage struct {
	client       *cos.Client
	secretId     string
	secretKey    string
	customDomain string
}

func NewCOSStorage() (Storage, error) {
	bucket := strings.TrimSpace(setting.TicketAttachmentCOSBucket)
	region := strings.TrimSpace(setting.TicketAttachmentCOSRegion)
	id := strings.TrimSpace(setting.TicketAttachmentCOSSecretId)
	key := strings.TrimSpace(setting.TicketAttachmentCOSSecretKey)
	if id == "" || key == "" {
		return nil, ErrStorageNotConfigured
	}

	var bucketURL *url.URL
	if endpoint := strings.TrimSpace(setting.TicketAttachmentCOSEndpoint); endpoint != "" {
		u, err := url.Parse(endpoint)
		if err != nil {
			return nil, fmt.Errorf("parse cos endpoint: %w", err)
		}
		bucketURL = u
	} else {
		if bucket == "" || region == "" {
			return nil, ErrStorageNotConfigured
		}
		u, err := cos.NewBucketURL(bucket, region, true)
		if err != nil {
			return nil, fmt.Errorf("build cos bucket url: %w", err)
		}
		bucketURL = u
	}

	client := cos.NewClient(&cos.BaseURL{BucketURL: bucketURL}, &http.Client{
		Transport: &cos.AuthorizationTransport{
			SecretID:  id,
			SecretKey: key,
		},
	})
	return &COSStorage{
		client:       client,
		secretId:     id,
		secretKey:    key,
		customDomain: strings.TrimRight(strings.TrimSpace(setting.TicketAttachmentCOSCustomDomain), "/"),
	}, nil
}

func (s *COSStorage) Kind() string { return "cos" }

func (s *COSStorage) Put(ctx context.Context, key string, r io.Reader, size int64, mime string) error {
	if key == "" {
		return errors.New("empty key")
	}
	opt := &cos.ObjectPutOptions{
		ObjectPutHeaderOptions: &cos.ObjectPutHeaderOptions{
			ContentType:   mime,
			ContentLength: size,
		},
	}
	_, err := s.client.Object.Put(ctx, key, r, opt)
	return err
}

func (s *COSStorage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	resp, err := s.client.Object.Get(ctx, key, nil)
	if err != nil {
		if isCOSNotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return resp.Body, nil
}

func (s *COSStorage) Delete(ctx context.Context, key string) error {
	_, err := s.client.Object.Delete(ctx, key)
	if err != nil && !isCOSNotFound(err) {
		return err
	}
	return nil
}

func (s *COSStorage) Exists(ctx context.Context, key string) (bool, error) {
	ok, err := s.client.Object.IsExist(ctx, key)
	if err != nil {
		return false, err
	}
	return ok, nil
}

func (s *COSStorage) SignedURL(ctx context.Context, key string, ttl time.Duration, filename string) (string, error) {
	if ttl <= 0 {
		ttl = time.Duration(setting.TicketAttachmentSignedURLTTL) * time.Second
	}
	// COS SDK 不原生支持 response-content-disposition 注入到 presign，需要通过 opt.PresignOptions.
	opt := &cos.PresignedURLOptions{
		Query: &url.Values{},
	}
	if filename != "" {
		opt.Query.Set("response-content-disposition",
			fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(filename)))
	}
	signed, err := s.client.Object.GetPresignedURL(ctx, http.MethodGet, key, s.secretId, s.secretKey, ttl, opt)
	if err != nil {
		return "", err
	}
	raw := signed.String()
	if s.customDomain == "" {
		return raw, nil
	}
	cd, cerr := url.Parse(s.customDomain)
	if cerr != nil || cd.Host == "" {
		return raw, nil
	}
	signed.Scheme = cd.Scheme
	signed.Host = cd.Host
	return signed.String(), nil
}

func isCOSNotFound(err error) bool {
	if err == nil {
		return false
	}
	var cosErr *cos.ErrorResponse
	if errors.As(err, &cosErr) && cosErr.Response != nil && cosErr.Response.StatusCode == http.StatusNotFound {
		return true
	}
	return false
}
