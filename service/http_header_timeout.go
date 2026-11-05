package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"eflow.local/new-api/setting/operation_setting"
)

// eflow: 等待上游响应头的超时按请求决定，不再固定在 http.Transport 上。
// 流式上游开始生成就回响应头，超时用来发现卡死的上游；非流式上游（如生图）
// 要等结果全部生成完才回响应头，需要更长的上限。两个值都在后台「运行参数」里改。

type responseHeaderTimeoutKey struct{}

var errResponseHeaderTimeout = errors.New("timeout awaiting response headers")

// WithResponseHeaderTimeout 为单个请求指定等待响应头的时长，0 表示不限制。
// 未指定的请求使用后台的通用值 operation_setting.ResponseHeaderTimeoutSeconds。
func WithResponseHeaderTimeout(ctx context.Context, timeout time.Duration) context.Context {
	return context.WithValue(ctx, responseHeaderTimeoutKey{}, timeout)
}

// RelayResponseHeaderTimeout 返回转发请求等待上游响应头的时长。
func RelayResponseHeaderTimeout(isStream bool) time.Duration {
	seconds := operation_setting.ResponseHeaderTimeoutSeconds()
	if !isStream {
		seconds = operation_setting.NonStreamResponseHeaderTimeoutSeconds()
	}
	return headerTimeoutDuration(seconds)
}

func headerTimeoutDuration(seconds int) time.Duration {
	if seconds <= 0 {
		return 0
	}
	// Clamp before conversion so large values cannot wrap into a short timeout.
	return time.Duration(min(seconds, int(math.MaxInt64/int64(time.Second)))) * time.Second
}

func responseHeaderTimeoutFor(ctx context.Context) time.Duration {
	if timeout, ok := ctx.Value(responseHeaderTimeoutKey{}).(time.Duration); ok {
		return timeout
	}
	return headerTimeoutDuration(operation_setting.ResponseHeaderTimeoutSeconds())
}

type headerTimeoutRoundTripper struct {
	base http.RoundTripper
}

func (rt *headerTimeoutRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	timeout := responseHeaderTimeoutFor(req.Context())
	if timeout <= 0 {
		return rt.base.RoundTrip(req)
	}
	ctx, cancel := context.WithCancelCause(req.Context())
	timer := time.AfterFunc(timeout, func() { cancel(errResponseHeaderTimeout) })
	resp, err := rt.base.RoundTrip(req.WithContext(ctx))
	if !timer.Stop() {
		// The timer fired, so the request context is already canceled and any
		// response that raced it cannot be read.
		if resp != nil {
			_ = resp.Body.Close()
		}
		cancel(nil)
		return nil, fmt.Errorf("%w after %s", errResponseHeaderTimeout, timeout)
	}
	if err != nil {
		cancel(nil)
		return nil, err
	}
	// The body is read under the same context; release it once the caller closes it.
	resp.Body = &cancelOnCloseBody{ReadCloser: resp.Body, cancel: cancel}
	return resp, nil
}

func (rt *headerTimeoutRoundTripper) CloseIdleConnections() {
	if closer, ok := rt.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

type cancelOnCloseBody struct {
	io.ReadCloser
	cancel context.CancelCauseFunc
}

func (b *cancelOnCloseBody) Close() error {
	err := b.ReadCloser.Close()
	b.cancel(nil)
	return err
}
