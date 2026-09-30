package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// TestFinalBindingCheckRejects 锁定 chat_completions 转发前终检的绑定判定：
// gemini 分组不再硬编码"账号平台必须为 gemini"——antigravity 账号开启
// mixed_scheduling 后应放行，未注册跨平台组合仍跳过；非 gemini 分组
// 维持原有行为（终检不做绑定拦截）。
func TestFinalBindingCheckRejects(t *testing.T) {
	mixedOn := &service.Account{Platform: service.PlatformAntigravity, Extra: map[string]any{"mixed_scheduling": true}}
	mixedOff := &service.Account{Platform: service.PlatformAntigravity, Extra: map[string]any{"mixed_scheduling": false}}

	cases := []struct {
		name          string
		groupPlatform string
		account       *service.Account
		want          bool
	}{
		{"gemini分组加gemini账号放行", service.PlatformGemini, &service.Account{Platform: service.PlatformGemini}, false},
		{"gemini分组加antigravity混合调度开启放行", service.PlatformGemini, mixedOn, false},
		{"gemini分组加antigravity混合调度关闭跳过", service.PlatformGemini, mixedOff, true},
		{"gemini分组加未注册平台跳过", service.PlatformGemini, &service.Account{Platform: service.PlatformOpenAI}, true},
		{"非gemini分组维持原有行为", service.PlatformAnthropic, &service.Account{Platform: service.PlatformOpenAI}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := finalBindingCheckRejects(tc.groupPlatform, tc.account)
			require.Equal(t, tc.want, got)
		})
	}
}
