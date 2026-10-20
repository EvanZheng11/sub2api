package service

import (
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/Wei-Shaw/sub2api/internal/pkg/geminicli"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// AccountTestRequestError 表示账号测试请求参数不合法（不会发起上游请求）。
// 处理器会把它转换成 HTTP 400，而不是当成已经写入 SSE 的上游错误。
type AccountTestRequestError struct {
	Message string
}

func (e *AccountTestRequestError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// accountTestReasoningSupport 描述当前账号/模型/测试模式实际使用的 effort 字段与可选档位。
// 只有这里返回的字段才会被真正写进上游请求体，避免给不支持的上游“伪传”参数。
type accountTestReasoningSupport struct {
	Field   string
	Options []string
}

// resolveAccountTestReasoningEffort 校验并归一化管理端传入的 reasoning_effort。
// 空值表示不发送该参数（保持原有测试行为）；未知档位或平台/模型不支持时返回 400 级错误。
func resolveAccountTestReasoningEffort(account *Account, modelID, mode, raw string) (string, error) {
	effort := strings.ToLower(strings.TrimSpace(raw))
	if effort == "" {
		return "", nil
	}

	support, ok := accountTestReasoningSupportFor(account, modelID, mode)
	if !ok {
		return "", &AccountTestRequestError{Message: "当前账号或测试模式不支持 reasoning_effort"}
	}
	for _, option := range support.Options {
		if effort == option {
			return effort, nil
		}
	}
	return "", &AccountTestRequestError{Message: fmt.Sprintf(
		"reasoning_effort %q 不受支持；当前账号/模型可用值：%s",
		strings.TrimSpace(raw),
		strings.Join(support.Options, ", "),
	)}
}

// accountTestReasoningSupportFor 与 TestAccountConnection 的分流顺序保持一致。
func accountTestReasoningSupportFor(account *Account, modelID, mode string) (accountTestReasoningSupport, bool) {
	if account == nil {
		return accountTestReasoningSupport{}, false
	}
	// 按入站协议分流的国产供应商：各原生端点对 effort 的支持不一致，不能安全透传。
	if account.RoutesProtocolByInbound() {
		return accountTestReasoningSupport{}, false
	}

	switch {
	case account.IsOpenAI():
		return accountTestOpenAIReasoningSupport(account, modelID, mode)
	case account.IsGemini():
		return accountTestGeminiReasoningSupport(account, modelID, mode)
	case account.IsGrok():
		return accountTestGrokReasoningSupport(account, modelID, mode)
	case account.Platform == PlatformAntigravity:
		// Antigravity 测试能把 prompt 带入（自定义题目请求体），但网关/测试路径都未接入
		// effort 转换，因此不开放档位，避免伪支持。
		return accountTestReasoningSupport{}, false
	case account.routesByModel():
		// 按模型分流的聚合平台可能落到 Anthropic/Responses/Chat 三种协议，无法统一保证。
		return accountTestReasoningSupport{}, false
	case account.IsTypeSafe():
		return accountTestReasoningSupport{}, false
	case account.Platform == PlatformAnthropic:
		return accountTestClaudeReasoningSupport(account, modelID)
	default:
		return accountTestReasoningSupport{}, false
	}
}

func accountTestOpenAIReasoningSupport(account *Account, modelID, mode string) (accountTestReasoningSupport, bool) {
	// compact 是独立探测（原生 v2 compaction），不携带推理档位。
	if normalizeAccountTestMode(mode) == AccountTestModeCompact {
		return accountTestReasoningSupport{}, false
	}
	model := strings.TrimSpace(modelID)
	if model == "" {
		model = openai.DefaultTestModel
	}
	model = account.GetMappedModel(model)
	if isOpenAIImageModel(model) || !accountTestSupportsOpenAIReasoningEffort(model) {
		return accountTestReasoningSupport{}, false
	}

	options := []string{"minimal", "low", "medium", "high", "xhigh"}
	if supportsOpenAIReasoningEffortMax(model) {
		options = append(options, "max")
	}
	// GPT-6 Sol/Luna 额外开放 none（显式关闭推理），与网关归一化保持一致。
	if openai.IsGPT6SolOrLunaModelSpelling(model) {
		options = append([]string{"none"}, options...)
	}
	// 复用网关既有校验（含别名/后缀拼写）：被禁用的档位（如 gpt-6.1-sol 的 minimal/none）
	// 不进入可选矩阵，避免前端可选中后被上游拒绝；直传非法值仍由
	// resolveAccountTestReasoningEffort 在写入 SSE 前转成 400。
	kept := make([]string, 0, len(options))
	for _, option := range options {
		if openai.ValidateGPT61SolReasoningEffort(model, option) == nil {
			kept = append(kept, option)
		}
	}
	return accountTestReasoningSupport{Field: "reasoning.effort", Options: kept}, true
}

// accountTestSupportsOpenAIReasoningEffort 只对仓库已确认的推理模型开放档位，
// 避免给 gpt-4o 等非推理模型发送 reasoning.effort 造成误导性测试结果。
func accountTestSupportsOpenAIReasoningEffort(model string) bool {
	canonical := canonicalizeOpenAIModelAliasSpelling(model)
	if rest, ok := strings.CutPrefix(canonical, "gpt-"); ok {
		major, digits := 0, 0
		for _, r := range rest {
			if r < '0' || r > '9' {
				break
			}
			major = major*10 + int(r-'0')
			digits++
		}
		if digits > 0 {
			return major >= 5
		}
	}
	for _, prefix := range []string{"o1", "o3", "o4"} {
		if canonical == prefix || strings.HasPrefix(canonical, prefix+"-") {
			return true
		}
	}
	return false
}

func accountTestGeminiReasoningSupport(account *Account, modelID, mode string) (accountTestReasoningSupport, bool) {
	if isMediaAccountTestMode(mode) {
		return accountTestReasoningSupport{}, false
	}
	model := strings.TrimSpace(modelID)
	if model == "" {
		model = geminicli.DefaultTestModel
	}
	// 与 testGeminiAccountConnection 相同：静态凭据账号才应用精确模型映射。
	if account.Type == AccountTypeAPIKey || account.Type == AccountTypeServiceAccount {
		if mapped, ok := account.GetModelMapping()[model]; ok {
			model = mapped
		}
	}
	if isImageGenerationModel(model) || !accountTestGeminiSupportsThinkingLevel(model) {
		return accountTestReasoningSupport{}, false
	}
	return accountTestReasoningSupport{
		Field:   "generationConfig.thinkingConfig.thinkingLevel",
		Options: []string{"minimal", "low", "medium", "high"},
	}, true
}

// accountTestGeminiSupportsThinkingLevel 仅 Gemini 3 及更新模型使用 thinkingLevel；
// Gemini 2.5 使用 thinkingBudget，不在本测试的档位映射范围内。
func accountTestGeminiSupportsThinkingLevel(model string) bool {
	id := strings.ToLower(strings.TrimSpace(model))
	id = strings.TrimPrefix(id, "models/")
	if slash := strings.LastIndex(id, "/"); slash >= 0 {
		id = id[slash+1:]
	}
	rest, ok := strings.CutPrefix(id, "gemini-")
	if !ok {
		return false
	}
	major, digits := 0, 0
	for _, r := range rest {
		if r < '0' || r > '9' {
			break
		}
		major = major*10 + int(r-'0')
		digits++
	}
	return digits > 0 && major >= 3
}

func accountTestGrokReasoningSupport(account *Account, modelID, mode string) (accountTestReasoningSupport, bool) {
	grokMode := normalizeGrokAccountTestMode(mode)
	if grokMode != AccountTestModeDefault && grokMode != AccountTestModeGrokText {
		return accountTestReasoningSupport{}, false
	}
	model := strings.TrimSpace(modelID)
	if model == "" {
		model = grokDefaultResponsesModel
	}
	if mapped := strings.TrimSpace(account.GetMappedModel(model)); mapped != "" {
		model = mapped
	}
	if !grokSupportsReasoningEffort(model) {
		return accountTestReasoningSupport{}, false
	}
	options := []string{"none", "low", "medium", "high"}
	if GrokSupportsXHighReasoningEffort(model) {
		options = append(options, "xhigh")
	}
	return accountTestReasoningSupport{Field: "reasoning.effort", Options: options}, true
}

func accountTestClaudeReasoningSupport(account *Account, modelID string) (accountTestReasoningSupport, bool) {
	// Bedrock invoke 只对部分模型保留 output_config.effort，无法稳定保证；
	// Vertex 与原生 Claude 共用 output_config.effort。
	if account.IsBedrock() {
		return accountTestReasoningSupport{}, false
	}
	model := strings.TrimSpace(modelID)
	if model == "" {
		model = claude.DefaultTestModel
	}
	if account.Type == AccountTypeAPIKey {
		model = account.GetMappedModel(model)
	}
	levels := claude.EffortLevelsForModel(model)
	if len(levels) == 0 {
		return accountTestReasoningSupport{}, false
	}
	return accountTestReasoningSupport{Field: "output_config.effort", Options: levels}, true
}

// firstAccountTestEffort 让内部测试函数保持向后兼容的可变参数调用。
func firstAccountTestEffort(effort []string) string {
	if len(effort) == 0 {
		return ""
	}
	return strings.TrimSpace(effort[0])
}

func isMediaAccountTestMode(mode string) bool {
	switch normalizeGrokAccountTestMode(mode) {
	case AccountTestModeGrokImage, AccountTestModeGrokVideo, AccountTestModeGrokTTS,
		AccountTestModeGrokSTT, AccountTestModeGrokRealtime:
		return true
	default:
		return false
	}
}
