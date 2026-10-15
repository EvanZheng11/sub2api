package service

// binding_capability.go 是"分组平台 × 账号平台"绑定能力的单一权威来源（设计 §3.2）。
// 绑定为静态关系，能力为运行时判定；矩阵白名单制——未显式列出的跨平台组合一律不支持。

import (
	"log/slog"
	"strings"
)

// normalizePlatformToken 按现有规范化方式处理平台标识：去首尾空白 + 转小写。
// 平台常量（domain.Platform*）本身全为小写，这里仅兜底兼容外部传入的大小写差异。
func normalizePlatformToken(platform string) string {
	return strings.ToLower(strings.TrimSpace(platform))
}

// SupportsBinding 判断入站协议（由分组平台/强制平台决定）能否由账号平台承接。
// 判定顺序：精确平台组合 → 账号 api_protocol 相关组合 → 白名单注册表。
// composite 入站不进入本函数：composite 路由先解析出最终 TargetPlatform
// （设计 §8），本函数只对最终平台生效。
func SupportsBinding(inboundPlatform, accountPlatform, apiProtocol string) bool {
	inbound := normalizePlatformToken(inboundPlatform)
	account := normalizePlatformToken(accountPlatform)
	if inbound == "" || account == "" {
		return false
	}
	if inbound == account {
		// 同平台组合全部支持（apiProtocol 由各平台转发链自行处理）。
		return true
	}
	switch {
	case account == PlatformAntigravity &&
		(inbound == PlatformAnthropic || inbound == PlatformGemini):
		// mixed scheduling 组合；IsMixedSchedulingEnabled 账号级开关
		// 在 bindingCapabilityAllows 中执行（设计 §3.4）。
		return true
	case inbound == PlatformOpenAI && account == PlatformOpenCodeGo:
		// OpenAI Chat Completions / Responses 入站 × opencode_go 账号，
		// 由 opencode_openai_adapter.go 承接（设计 §4）。
		return true
	case inbound == PlatformOpenAI && account == PlatformGrok:
		// OpenAI Chat Completions / Responses 入站 × grok（supergrok）账号：
		// 由 forwardGrokResponses / Chat Completions 转发链按账号平台分流，
		// 账号通过 account_groups 绑定关系承接 openai 分组请求。
		return true
	default:
		return false
	}
}

// bindingCapabilityAllows 在 SupportsBinding 之上叠加账号级资格：
// antigravity 账号承接 anthropic/gemini 分组时必须启用 mixed_scheduling；
// gemini 分组的 antigravity 混合入站仅支持 OAuth 账号。
// 判定统一使用规范化后的平台值，避免大小写/首尾空白绕过 mixed_scheduling 检查。
func bindingCapabilityAllows(platform string, acc *Account) bool {
	if acc == nil {
		return false
	}
	groupPlatform := normalizePlatformToken(platform)
	accountPlatform := normalizePlatformToken(acc.Platform)
	if !SupportsBinding(groupPlatform, accountPlatform, acc.GetAPIProtocol()) {
		return false
	}
	if groupPlatform == PlatformGemini &&
		accountPlatform == PlatformAntigravity &&
		acc.Type == AccountTypeUpstream {
		// Gemini 混合入站的 Chat 兼容转发只支持 Antigravity OAuth。
		return false
	}
	if accountPlatform == PlatformAntigravity && accountPlatform != groupPlatform {
		// IsMixedSchedulingEnabled 内部用原始 Platform 做前置判断，
		// 这里传入规范化后的副本，避免大小写/首尾空白绕过 mixed_scheduling 开关。
		normalizedAcc := *acc
		normalizedAcc.Platform = accountPlatform
		if !normalizedAcc.IsMixedSchedulingEnabled() {
			return false
		}
	}
	return true
}

// filterAccountsByBindingCapability 按能力矩阵内存过滤候选账号；
// 被跳过的账号记 binding_capability_skip Debug 日志（设计 §3.2）。
func filterAccountsByBindingCapability(accounts []Account, platform string) []Account {
	kept := make([]Account, 0, len(accounts))
	for i := range accounts {
		if bindingCapabilityAllows(platform, &accounts[i]) {
			kept = append(kept, accounts[i])
			continue
		}
		slog.Debug("binding_capability_skip",
			"group_platform", platform,
			"account_id", accounts[i].ID,
			"account_platform", accounts[i].Platform,
			"account_type", accounts[i].Type)
	}
	return kept
}

// BindingCapabilityAllows 是 bindingCapabilityAllows 的公开包装，供 handler
// 转发前终检复用同一套"矩阵白名单 + 账号级 mixed_scheduling 资格"判定，
// 避免在 handler 层重复硬编码平台匹配规则。
func BindingCapabilityAllows(platform string, acc *Account) bool {
	return bindingCapabilityAllows(platform, acc)
}
