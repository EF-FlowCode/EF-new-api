package ali

import (
	"encoding/json"
	"io"
	"net/http"

	"eflow.local/new-api/dto"
	relaycommon "eflow.local/new-api/relay/common"
	"eflow.local/new-api/service"
	"eflow.local/new-api/types"

	"github.com/gin-gonic/gin"
)

func ConvertRerankRequest(request dto.RerankRequest) *AliRerankRequest {
	returnDocuments := request.ReturnDocuments
	if returnDocuments == nil {
		t := true
		returnDocuments = &t
	}
	return &AliRerankRequest{
		Model: request.Model,
		Input: AliRerankInput{
			Query:     request.Query,
			Documents: request.Documents,
		},
		Parameters: AliRerankParameters{
			Instruct:        request.Instruct,
			FPS:             request.FPS,
			TopN:            request.TopN,
			ReturnDocuments: returnDocuments,
		},
	}
}

func RerankHandler(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) (*types.NewAPIError, *dto.Usage) {
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError), nil
	}
	service.CloseResponseBodyGracefully(resp)

	var aliResponse AliRerankResponse
	err = json.Unmarshal(responseBody, &aliResponse)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError), nil
	}

	if aliResponse.Code != "" {
		return types.WithOpenAIError(types.OpenAIError{
			Message: aliResponse.Message,
			Type:    aliResponse.Code,
			Param:   aliResponse.RequestId,
			Code:    aliResponse.Code,
		}, resp.StatusCode), nil
	}

	usage, usageErr := service.KnowledgeUsage(c, info, responseBody, false)
	if usageErr != nil {
		return types.NewOpenAIError(usageErr, types.ErrorCodeBadResponseBody, http.StatusBadGateway), nil
	}
	results := aliResponse.Output.Results
	if results == nil {
		results = aliResponse.Results
	}
	rerankResponse := dto.RerankResponse{
		Results: results,
		Usage:   *usage,
	}

	jsonResponse, err := json.Marshal(rerankResponse)
	if err != nil {
		return types.NewError(err, types.ErrorCodeBadResponseBody), nil
	}
	c.Writer.Header().Set("Content-Type", "application/json")
	c.Writer.WriteHeader(resp.StatusCode)
	c.Writer.Write(jsonResponse)
	return nil, usage
}
