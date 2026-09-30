package service

// 本文件收敛 OpenAI 入站到 opencode_go 账号的协议分派。
// 具体协议转换、流式响应和工具调用继续复用既有 apicompat 与转发链。

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// ensureOpenCodeGoForwardCredentials 在转发前校验 OpenCode Go 的 API Key 凭据。
// 不记录密钥内容；缺少凭据或 URL 时不发起上游请求。
func ensureOpenCodeGoForwardCredentials(account *Account) error {
	if account == nil {
		return fmt.Errorf("OpenCode 转发账号为空")
	}
	if !account.IsOpenCodeGo() || !BindingCapabilityAllows(PlatformOpenAI, account) {
		slog.Warn("OpenCode 转发账号不支持 OpenAI 入站", "账号ID", account.ID)
		return fmt.Errorf("账号 %d 不支持 OpenAI 到 OpenCode 的兼容转发", account.ID)
	}
	if account.Type != AccountTypeAPIKey {
		slog.Warn("OpenCode 转发账号类型不支持", "账号ID", account.ID)
		return fmt.Errorf("OpenCode 账号 %d 仅支持 API Key 类型，当前类型为 %q", account.ID, account.Type)
	}
	if strings.TrimSpace(account.GetCredential("api_key")) == "" {
		slog.Warn("OpenCode 转发缺少 API Key", "账号ID", account.ID)
		return fmt.Errorf("OpenCode 账号 %d 缺少 credentials.api_key", account.ID)
	}
	if openCodeGoConfiguredBaseURL(account, APIProtocolChatCompletions) == "" &&
		openCodeGoConfiguredBaseURL(account, APIProtocolResponses) == "" &&
		openCodeGoConfiguredBaseURL(account, APIProtocolAnthropic) == "" {
		slog.Warn("OpenCode 转发缺少上游地址", "账号ID", account.ID)
		return fmt.Errorf("OpenCode 账号 %d 缺少 credentials.base_url 或 credentials.api_base_urls", account.ID)
	}
	return nil
}

func openCodeGoConfiguredBaseURL(account *Account, protocol string) string {
	if account == nil {
		return ""
	}
	if baseURL, present := openCodeGoExplicitBaseURL(account, protocol); present {
		return baseURL
	}
	if baseURL := strings.TrimSpace(account.GetCredential("base_url")); baseURL != "" {
		return baseURL
	}
	return strings.TrimSpace(account.GetCNProtocolBaseURL(protocol))
}

func openCodeGoExplicitBaseURL(account *Account, protocol string) (string, bool) {
	switch values := account.Credentials["api_base_urls"].(type) {
	case map[string]any:
		value, present := values[protocol]
		if !present {
			return "", false
		}
		baseURL, ok := value.(string)
		if !ok {
			return "", true
		}
		baseURL = strings.TrimSpace(baseURL)
		return baseURL, baseURL != ""
	case map[string]string:
		baseURL, present := values[protocol]
		if !present {
			return "", false
		}
		baseURL = strings.TrimSpace(baseURL)
		return baseURL, baseURL != ""
	default:
		return "", false
	}
}

// 保留已有带前缀的精确映射；未命中时按去前缀模型查账号映射。
// OpenAI 分组默认模型不能作为 OpenCode 的账号模型兜底。
func resolveOpenCodeGoForwardModel(account *Account, requestedModel string) string {
	mapped, matched := account.ResolveMappedModel(requestedModel)
	if !matched {
		mapped = account.GetMappedModel(normalizeOpenCodeGoModelID(requestedModel))
	}
	return normalizeOpenCodeGoModelID(mapped)
}

