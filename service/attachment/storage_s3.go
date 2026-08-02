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
	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go"
)

// S3Storage 适配任何兼容 AWS S3 协议的对象存储（官方 AWS、MinIO、R2 等）。
//   - 需同时配置 endpoint（可选，默认走 AWS）、region、bucket、AK/SK；
//   - 自定义 endpoint 场景强制走 path-style（兼容 MinIO）。
type S3Storage struct {
	client     *s3.Client
	presign    *s3.PresignClient
	bucket     string
	customHost string // 可选 CDN / 自定义域名，仅用于重写签名 URL 的 scheme+host
}

func NewS3Storage() (Storage, error) {
	bucket := strings.TrimSpace(setting.TicketAttachmentS3Bucket)
	region := strings.TrimSpace(setting.TicketAttachmentS3Region)
	ak := strings.TrimSpace(setting.TicketAttachmentS3AccessKeyId)
	sk := strings.TrimSpace(setting.TicketAttachmentS3AccessKeySecret)
	endpoint := strings.TrimSpace(setting.TicketAttachmentS3Endpoint)
	if bucket == "" || region == "" || ak == "" || sk == "" {
		return nil, ErrStorageNotConfigured
	}
	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion(region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(ak, sk, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load aws config: %w", err)
	}
	optFns := []func(*s3.Options){}
	if endpoint != "" {
		optFns = append(optFns, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(endpoint)
			o.UsePathStyle = true
		})
	}
	client := s3.NewFromConfig(cfg, optFns...)
	return &S3Storage{
		client:     client,
		presign:    s3.NewPresignClient(client),
		bucket:     bucket,
		customHost: strings.TrimRight(strings.TrimSpace(setting.TicketAttachmentS3CustomDomain), "/"),
	}, nil
}

func (s *S3Storage) Kind() string { return "s3" }

func (s *S3Storage) Put(ctx context.Context, key string, r io.Reader, size int64, mime string) error {
	if key == "" {
		return errors.New("empty key")
	}
	in := &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(key),
		Body:          r,
		ContentLength: aws.Int64(size),
	}
	if mime != "" {
		in.ContentType = aws.String(mime)
	}
	if size <= 0 {
		// 没有声明长度时 S3 SDK 会 buffer 整个 body，允许但 put 进度未知。
		in.ContentLength = nil
	}
	_, err := s.client.PutObject(ctx, in)
	return err
}

func (s *S3Storage) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isS3NotFound(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return out.Body, nil
}

func (s *S3Storage) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil && !isS3NotFound(err) {
		return err
	}
	return nil
}

func (s *S3Storage) Exists(ctx context.Context, key string) (bool, error) {
	_, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if isS3NotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

func (s *S3Storage) SignedURL(ctx context.Context, key string, ttl time.Duration, filename string) (string, error) {
	if ttl <= 0 {
		ttl = time.Duration(setting.TicketAttachmentSignedURLTTL) * time.Second
	}
	in := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}
	if filename != "" {
		disp := fmt.Sprintf("attachment; filename*=UTF-8''%s", url.PathEscape(filename))
		in.ResponseContentDisposition = aws.String(disp)
	}
	signed, err := s.presign.PresignGetObject(ctx, in, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", err
	}
	rawURL := signed.URL
	if s.customHost == "" {
		return rawURL, nil
	}
	u, perr := url.Parse(rawURL)
	if perr != nil {
		return rawURL, nil
	}
	cd, cerr := url.Parse(s.customHost)
	if cerr != nil || cd.Host == "" {
		return rawURL, nil
	}
	u.Scheme = cd.Scheme
	u.Host = cd.Host
	return u.String(), nil
}

func isS3NotFound(err error) bool {
	if err == nil {
		return false
	}
	var nsk *types.NoSuchKey
	if errors.As(err, &nsk) {
		return true
	}
	var notFound *types.NotFound
	if errors.As(err, &notFound) {
		return true
	}
	var apiErr smithy.APIError
	if errors.As(err, &apiErr) {
		code := apiErr.ErrorCode()
		if code == "NoSuchKey" || code == "NotFound" || code == "404" {
			return true
		}
	}
	return false
}
