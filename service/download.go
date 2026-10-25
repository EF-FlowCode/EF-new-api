package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"eflow.local/new-api/common"
	"eflow.local/new-api/setting/system_setting"
)

// WorkerRequest Worker请求的数据结构。Redirect 字段用于协商逐跳重定向合同，详见
// docs/development/worker-outbound-contract.md。
type WorkerRequest struct {
	URL           string               `json:"url"`
	Key           string               `json:"key"`
	Method        string               `json:"method,omitempty"`
	Headers       map[string]string    `json:"headers,omitempty"`
	Body          json.RawMessage      `json:"body,omitempty"`
	Redirect      string               `json:"redirect,omitempty"`
	RedirectToken string               `json:"redirect_token,omitempty"`
	Target        *WorkerTargetBinding `json:"target,omitempty"`
}

// WorkerTargetBinding pins one URL host to the complete IP set validated by
// new-api. A compliant Worker must connect to one of IPs without resolving Host
// again, while preserving Host for HTTP Host and TLS SNI/certificate checks.
type WorkerTargetBinding struct {
	Version string   `json:"version"`
	Scheme  string   `json:"scheme"`
	Host    string   `json:"host"`
	Port    int      `json:"port"`
	IPs     []string `json:"ips"`
}

func doWorkerRequest(ctx context.Context, settings system_setting.WorkerSettings, req *WorkerRequest) (*http.Response, error) {
	if !settings.Enabled() {
		return nil, fmt.Errorf("worker not enabled")
	}
	if !settings.AllowHTTPImageRequest && !strings.HasPrefix(req.URL, "https") {
		return nil, fmt.Errorf("only support https url")
	}

	workerUrl := settings.URL
	if !strings.HasSuffix(workerUrl, "/") {
		workerUrl += "/"
	}

	// 序列化worker请求数据
	workerPayload, err := common.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal worker payload: %v", err)
	}

	workerRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, workerUrl, bytes.NewBuffer(workerPayload))
	if err != nil {
		return nil, fmt.Errorf("failed to create worker request: %v", err)
	}
	workerRequest.Header.Set("Content-Type", "application/json")

	client := GetHttpClient()
	if client == nil {
		return nil, fmt.Errorf("worker HTTP client is not initialized")
	}
	redirectlessClient := *client
	redirectlessClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return redirectlessClient.Do(workerRequest)
}

func DoDownloadRequest(originUrl string, reason ...string) (resp *http.Response, err error) {
	adapter, throughWorker := configuredOutboundHTTPAdapter()
	request := outboundRequest{URL: originUrl}

	if throughWorker {
		common.SysLog(fmt.Sprintf("downloading file from worker: %s, reason: %s", originUrl, strings.Join(reason, ", ")))
	} else {
		common.SysLog(fmt.Sprintf("downloading from origin: %s, reason: %s", common.MaskSensitiveInfo(originUrl), strings.Join(reason, ", ")))
	}
	return doOutboundHTTPRequest(context.Background(), adapter, request, downloadOutboundPolicy())
}
