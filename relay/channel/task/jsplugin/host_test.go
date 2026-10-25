package jsplugin

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"eflow.local/new-api/plugins"
	"github.com/stretchr/testify/require"
)

func TestAllTenFactoryPluginsBuildThroughHost(t *testing.T) {
	for _, key := range []string{"alibaba", "doubao", "google", "hailuo", "jimeng", "kling", "sora", "sunoapi", "vertex-ai", "vidu"} {
		t.Run(key, func(t *testing.T) {
			source, err := plugins.Source(key)
			require.NoError(t, err)
			h, err := Compile(source)
			require.NoError(t, err)
			meta := h.Meta()
			require.Equal(t, key, h.Identity().Key)
			model := meta.Models[0]
			input := map[string]any{"upstreamModel": model, "model": model, "baseUrl": "https://provider.example", "apiKey": "test-ak|test-sk", "authHeader": "Bearer prepared-test-token", "auth": map[string]any{"authHeader": "Bearer prepared-test-token", "projectId": "test-project"}, "action": "text_to_video", "taskId": "test-task", "requestBody": map[string]any{"model": model, "prompt": "a cat", "duration": 5}}
			if key == "alibaba" {
				model = "wan2.6-t2v"
				input["model"] = model
				input["upstreamModel"] = model
				input["requestBody"] = map[string]any{"model": model, "prompt": "a cat", "duration": 5}
			}
			if key == "sunoapi" {
				input["action"] = "music"
				input["requestBody"] = map[string]any{"prompt": "a song", "gpt_description_prompt": "a song", "make_instrumental": true}
			}
			d, err := h.Build(t.Context(), "buildSubmitRequest", input)
			require.NoError(t, err)
			req, err := d.Request(t.Context(), nil)
			require.NoError(t, err)
			require.NotEmpty(t, req.URL.Host)
			ack := map[string]any{"task_id": "test-task", "id": "test-task", "output": map[string]any{"task_id": "test-task"}, "data": map[string]any{"task_id": "test-task"}, "code": 0, "base_resp": map[string]any{"status_code": 0}, "name": "projects/test-project/locations/us-central1/publishers/google/models/veo-3.0-generate-001/operations/test-task"}
			if key == "jimeng" {
				ack["code"] = 10000
			}
			if key == "sunoapi" {
				ack["code"] = "success"
				ack["data"] = "test-task"
			}
			parsedObject, err := h.ParseSubmit(t.Context(), input, Response{Status: 200, Body: ack})
			require.NoError(t, err)
			require.NotEmpty(t, parsedObject["taskId"])
			input["taskId"] = parsedObject["taskId"]
			q, err := h.Build(t.Context(), "buildQueryRequest", input)
			require.NoError(t, err)
			require.NotEmpty(t, q.URL)
			if meta.FetchMode == "batch" {
				batch, err := h.Build(t.Context(), "buildBatchQueryRequest", input, []any{map[string]any{"taskId": "one"}, map[string]any{"taskId": "two"}})
				require.NoError(t, err)
				require.Equal(t, []any{"one", "two"}, batch.Body.(map[string]any)["ids"])
				rows, err := h.ParseBatch(t.Context(), input, Response{Status: 200, Body: map[string]any{"code": "success", "data": []any{map[string]any{"task_id": "one", "status": "IN_PROGRESS"}}}})
				require.NoError(t, err)
				require.Len(t, rows, 1)
			}
			t.Logf("%s@%s: submit=%s, fetch=%s, auth=%s, response=%s", key, h.Identity().Version, d.BodyType, meta.FetchMode, meta.Auth.Type, d.ResponseType)
		})
	}
}

