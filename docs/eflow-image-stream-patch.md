# eflow 私改：`/v1/images/generations` 流式透传

> 本文档对应 `eflow:` 分支的三条 commit：`dd014512d` / `3b8796ba4` / `d97fec39e`，
> 给 `/v1/images/generations` 与 `/v1/images/edits` 加 OpenAI 官方的
> **`stream:true` + `partial_images`** 流式协议透传。
>
> 仅作用于 image_generation 入口；chat / responses / claude / gemini / 其它端点零影响。

---

## TL;DR

**问题**：upstream new-api 完全没接 image_generation 的流式协议。`stream:true` 在转换层被吞、`IsStream(c)` 硬编码 false、handler 全量缓冲，整条链路被堵成同步阻塞。结果：4K medium/high 这种 5–25 分钟的长任务，CF / nginx / 中间链路 60s idle 必把客户端切断，永远拿不到结果。

**修法**：分层补三处：
1. `dto.ImageRequest` 加回 `Stream` 字段，让客户端 `stream:true` 真的能传到 upstream
2. `adaptor.DoResponse` 给 `RelayModeImagesGenerations` 加流式分支，新增 `OaiImageStreamHandler` 透传 SSE
3. `image_handler.go` 在 DoRequest 之前先给 client 写 SSE 头 + 起 ping ticker（pre-flight keepalive），DoRequest 返回后用新加的 `RelayInfo.ForcePing` 字段强制让 `StreamScannerHandler` 持续 ping

效果：客户端发 `stream:true + partial_images:N` 时，整条链路 idle 永远 < 11s，CF 60s 墙完全打通；上游 SSE 事件原样转发，partial_image / completed 客户端都能拿到。

**剩余风险**：上游通道（gpt-image-2 接 codex/vmvia 那套）本身偶发 `partial_image` 之后 disconnect。这是上游服务器的稳定性问题，跟本 patch 无关，靠应用层 retry 拿到 `completed` 即可——本 patch 已经把这条路在协议层修通。

---

## 三个 commit

### Commit 1：`dd014512d` —— 透传

**dto/openai_image.go**

1. `ImageRequest.Stream` 字段在原版被注释掉了：
   ```go
   // Stream            bool            `json:"stream,omitempty"`
   ```
   再加上 `MarshalJSON` 显式拒绝合并 `Extra`：
   ```go
   // 不能合并ExtraFields！！！！！！！！
   // for k, v := range r.Extra { ... }
   ```
   导致客户端传的 `stream:true` 进 `Extra` 后被静默吞掉，永远到不了上游。
   修：把 `Stream bool` 字段加回来，正常走结构体序列化。

2. `IsStream(c *gin.Context)` 原版硬编码 `return false`，`info.IsStream` 永远是 false。
   修：改成 `return i.Stream`。

**relay/channel/openai/adaptor.go**

`DoResponse` 的 image gen 分支原本：
```go
case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
    usage, err = OpenaiHandlerWithUsage(c, info, resp)
```
对照 `default`（chat）分支有完整的 `if info.IsStream { OaiStreamHandler } else { OpenaiHandler }`，**image gen 完全没有流式分支**，无论上游是不是 SSE 都走 `OpenaiHandlerWithUsage`（`io.ReadAll(resp.Body)` 一次性吃完）。

修：加 stream 分支：
```go
case relayconstant.RelayModeImagesGenerations, relayconstant.RelayModeImagesEdits:
    if info.IsStream {
        usage, err = OaiImageStreamHandler(c, info, resp)
    } else {
        usage, err = OpenaiHandlerWithUsage(c, info, resp)
    }
```

**relay/channel/openai/relay-openai.go**

新增 `OaiImageStreamHandler`：复用 `helper.StreamScannerHandler` 框架（自带 bufio.Scanner + 大 buffer + ping ticker + goroutine 调度），callback 里：
- 每行 `data:` 调 `helper.StringData(c, data)` 透传给客户端
- 同时尝试解析 JSON 抽 `usage` 字段（image_generation.completed 事件携带）
- 流末 `helper.Done(c)` 写 `[DONE]`

