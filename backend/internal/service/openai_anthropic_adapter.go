package service

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// SetAnthropicGateway 复用 Anthropic 网关的 OAuth 刷新、客户端兼容和协议转换链。
func (s *OpenAIGatewayService) SetAnthropicGateway(gateway *GatewayService) {
	s.anthropicGateway = gateway
}

// 仅放行已具备双向文本/工具转换的凭据类型，不改变平台自身所属的网关。
func (a *Account) supportsAnthropicOpenAITextBridge() bool {
	if a == nil || normalizePlatformToken(a.Platform) != PlatformAnthropic {
		return false
	}
	switch a.Type {
	case AccountTypeOAuth, AccountTypeSetupToken, AccountTypeAPIKey:
		return true
	default:
		return false
	}
}

// 无显式映射的 Anthropic 账号只能承接 Claude，避免共享后抢走同组的 GPT 请求。
// 显式映射仍可声明自定义别名；不改变原生 Anthropic 分组的模型支持语义。
func supportsOpenAIRequestModel(account *Account, platform, model string) bool {
	if !account.IsModelSupported(model) {
		return false
	}
	if platform == PlatformOpenAI && account.supportsAnthropicOpenAITextBridge() && len(account.GetModelMapping()) == 0 {
		return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "claude-")
	}
	return true
}

func (s *OpenAIGatewayService) forwardViaAnthropicGateway(ctx context.Context, c *gin.Context, account *Account, body []byte, chatCompletions bool) (*OpenAIForwardResult, error) {
	if !account.supportsAnthropicOpenAITextBridge() || s.anthropicGateway == nil {
		return nil, fmt.Errorf("Anthropic 账号的 OpenAI 文本适配器未配置或不支持该凭据类型")
	}
	// Anthropic 没有原生 Responses compact 端点；不能把压缩请求当普通对话转发。
	if isOpenAIResponsesCompactPath(c) || IsOpenAINativeCompactionV2(c) {
		writeResponsesError(c, http.StatusBadRequest, "invalid_request_error", "Anthropic 账号不支持原生 Responses compact")
		return nil, ErrNoAvailableCompactAccounts
	}
	SetActualOpenAIUpstreamEndpoint(c, "/v1/messages")
	var result *ForwardResult
	var err error
	// 部分客户端在 Chat Completions URL 发 Responses 请求体，按实际形状转换。
	if chatCompletions && (gjson.GetBytes(body, "messages").Exists() || !gjson.GetBytes(body, "input").Exists()) {
		result, err = s.anthropicGateway.ForwardAsChatCompletions(ctx, c, account, body, nil)
	} else {
		result, err = s.anthropicGateway.ForwardAsResponses(ctx, c, account, body, nil)
	}
	if result == nil {
		return nil, err
	}
	billingModel := result.UpstreamModel
	if billingModel == "" {
		billingModel = result.Model
	}
	return &OpenAIForwardResult{
		RequestID: result.RequestID, UpstreamHeaders: result.UpstreamHeaders,
		Usage: claudeUsageToOpenAIUsage(&result.Usage), Model: result.Model,
		BillingModel: billingModel, UpstreamModel: result.UpstreamModel,
		UpstreamResponseModel:         result.UpstreamResponseModel,
		UpstreamResponseModelConflict: result.UpstreamResponseModelConflict,
		UpstreamResponseServiceTier:   result.UpstreamResponseServiceTier,
		UpstreamEndpoint:              "/v1/messages", ServiceTier: result.ServiceTier,
		ReasoningEffort: result.ReasoningEffort, RequestedReasoningEffort: result.RequestedReasoningEffort,
		Stream: result.Stream, Duration: result.Duration, FirstTokenMs: result.FirstTokenMs,
		ClientDisconnect: result.ClientDisconnect, SearchCount: result.SearchCount,
	}, err
}
