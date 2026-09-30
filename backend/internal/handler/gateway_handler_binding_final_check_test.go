package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// TestFinalBindingCheckRejects 锁定 chat_completions 转发前终检的绑定判定：
// gemini 分组不再硬编码"账号平台必须为 gemini"——antigravity 账号开启
// mixed_scheduling 后应放行，其他不支持的账号类型和未注册跨平台组合仍跳过；
// 非 gemini 分组维持原有行为（终检不做绑定拦截）。
func TestFinalBindingCheckRejects(t *testing.T) {
	mixedOAuth := &service.Account{Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth, Extra: map[string]any{"mixed_scheduling": true}}
	mixedUpstream := &service.Account{Platform: service.PlatformAntigravity, Type: service.AccountTypeUpstream, Extra: map[string]any{"mixed_scheduling": true}}
	mixedOff := &service.Account{Platform: service.PlatformAntigravity, Type: service.AccountTypeOAuth, Extra: map[string]any{"mixed_scheduling": false}}

	cases := []struct {
		name          string
		groupPlatform string
		account       *service.Account
		want          bool
	}{
		{"gemini分组加gemini账号放行", service.PlatformGemini, &service.Account{Platform: service.PlatformGemini}, false},
		{"gemini分组加antigravity OAuth混合调度开启放行", service.PlatformGemini, mixedOAuth, false},
		{"gemini分组加antigravity Upstream明确排除", service.PlatformGemini, mixedUpstream, true},
		{"gemini分组加antigravity混合调度关闭跳过", service.PlatformGemini, mixedOff, true},
		{"gemini分组加未注册平台跳过", service.PlatformGemini, &service.Account{Platform: service.PlatformOpenAI}, true},
		{"anthropic分组加antigravity Upstream维持放行", service.PlatformAnthropic, mixedUpstream, false},
		{"非gemini分组维持原有行为", service.PlatformAnthropic, &service.Account{Platform: service.PlatformOpenAI}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := finalBindingCheckRejects(tc.groupPlatform, tc.account)
			require.Equal(t, tc.want, got)
		})
	}

	if !shouldUseAntigravityCompat(mixedOAuth) {
		t.Fatal("Antigravity OAuth 应进入兼容转发")
	}
	if shouldUseAntigravityCompat(mixedUpstream) {
		t.Fatal("Antigravity Upstream 不应进入兼容转发")
	}
}
