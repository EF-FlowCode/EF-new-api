# 翻译术语表 (Translation Glossary)

> 定位：现行开发与行为约束。带日期的部署或验证段落保留为当时记录；分类入口见[文档导航](../README.md)。

本文档为翻译贡献者提供项目中关键术语的标准翻译参考，以确保翻译的一致性和准确性。

This document provides standard translation references for key terminology in the project to ensure consistency and accuracy for translation contributors.

## 核心概念 (Core Concepts)

| 中文 | English | 说明 | Description |
|------|---------|------|-------------|
| 倍率 | Ratio | 用于计算价格的乘数因子 | Multiplier factor used for price calculation |
| API 密钥 | API Key | 用户调用接口的 `sk-` 凭证（界面不再称“令牌”） | User-facing `sk-` credential for API calls |
| Token | Token | 模型处理的文本单元，计量词保留英文（如“预算 Token”“美元/百万 Token”） | Text units processed by models |
| 访问令牌 | Access Token | `nap_` 个人访问令牌，与 API 密钥是不同概念 | `nap_` personal access token, distinct from API keys |
| 渠道 | Channel | API服务提供商的接入通道 | Access channel for API service providers |
| 分组 | Group | 用户或 API 密钥的分类，影响价格倍率（API 密钥上的称“API 密钥分组”） | Classification of users or API keys, affecting price ratios |
| 额度 | Quota | 用户可用的服务额度 | Available service quota for users |

## 模型相关 (Model Related)

| 中文 | English | 说明 | Description |
|------|---------|------|-------------|
| 提示 | Prompt | 模型输入内容 | Model input content |
| 补全 | Completion | 模型输出内容 | Model output content |
| 输入 | Input/Prompt | 发送给模型的内容 | Content sent to the model |
| 输出 | Output/Completion | 模型返回的内容 | Content returned by the model |
| 模型倍率 | Model Ratio | 不同模型的计费倍率 | Billing ratio for different models |
| 补全倍率 | Completion Ratio | 输出内容的额外计费倍率 | Additional billing ratio for output content |
| 固定价格 | Price per call | 按次计费的价格 | Fixed price per call |
| 按量计费 | Pay-as-you-go | 根据使用量计费 | Billing based on usage |
| 按次计费 | Pay-per-view | 每次调用固定价格 | Fixed price per invocation |

## 用户管理 (User Management)

| 中文 | English | 说明 | Description |
|------|---------|------|-------------|
| 超级管理员 | Root User | 最高权限管理员 | Administrator with highest privileges |
| 管理员 | Admin User | 系统管理员 | System administrator |
| 普通用户 | Normal User | 普通权限用户 | Regular user with standard privileges |

## 充值与兑换 (Recharge & Redemption)

| 中文 | English | 说明 | Description |
|------|---------|------|-------------|
| 充值 | Top Up | 为账号增加额度 | Add quota to account |
| 兑换码 | Redemption Code | 可兑换额度的代码 | Code that can be redeemed for quota |

## 渠道管理 (Channel Management)

| 中文 | English | 说明 | Description |
|------|---------|------|-------------|
| 渠道 | Channel | API服务提供通道 | API service provider channel |
| 密钥 | Key | API访问密钥 | API access key |
| 优先级 | Priority | 渠道选择优先级 | Channel selection priority |
| 权重 | Weight | 负载均衡权重 | Load balancing weight |
| 代理 | Proxy | 代理服务器地址 | Proxy server address |
| 模型重定向 | Model Mapping | 请求体中模型名称替换 | Model name replacement in request body |

## 安全相关 (Security Related)

| 中文 | English | 说明 | Description |
|------|---------|------|-------------|
| 两步验证 | Two-Factor Authentication | 为账号提供额外安全保护的验证方式 | Additional security verification method for accounts |
| 2FA | Two-Factor Authentication | 两步验证的缩写 | Abbreviation for Two-Factor Authentication |

## 计费相关 (Billing Related)

| 中文 | English | 说明 | Description |
|------|---------|------|-------------|
| 倍率 | Ratio | 价格计算的乘数因子 | Multiplier factor used for price calculation |
| 倍率 | Multiplier | 价格计算的乘数因子（同义词） | Multiplier factor used for price calculation (synonym) |

## 翻译注意事项 (Translation Guidelines)

- **提示 (Prompt)** = 模型输入内容 / Model input content
- **补全 (Completion)** = 模型输出内容 / Model output content
- **倍率 (Ratio)** = 价格计算的乘数因子 / Multiplier factor for price calculation
- **额度 (Quota)** = 可用的用户服务额度，有时也翻译为 Credit / Available service quota for users, sometimes also translated as Credit
- **Token** = 根据上下文可能指 / Depending on context, may refer to:
  - API 密钥 (API Key / API Token)：中文界面统一译“API 密钥”
  - 模型处理的文本单元 (Text Token)：中文保留“Token”
  - 个人访问令牌 (Access Token)：中文保留“访问令牌”
- **账号 (Account)**：中文界面统一用“账号”，不用“账户”

---

**贡献说明**: 如发现术语翻译不一致或有更好的翻译建议，欢迎提交 Issue 或 Pull Request。

**Contribution Note**: If you find any inconsistencies in terminology translations or have better translation suggestions, please feel free to submit an Issue or Pull Request.
