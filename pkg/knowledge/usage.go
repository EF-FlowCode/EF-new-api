package knowledge

import (
	"encoding/json"
	"fmt"
)

// TokenUsage retains presence: an authoritative zero is not missing usage.
type TokenUsage struct {
	Prompt       *int `json:"prompt_tokens"`
	Input        *int `json:"input_tokens"`
	Total        *int `json:"total_tokens"`
	Output       *int `json:"output_tokens"`
	Completion   *int `json:"completion_tokens"`
	Image        *int `json:"image_tokens"`
	Text         *int `json:"text_tokens"`
	InputDetails struct {
		Image *int `json:"image_tokens"`
		Text  *int `json:"text_tokens"`
	} `json:"input_tokens_details"`
	PromptDetails struct {
		Image *int `json:"image_tokens"`
		Text  *int `json:"text_tokens"`
	} `json:"prompt_tokens_details"`
}

type Usage struct {
	Input      int
	Output     int
	Image      int
	ImageKnown bool
	Estimated  bool
}

func ParseUsage(body []byte, estimate int, aliLegacyTextInput bool) (Usage, error) {
	var response struct {
		Usage *TokenUsage `json:"usage"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return Usage{}, err
	}
	return NormalizeUsage(response.Usage, estimate, aliLegacyTextInput)
}

func NormalizeUsage(u *TokenUsage, estimate int, aliLegacyTextInput bool) (Usage, error) {
	result := Usage{}
	if u == nil {
		result.Input = max(0, estimate)
		result.Estimated = true
		return result, nil
	}
	for _, p := range []*int{u.Prompt, u.Input, u.Total, u.Output, u.Completion, u.Image, u.Text, u.InputDetails.Image, u.InputDetails.Text, u.PromptDetails.Image, u.PromptDetails.Text} {
		if p != nil && *p < 0 {
			return result, fmt.Errorf("negative knowledge model token usage")
		}
	}
	output := u.Completion
	if output == nil {
		output = u.Output
	}
	if output != nil {
		result.Output = *output
	}
	image := u.PromptDetails.Image
	if image == nil {
		image = u.InputDetails.Image
	}
	if image == nil {
		image = u.Image
	}
	if image != nil {
		result.Image, result.ImageKnown = *image, true
	}
	switch {
	case u.Prompt != nil:
		result.Input = *u.Prompt
	case u.Total != nil:
		if *u.Total < result.Output {
			return result, fmt.Errorf("total tokens smaller than output tokens")
		}
		result.Input = *u.Total - result.Output
	case u.Input != nil:
		result.Input = *u.Input
		if aliLegacyTextInput {
			if result.Image > int(^uint(0)>>1)-result.Input {
				return result, fmt.Errorf("token usage overflow")
			}
			result.Input += result.Image
		}
	default:
		result.Input, result.Estimated = max(0, estimate), true
	}
	if result.Image > result.Input {
		return result, fmt.Errorf("image tokens exceed input tokens")
	}
	if result.Output > int(^uint(0)>>1)-result.Input {
		return result, fmt.Errorf("token usage overflow")
	}
	return result, nil
}
