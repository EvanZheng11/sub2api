//go:build unit

package service

import "testing"

// TestSupportsBindingMatrix 能力矩阵全组合表驱动用例（设计 §3.2）。
// 矩阵白名单制：未显式注册的跨平台组合一律不支持。
func TestSupportsBindingMatrix(t *testing.T) {
	cases := []struct {
		name            string
		inboundPlatform string
		accountPlatform string
		apiProtocol     string
		want            bool
	}{
		{"同平台 anthropic", PlatformAnthropic, PlatformAnthropic, "", true},
		{"同平台 openai", PlatformOpenAI, PlatformOpenAI, "", true},
		{"同平台 gemini", PlatformGemini, PlatformGemini, "", true},
		{"同平台 antigravity", PlatformAntigravity, PlatformAntigravity, "", true},
		{"同平台 opencode", PlatformOpenCodeGo, PlatformOpenCodeGo, APIProtocolAdaptive, true},
		{"同平台 kimi", PlatformKimi, PlatformKimi, "", true},
		{"antigravity 到 anthropic（mixed scheduling）", PlatformAnthropic, PlatformAntigravity, "", true},
		{"antigravity 到 gemini（mixed scheduling）", PlatformGemini, PlatformAntigravity, "", true},
		{"openai 到 opencode（本设计新增适配器）", PlatformOpenAI, PlatformOpenCodeGo, APIProtocolAdaptive, true},
		{"anthropic 到 opencode 不支持", PlatformAnthropic, PlatformOpenCodeGo, APIProtocolAdaptive, false},
		{"gemini 到 opencode 不支持", PlatformGemini, PlatformOpenCodeGo, APIProtocolAdaptive, false},
		{"openai 到 anthropic 不支持", PlatformOpenAI, PlatformAnthropic, "", false},
		{"anthropic 到 openai 不支持", PlatformAnthropic, PlatformOpenAI, "", false},
		{"openai 到 kimi 不支持", PlatformOpenAI, PlatformKimi, "", false},
		{"kimi 到 openai 不支持", PlatformKimi, PlatformOpenAI, "", false},
		{"antigravity 到 openai 不支持", PlatformOpenAI, PlatformAntigravity, "", false},
		{"composite 入站未注册不支持", PlatformComposite, PlatformAnthropic, "", false},
		{"grok 到 anthropic 不支持", PlatformGrok, PlatformAnthropic, "", false},
		{"空入站平台", "", PlatformOpenAI, "", false},
		{"空白入站平台", "   ", PlatformOpenAI, "", false},
		{"空账号平台", PlatformOpenAI, "", "", false},
		{"空白账号平台", PlatformOpenAI, "  ", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SupportsBinding(tc.inboundPlatform, tc.accountPlatform, tc.apiProtocol)
			if got != tc.want {
				t.Fatalf("SupportsBinding(%q, %q, %q) = %v, want %v",
					tc.inboundPlatform, tc.accountPlatform, tc.apiProtocol, got, tc.want)
			}
		})
	}
}

// TestSupportsBindingPlatformNormalization 平台标识大小写/首尾空白
// 按现有规范化方式（strings.ToLower + strings.TrimSpace）处理后再匹配矩阵。
func TestSupportsBindingPlatformNormalization(t *testing.T) {
	cases := []struct {
		name            string
		inboundPlatform string
		accountPlatform string
		apiProtocol     string
		want            bool
	}{
		{"入站平台大写混排", "OpenAI", PlatformOpenCodeGo, APIProtocolAdaptive, true},
		{"账号平台大写混排", PlatformOpenAI, "OpenCode_Go", APIProtocolAdaptive, true},
		{"入站平台带首尾空白", " openai ", PlatformOpenCodeGo, APIProtocolAdaptive, true},
		{"账号平台带首尾空白", PlatformAnthropic, " Anthropic ", "", true},
		{"空白规整后为空不支持", " \t", PlatformOpenAI, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SupportsBinding(tc.inboundPlatform, tc.accountPlatform, tc.apiProtocol)
			if got != tc.want {
				t.Fatalf("SupportsBinding(%q, %q, %q) = %v, want %v",
					tc.inboundPlatform, tc.accountPlatform, tc.apiProtocol, got, tc.want)
			}
		})
	}
}

// TestBindingCapabilityAllowsAntigravitySwitch antigravity 账号承接
// anthropic/gemini 分组时必须打开 IsMixedSchedulingEnabled（设计 §3.4）。
func TestBindingCapabilityAllowsAntigravitySwitch(t *testing.T) {
	mixedOn := &Account{Platform: PlatformAntigravity, Extra: map[string]any{"mixed_scheduling": true}}
	mixedOff := &Account{Platform: PlatformAntigravity, Extra: map[string]any{"mixed_scheduling": false}}
	native := &Account{Platform: PlatformAnthropic}

	if !bindingCapabilityAllows(PlatformAnthropic, mixedOn) {
		t.Fatal("mixed_scheduling 开启的 antigravity 账号应可承接 anthropic 分组")
	}
	if bindingCapabilityAllows(PlatformAnthropic, mixedOff) {
		t.Fatal("mixed_scheduling 关闭的 antigravity 账号不应承接 anthropic 分组")
	}
	if !bindingCapabilityAllows(PlatformAntigravity, &Account{Platform: PlatformAntigravity}) {
		t.Fatal("antigravity 分组应可承接 antigravity 账号")
	}
	if bindingCapabilityAllows(PlatformAntigravity, native) {
		t.Fatal("antigravity 分组不应承接 anthropic 账号（矩阵未注册）")
	}
	if !bindingCapabilityAllows(PlatformAnthropic, &Account{Platform: PlatformAnthropic}) {
		t.Fatal("anthropic 分组应可承接 anthropic 账号")
	}
	if bindingCapabilityAllows(PlatformAnthropic, nil) {
		t.Fatal("nil 账号不可承接任何分组")
	}
}

// TestFilterAccountsByBindingCapability 候选账号按能力矩阵内存过滤，
// 未通过能力的账号被跳过，通过账号保持原有顺序。
func TestFilterAccountsByBindingCapability(t *testing.T) {
	accounts := []Account{
		{ID: 1, Platform: PlatformAnthropic},
		{ID: 2, Platform: PlatformAntigravity, Extra: map[string]any{"mixed_scheduling": true}},
		{ID: 3, Platform: PlatformOpenAI},
		{ID: 4, Platform: PlatformAntigravity, Extra: map[string]any{"mixed_scheduling": false}},
		{ID: 5, Platform: PlatformAnthropic},
	}

	kept := filterAccountsByBindingCapability(accounts, PlatformAnthropic)
	if len(kept) != 3 {
		t.Fatalf("anthropic 分组候选应保留 3 个账号，实际保留 %d 个：%+v", len(kept), kept)
	}
	wantIDs := []int64{1, 2, 5}
	for i, want := range wantIDs {
		if kept[i].ID != want {
			t.Fatalf("保留顺序不匹配：位置 %d 期望账号 %d，实际 %d", i, want, kept[i].ID)
		}
	}

	// gemini 分组仅保留 mixed_scheduling 开启的 antigravity 账号（ID 2）。
	keptGemini := filterAccountsByBindingCapability(accounts, PlatformGemini)
	if len(keptGemini) != 1 || keptGemini[0].ID != 2 {
		t.Fatalf("gemini 分组应仅保留 ID 2 的 antigravity 账号，实际保留：%+v", keptGemini)
	}
}