// openCodeGoForwardBaseURL 与三条实际发送链共用，禁止配置预检后回退到其他域名。
func (s *OpenAIGatewayService) openCodeGoForwardBaseURL(account *Account, protocol string) (string, error) {
	base := openCodeGoConfiguredBaseURL(account, protocol)
	if base == "" {
		slog.Warn("OpenCode 转发缺少协议上游地址", "账号ID", account.ID, "协议", protocol)
		return "", fmt.Errorf("OpenCode 账号 %d 缺少 %s 协议上游地址，请配置 credentials.api_base_urls 或 credentials.base_url", account.ID, protocol)
	}
	parsed, err := url.Parse(base)
	if err == nil && parsed.User == nil && parsed.RawQuery == "" && parsed.Fragment == "" {
		if validated, validateErr := s.validateUpstreamBaseURL(base); validateErr == nil {
			return validated, nil
		}
	}
	// 校验器错误可能携带 URL 凭据，因此只输出账号与协议，不回显地址或底层错误。
	slog.Warn("OpenCode 转发上游地址无效或不被允许", "账号ID", account.ID, "协议", protocol)
	return "", fmt.Errorf("OpenCode 账号 %d 的 %s 协议 base_url 无效或不被允许", account.ID, protocol)
}

// forwardOpenCodeGoFromResponses 处理 Responses 入站到 OpenCode Go。
// 目标协议为 Responses 时返回 (nil, nil)，交给原有 Responses 通用链。
func (s *OpenAIGatewayService) forwardOpenCodeGoFromResponses(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
) (*OpenAIForwardResult, error) {
	if err := ensureOpenCodeGoForwardCredentials(account); err != nil {
		return nil, err
	}
	mapped := resolveOpenCodeGoMappedModel(account, body, "")
	protocol := openCodeGoNativeProtocol(account, mapped)
	if _, err := s.openCodeGoForwardBaseURL(account, protocol); err != nil {
		return nil, err
	}
	switch protocol {
	case APIProtocolAnthropic:
		return s.forwardResponsesViaNativeAnthropic(ctx, c, account, body, "")
	case APIProtocolResponses:
		return nil, nil
	default:
		return s.forwardResponsesViaRawChatCompletions(ctx, c, account, body)
	}
}

// forwardOpenCodeGoFromChatCompletions 处理 Chat Completions 入站到 OpenCode Go。
// 目标协议为 Responses 时返回 (nil, nil)，交给原有 CC 到 Responses 链。
func (s *OpenAIGatewayService) forwardOpenCodeGoFromChatCompletions(
	ctx context.Context,
	c *gin.Context,
	account *Account,
	body []byte,
	defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	if err := ensureOpenCodeGoForwardCredentials(account); err != nil {
		return nil, err
	}
	mapped := resolveOpenCodeGoMappedModel(account, body, defaultMappedModel)
	protocol := openCodeGoNativeProtocol(account, mapped)
	if _, err := s.openCodeGoForwardBaseURL(account, protocol); err != nil {
		return nil, err
	}
	if protocol == APIProtocolResponses {
		return nil, nil
	}

	isResponsesShape := !gjson.GetBytes(body, "messages").Exists() && gjson.GetBytes(body, "input").Exists()
	if isResponsesShape {
		if protocol == APIProtocolAnthropic {
			return s.forwardResponsesViaNativeAnthropic(ctx, c, account, body, "")
		}
		var responsesReq apicompat.ResponsesRequest
		if err := json.Unmarshal(body, &responsesReq); err != nil {
			return nil, fmt.Errorf("解析 Responses 形状的 Chat Completions 请求失败: %w", err)
		}
		chatReq, err := apicompat.ResponsesToChatCompletionsRequestWithOptions(
			&responsesReq,
			&apicompat.ResponsesToChatOptions{ReasoningContentByID: s.reasoningContentByID},
		)
		if err != nil {
			return nil, fmt.Errorf("转换 Responses 形状的 Chat Completions 请求失败: %w", err)
		}
		chatBody, err := json.Marshal(chatReq)
		if err != nil {
			return nil, fmt.Errorf("序列化转换后的 Chat Completions 请求失败: %w", err)
		}
		return s.forwardAsRawChatCompletions(ctx, c, account, chatBody, defaultMappedModel)
	}
	if protocol == APIProtocolAnthropic {
		return s.forwardChatCompletionsViaNativeAnthropic(ctx, c, account, body, defaultMappedModel)
	}
	return s.forwardAsRawChatCompletions(ctx, c, account, body, defaultMappedModel)
}