func TestHostExecutorBoundsAndRejectsRedirects(t *testing.T) {
	source, _ := plugins.Source("alibaba")
	h, err := Compile(source)
	require.NoError(t, err)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/leak", 302)
		case "/big":
			_, _ = io.WriteString(w, strings.Repeat("x", MaxPayloadBytes+1))
		default:
			_, _ = io.WriteString(w, `{"ok":true}`)
		}
	}))
	defer srv.Close()
	executor := Executor{Client: srv.Client(), ValidateURL: func(raw string) error { require.True(t, strings.HasPrefix(raw, srv.URL)); return nil }}
	d := Descriptor{URL: srv.URL, Method: "POST", Headers: map[string]string{"Authorization": "secret"}, Body: map[string]any{"hello": "world"}}
	r, err := executor.Execute(t.Context(), h, srv.URL, d, nil)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"ok": true}, r.Body)
	d.URL = srv.URL + "/redirect"
	_, err = executor.Execute(t.Context(), h, srv.URL, d, nil)
	require.ErrorContains(t, err, "redirect")
	require.EqualValues(t, 2, calls.Load())
	d.URL = srv.URL + "/big"
	_, err = executor.Execute(t.Context(), h, srv.URL, d, nil)
	require.ErrorContains(t, err, "limit")
	d.URL = "http://untrusted.example"
	_, err = executor.Execute(t.Context(), h, srv.URL, d, nil)
	require.Error(t, err)
	require.EqualValues(t, 3, calls.Load())
	d.URL = srv.URL
	d.Headers["Host"] = "untrusted.example"
	_, err = executor.Execute(t.Context(), h, srv.URL, d, nil)
	require.Error(t, err)
}

func TestHostMultipartAndInlineFiles(t *testing.T) {
	files := map[string]File{"upload": {Name: "image.png", ContentType: "image/png", Data: []byte("fixture")}}
	d := Descriptor{URL: "https://provider.example", Method: "POST", BodyType: "multipart", Parts: []Part{{Name: "prompt", Value: "hello"}, {Name: "image", FileRef: "upload"}}}
	req, err := d.Request(t.Context(), files)
	require.NoError(t, err)
	require.NoError(t, req.ParseMultipartForm(1024))
	defer req.MultipartForm.RemoveAll()
	require.Equal(t, "hello", req.FormValue("prompt"))
	f, _, err := req.FormFile("image")
	require.NoError(t, err)
	data, err := io.ReadAll(f)
	require.NoError(t, err)
	_ = f.Close()
	require.Equal(t, "fixture", string(data))
	d.BodyType = "json"
	d.Body = map[string]any{"image": map[string]any{"__fileRef": "upload", "encoding": "dataUrl"}}
	req, err = d.Request(t.Context(), files)
	require.NoError(t, err)
	data, err = io.ReadAll(req.Body)
	require.NoError(t, err)
	require.Contains(t, string(data), "data:image/png;base64,Zml4dHVyZQ==")
	_, err = d.Request(t.Context(), nil)
	require.ErrorContains(t, err, "file reference")
}

func TestHostUsageAndCancellation(t *testing.T) {
	source, _ := plugins.Source("alibaba")
	h, err := Compile(source)
	require.NoError(t, err)
	_, err = h.Usage(t.Context(), "wan3.0-video", "extractUsageOnComplete", map[string]any{"model": "wan3.0-video"}, map[string]any{}, map[string]any{"usage": map[string]any{"input_video_duration": -5, "output_video_duration": 10}})
	require.Error(t, err)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = h.Invoke(ctx, "buildSubmitRequest", map[string]any{})
	require.Error(t, err)
	start := time.Now()
	source += ` export function spin(){while(true){}}`
	h, err = Compile(source)
	require.NoError(t, err)
	ctx, cancel = context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, err = h.Invoke(ctx, "spin")
	require.Error(t, err)
	require.Less(t, time.Since(start), time.Second)
}

func TestDifferenceReportDoesNotExposeKeysOrValues(t *testing.T) {
	paths := DifferencePaths(map[string]any{"secret-api-key": map[string]any{"https://signed.example/token": "secret"}, "body": map[string]any{"prompt": "private prompt"}}, map[string]any{"secret-api-key": map[string]any{"https://signed.example/token": "other"}, "body": map[string]any{"prompt": "different"}})
	raw, err := json.Marshal(paths)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "secret")
	require.NotContains(t, string(raw), "signed.example")
	require.NotContains(t, string(raw), "private prompt")
	require.Contains(t, paths, "/body/prompt")
}
