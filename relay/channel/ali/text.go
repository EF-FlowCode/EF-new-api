package ali

import (
	"eflow.local/new-api/dto"
)

// https://help.aliyun.com/document_detail/613695.html?spm=a2c4g.2399480.0.0.1adb778fAdzP9w#341800c0f8w0r

const EnableSearchModelSuffix = "-internet"

func requestOpenAI2Ali(request dto.GeneralOpenAIRequest, upstreamModelName string) *dto.GeneralOpenAIRequest {
	modelName := upstreamModelName
	if modelName == "" {
		modelName = request.Model
	}
	if !dto.IsQwenThinkingBudgetModel(modelName) {
		request.ThinkingBudget = nil
	}

	// DashScope rejects top_p at the 0 and 1 boundaries. Preserve an omitted
	// value so the upstream model can apply its own default.
	if request.TopP != nil {
		if *request.TopP >= 1 {
			value := 0.99
			request.TopP = &value
		} else if *request.TopP <= 0 {
			value := 0.01
			request.TopP = &value
		}
	}
	return &request
}
