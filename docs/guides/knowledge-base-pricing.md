# 知识库模型定价与能力

> 定位：现行使用与配置指南。带日期的部署或验证段落保留为当时记录；分类入口见[文档导航](../README.md)。

本文适用于 Embedding / Rerank 模型，包括 `Tools · Knowledge Base` 分组。模型能力由模型名和渠道确定，不依赖分组名称；同类模型放到其他分组后仍使用相同规则。

## 普通模型定价中的知识库字段

入口：网关设置 → 定价比例，或新建/编辑模型中的“编辑模型定价”。所有模型共用价格编辑器，无需能力检测即可填写各维度价格。

知识库使用统一的 `ModelPricing` 记录，同步价格保存为固定单价表达式，Batch 存入同一模型记录。旧独立选项已一次性迁移，仅保留计算/API 投影。价格输入字段单位为美元／百万 Token；同步表达式使用 `v2:` 并返回美元金额，宿主统一转为 quota（详见[统一金额单位](model-pricing.md#统一金额单位)）；`0` 表示免费，留空表示未配置。

新配置 Embedding/Rerank 时将输出价设为 `0`；迁移保留原只计输入的表达式。未知模型也可以预设 Batch 价，但不会因此获得供应商的文件批处理能力。图片价格留空时按文本输入价计费；Batch 价格留空时关闭 Batch，填写 `0` 时允许免费 Batch。

| 字段 | 作用 |
| --- | --- |
| `input` | 必填；同步输入价。没有模态用量明细时，也按此价格结算全部输入 |
| `image` | 多模态模型可选；仅上游返回图片／视频视觉 Token 数时单独计价 |
| `batch_input` | 原生文本 Embedding Batch 输入价；仅允许官方支持的模型 |

知识库模型沿用普通模型的表达式预扣与结算；Embedding / Rerank 不对输出向量或相关性分数计输出费。配置按精确模型名存储，重命名模型时需同时迁移对应定价字段。

模型广场的卡片、表格、详情及分组比较显示同步输入和已配置的模态价格，知识库模型隐藏零输出项。`knowledge.batch_file` 表示供应商已确认支持；`knowledge.batch_groups` 表示当前具备兼容渠道的分组。展示 Batch 价格还要求该分组可用且已经填写 `batch_input`。创建时会再次检查实时配置。

`/api/ratio_config` 继续输出普通模型倍率和表达式；Batch 价格通过 `BatchInputRatio` 单独公开，不能从同步倍率自行推导。

## 用量口径

- 优先使用上游 `prompt_tokens`；否则由 `total_tokens - output_tokens` 得到输入，再回退到 `input_tokens`。明确的 `0` 不会被估算替换。
- 旧版 `multimodal-embedding-v1` 以及 Qwen VL 的 `input_tokens` 仅表示文本；缺少总量时加上视觉 Token。`tongyi-embedding-vision-*` 的 `input_tokens` 已包含图片，不能重复相加。
- 有图片明细时，表达式先从总输入扣除图片量，再分别计算文字和图片费用。无明细时使用 `input` 统一收费，不根据图片数量或 URL 长度虚构图片 Token。
- 同步缺少权威用量时使用现有估算，并在消费日志 `other.knowledge_usage_source` 标记 `estimated`；有权威用量则为 `upstream`。`knowledge_image_usage_known` 标记图片明细是否存在。
- Rerank 的计量包含 query 与每篇文档的配对处理，不只计算 query 一次；最终以供应商返回的用量为准，不能按 `top_n` 返回条数收费。
- Batch 只接受成功行的权威输入用量；缺失、重复或不一致的结果会延后结算，不使用字符估算作为最终账单。

## 官方价格与当前线上配置

2026-09-14 核对并配置公开分组 `0_Tools_Knowledge_Base` 的 10 个模型。按运营者要求，模型基础价采用**百炼北京官方人民币／百万输入 Token**。本站当前人民币 1:1 计价，保留原分组倍率 `0.5`，因此该分组标准价格是官方基础价的一半；客户层额外折扣仍按原规则应用。价格为此次核对后手动配置，不会自动跟随供应商调价。

| 模型 | 官方／本站同步基础价 | 官方／本站 Batch 基础价 | 当前分组同步价 | 当前分组 Batch 价 |
| --- | --- | --- | --- | --- |
| qwen3.7-text-embedding | 0.5 | 0.25 | 0.25 | 0.125 |
| qwen3.7-text-embedding-flash | 0.125 | 0.063 | 0.0625 | 0.0315 |
| text-embedding-v4 | 0.5 | 0.25 | 0.25 | 0.125 |
| qwen3.7-text-rerank | 0.5 | 不支持 | 0.25 | — |
| qwen3-rerank | 0.5 | 不支持 | 0.25 | — |
| gte-rerank-v2 | 0.8 | 不支持 | 0.4 | — |
| qwen3-vl-embedding | 文本 0.7；视觉 1.8 | 不支持 | 文本 0.35；视觉 0.9 | — |
| qwen3-vl-rerank | 文本／图片均为 0.5 | 不支持 | 0.25 | — |
| tongyi-embedding-vision-flash | 0.15 | 不支持 | 0.075 | — |
| tongyi-embedding-vision-plus | 0.5 | 不支持 | 0.25 | — |

flash 的 Batch 官方文档报价为 0.063，不直接假设为精确的 0.0625。以上当前分组价格包含原有 0.5 倍率，不能将其当成供应商成本或无折扣售价。

`qwen3-vl-embedding` 只有取得视觉 Token 明细时才应用图片单价；无明细则按 `input` 统一结算，不能宣称已经完成视觉费用拆分。此次未发起付费供应商调用，真实账号返回的明细仍需在使用中核对。`qwen3-vl-rerank` 官方当前文本／图片价均为 0.5，此次专用价格已按该口径配置，与旧描述沿用 Embedding 的 0.7／1.8 不同。

### 换算成后台价格

在其他兑换设置下，设 `R` 为用户购买 1 站内美元额度实际需要支付的人民币，`g` 为最终分组倍率，`S` 为希望用户最终支付的人民币／百万 Token，则：

```text
后台 input 或 batch_input = S / (R × g)
实际人民币费用 = 输入 Tokens / 1,000,000 × 后台价格 × g × R
```

本站此次核实 `R=1`、人民币显示兑换率也为 `1`，官方基础价可以直接按人民币数字填写。以同步基础价 `input=0.5`、分组倍率 `0.5` 为例，最终标准价格为 ¥0.25／百万 Token。客户层特殊折扣、充值优惠和兑换设置发生变化时需重新核对最终售价。

三款 qwen3.7 模型此前的旧倍率为 `37.5`、输出倍率为 `1`。迁移后价格直接写入普通模型定价字段，旧的 `billing_setting.knowledge_prices` 会在启动时一次性转换并删除，不再作为运行时配置。

## 官方依据

- [文本向量同步 API 与地域价格](https://help.aliyun.com/zh/model-studio/text-embedding-synchronous-api)
- [qwen3.7-text-embedding](https://help.aliyun.com/zh/model-studio/qwen3-7-text-embedding)
- [qwen3.7-text-embedding-flash](https://help.aliyun.com/zh/model-studio/qwen3-7-text-embedding-flash)
- [qwen3.7-text-rerank](https://help.aliyun.com/zh/model-studio/qwen3-7-text-rerank)
- [qwen3-rerank](https://help.aliyun.com/zh/model-studio/qwen3-rerank)、[gte-rerank-v2](https://help.aliyun.com/zh/model-studio/gte-rerank-v2)
- [qwen3-vl-rerank](https://help.aliyun.com/zh/model-studio/qwen3-vl-rerank)
- [多模态向量 API、图片用量与价格](https://help.aliyun.com/zh/model-studio/multimodal-embedding-api-reference)
- [Rerank API](https://help.aliyun.com/zh/model-studio/text-rerank-api)
- [Batch API 与模型清单](https://help.aliyun.com/zh/model-studio/batch-interfaces-compatible-with-openai)

Batch 操作及恢复边界见 [knowledge-batch.md](knowledge-batch.md)。供应商可能调整模型价格与能力，上线前应以所选地域和实际账号合同复核。
