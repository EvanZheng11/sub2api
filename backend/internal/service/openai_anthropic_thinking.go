package service

import (
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// normalizeOpenAIAnthropicThinking 将 OpenAI 客户端的 thinking 扩展转为
// reasoning 语义，避免把 DeepSeek 的开关当成 Claude 原生 thinking 模式校验。
// 只用于 OpenAI→Anthropic 边界；原生 /v1/messages 仍严格校验原始参数。
func normalizeOpenAIAnthropicThinking(body []byte, req *apicompat.ResponsesRequest, model string) error {
	if !isClaude55SignedThinkingModel(model) {
		return nil
	}
	validationBody := body
	switch gjson.GetBytes(body, "thinking.type").String() {
	case "enabled", "disabled":
		disabled := gjson.GetBytes(body, "thinking.type").String() == "disabled"
		if disabled {
			reasoning := apicompat.ResponsesReasoning{}
			if req.Reasoning != nil {
				reasoning = *req.Reasoning
			}
			reasoning.Effort = "none"
			req.Reasoning = &reasoning
		}
		var err error
		validationBody, err = sjson.DeleteBytes(body, "thinking")
		if err != nil {
			return fmt.Errorf("normalize OpenAI thinking: %w", err)
		}
	}
	// 保留 sampling、强制工具选择和其他原生扩展的已有校验，不能静默降级。
	return validateClaude55Request(validationBody, model)
}
