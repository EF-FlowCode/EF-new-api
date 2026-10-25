package common

import "strings"

// ResponseModel 记录上游在响应转换前声明的模型名，只用于诊断：
// 绝不能影响路由、计费或下游输出。
//
// eflow: 按上游 ed7c4e35d + 972aed197，只存三个名字，不存 mismatch 标志位；
// 是否不一致每次按当前规则现算（前端 isResponseModelMismatch 同口径），
// 规则调整后历史日志也按新规则判断。
type ResponseModel struct {
	RequestedModel string `json:"requested_model"`
	UpstreamModel  string `json:"upstream_model"`
	ReturnedModel  string `json:"returned_model"`
}

// matches 判断上游声明的模型与请求模型或上游请求模型是否兼容：忽略大小写相等、
// 以其为前缀（日期版本、变体），或以其为后缀（"deepseek/deepseek-v4.1-flash"
// 这类带供应商路径的名字）。
func (r *ResponseModel) matches(model string) bool {
	returned := strings.ToLower(model)
	for _, expected := range []string{r.RequestedModel, r.UpstreamModel} {
		expected = strings.ToLower(expected)
		if expected != "" && (strings.HasPrefix(returned, expected) || strings.HasSuffix(returned, expected)) {
			return true
		}
	}
	return false
}

// Mismatch 报告保留的上游声明是否与请求模型、上游请求模型都不一致。
func (r *ResponseModel) Mismatch() bool {
	return r != nil && r.ReturnedModel != "" && !r.matches(r.ReturnedModel)
}

// ObserveResponseModel 保留第一个与请求不同的上游声明供排查；不一致优先于
// 供应商路径、前缀或大小写这类兼容差异，之后的匹配帧或空帧不会把它抹掉。
// 只能传上游原始声明，不能传响应转换器合成的模型名。
func (info *RelayInfo) ObserveResponseModel(model string) {
	if info == nil || strings.TrimSpace(model) == "" {
		return
	}
	if info.ResponseModel == nil {
		upstream := ""
		if info.ChannelMeta != nil {
			upstream = info.UpstreamModelName
		}
		info.ResponseModel = &ResponseModel{
			RequestedModel: info.OriginModelName,
			UpstreamModel:  upstream,
		}
	}
	observation := info.ResponseModel
	if observation.Mismatch() {
		return
	}
	if observation.matches(model) && observation.ReturnedModel != "" &&
		observation.ReturnedModel != observation.RequestedModel && observation.ReturnedModel != observation.UpstreamModel {
		return
	}
	observation.ReturnedModel = model
}