usage 计费规则：
- `usage.InputTokens` → `usage.PromptTokens`
- `usage.OutputTokens` → `usage.CompletionTokens`
- `usage.TotalTokens` 兜底为 prompt+completion 之和
- 仍为 0 时强制 ≥1，避免 0-quota 写表异常

---

### Commit 2：`3b8796ba4` —— pre-flight keepalive

发现 commit 1 跑通后小图（1024×1024 low）OK，但 4K 仍 61s 死。

**根因**：`adaptor.DoRequest` 是同步阻塞调用，上游在 4K 上首字节（response headers）要等 30s ~ 25min。这段等待期间 new-api 没给客户端写任何字节，CF/中间链路按 60s idle 把客户端连接掐了，`OaiImageStreamHandler` 还没机会执行。

**relay/image_handler.go** 新增 `preFlightStreamKeepalive(c *gin.Context)`：
```go
func preFlightStreamKeepalive(c *gin.Context) (stop func(), writeMutex *sync.Mutex) {
    helper.SetEventStreamHeaders(c)            // 立刻把 200 + text/event-stream 头发出去
    writeMutex = &sync.Mutex{}
    stopChan := make(chan struct{})
    var wg sync.WaitGroup

    stop = func() {
        ...
        close(stopChan)
        wg.Wait()  // 必须等 ticker goroutine 完整退出，否则后续接管的 stream handler
                   // 在 ticker 还在写 PingData 的瞬间会和它并发写 c.Writer
    }

    wg.Add(1)
    gopool.Go(func() {
        defer wg.Done()
        t := time.NewTicker(10 * time.Second)
        defer t.Stop()
        for {
            select {
            case <-t.C:
                writeMutex.Lock()
                _ = helper.PingData(c)
                writeMutex.Unlock()
            case <-stopChan:
                return
            case <-c.Request.Context().Done():
                return
            }
        }
    })
    return stop, writeMutex
}
```

`ImageHelper` 里在 `DoRequest` 之前调用：
```go
var stopKeepalive func()
if info.IsStream {
    stopKeepalive, _ = preFlightStreamKeepalive(c)
    defer func() {
        if stopKeepalive != nil {
            stopKeepalive()
        }
    }()
}

resp, err := adaptor.DoRequest(c, info, requestBody)
if err != nil { ... }
if stopKeepalive != nil {
    stopKeepalive()
    stopKeepalive = nil   // 让 defer 不重复调
}
```

关键细节：`stop()` 必须 `wg.Wait()` 等 ticker goroutine 真正退出再返回，否则后续接管的 `OaiImageStreamHandler` 跟 ticker 并发写 `c.Writer` 会出竞态。

---

### Commit 3：`d97fec39e` —— ForcePing

发现 commit 2 跑通后客户端撑过 60s 拿到 HTTP 200 + 几个 PING，但 partial 之间又 60s 死。

**根因**：`StreamScannerHandler` 内置 ping 走 `operation_setting.GetGeneralSetting().PingIntervalEnabled`，**默认 false**。`OaiImageStreamHandler` 接管之后，到上游下一次 `partial_image` 之间（4K medium 实测 200s+）没人吐字节给客户端。
更糟：`StreamScannerHandler` 的 scanner 把 SSE `:` 注释心跳当 non-data 行直接跳过，上游自己的 keepalive 也漏不出去。

**修法 1**：加 `RelayInfo.ForcePing` 字段（`DisablePing` 的反向开关）：
```go
// relay/common/relay_info.go
ForcePing bool // 强制对该请求开启自定义 Ping（覆盖全局 PingIntervalEnabled，
               // 用于 image gen 这类长沉默任务）
```

**修法 2**：`relay/helper/stream_scanner.go` 把 pingEnabled 逻辑改成：
```go
// 优先级：DisablePing > ForcePing > 全局开关
pingEnabled := !info.DisablePing && (info.ForcePing || generalSettings.PingIntervalEnabled)
```
`ForcePing` 时若全局 `PingIntervalSeconds` > 30s 自动压回 `DefaultPingInterval`（10s），保证 60s 墙之前一定有 ping。

**修法 3**：`OaiImageStreamHandler` 入口设 `info.ForcePing = true`：
```go
func OaiImageStreamHandler(c *gin.Context, info *relaycommon.RelayInfo, resp *http.Response) (...) {
    ...
    info.ForcePing = true
    ...
    helper.StreamScannerHandler(c, resp, info, func(data string, sr *helper.StreamResult) { ... })
}
```

只影响这一次请求；不污染全局设置；对 chat/responses 等其它走 `StreamScannerHandler` 的路径零影响。

---

## 链路时间线（patch 后）

```
client POST {stream:true, partial_images:N}
     ↓
new-api ImageHelper:
     │ 1. preFlightStreamKeepalive(c) → 立刻 SetEventStreamHeaders, 起 10s ticker
     │ 2. adaptor.DoRequest(c, info, body) ... 阻塞等上游 headers (4K 可能 30s+)
     │    └ 期间 ticker 每 10s `: PING` 给 client，CF idle 不触发
     ↓
upstream HTTP 200 (text/event-stream)
     ↓
new-api: stopKeepalive() → wg.Wait() 等 ticker 退出
     ↓
adaptor.DoResponse → OaiImageStreamHandler
     │ info.ForcePing = true
     │ helper.StreamScannerHandler 接管（10s ping ticker 持续吐）
     ↓
上游 SSE event: partial_image / completed → 透传 client
     ↓
[DONE] + 计费写表
```

任何环节 idle 间隔 < 11s，CF 60s 墙完全打通。

---

## 文件改动一览

| 文件 | 变更 |
|---|---|
| `dto/openai_image.go` | 恢复 `Stream` 字段；`IsStream()` 返回 `i.Stream` |
| `relay/channel/openai/adaptor.go` | `DoResponse` ImageGen 分支按 `info.IsStream` 二分 |
| `relay/channel/openai/relay-openai.go` | 新增 `OaiImageStreamHandler` 函数 |
| `relay/image_handler.go` | 新增 `preFlightStreamKeepalive`，`ImageHelper` 在 DoRequest 前后接入 |
| `relay/common/relay_info.go` | 加 `RelayInfo.ForcePing bool` 字段 |
| `relay/helper/stream_scanner.go` | `pingEnabled` 逻辑加 `ForcePing` 路径，超长 interval 自动压回 |

总计：6 个文件，新增 ~145 行，修改 ~10 行。

---

## 兼容性

| 客户端行为 | 路径 | 影响 |
|---|---|---|
| 不传 `stream` 字段 | `OpenaiHandlerWithUsage`（旧路径） | 行为完全没变 |
| 传 `stream:true` | `OaiImageStreamHandler`（新路径） | 透传 SSE，自动 ping |
| 传 `stream:false` | `OpenaiHandlerWithUsage` | 同 "不传" |
| 客户端未启用流式，但上游返回 `text/event-stream` | 响应阶段将 `info.IsStream` 提升为 `true` 后进入 `OaiImageStreamHandler` | 没有 pre-flight 心跳，但拿到响应头后可透传上游 SSE |
| 上游不返 SSE（Content-Type ≠ text/event-stream） | `info.IsStream` 来自 client，仍走流式 handler；scanner 读到非 SSE 数据全部 skip，最终 client 收 `[DONE]` 后空响应。**这种上游配该 channel 不要传 stream:true** | 不会崩，但拿不到图 |

其他端点（chat / responses / claude / gemini / embedding / rerank / audio / mjproxy 等）一律没碰，零影响。

---

## 部署 / 回滚

部署：`docker build -t new-api-eflow . && docker save -o ... && scp ... && docker compose up -d`，参考仓库根的 `deploy.ps1` 脚本。

回滚：
```bash
git revert d97fec39e 3b8796ba4 dd014512d
```
然后重 build/load/up。

---

## 已知不在 patch 范围内的事

- 上游 `codex.vmvia.com` 当前对 4K medium/high 稳定性差，`partial_image` 之后偶发 disconnect。直连测同款症状（与 new-api 完全无关）。需要应用层 retry 直到拿到 `completed`，patch 已经把这条协议路打通，应用层重试就能拿到结果。
- 客户端如果不传 `stream:true`，4K 长任务依然会 60s 死。这是预期：旧路径就是同步阻塞，patch 只解决加了 `stream:true` 的场景。建议用客户端默认开 stream。
