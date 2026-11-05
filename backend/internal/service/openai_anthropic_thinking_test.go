//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestNormalizeOpenAIAnthropicThinking(t *testing.T) {
	for _, tc := range []struct {
		name, model, body, effort, wantEffort, wantType string
		wantErr                                         bool
	}{
		{name: "sonnet_enabled_default", model: "claude-sonnet-5-5", body: `{"thinking":{"type":"enabled"}}`, wantEffort: "high", wantType: "adaptive"},
		{name: "opus_enabled", model: "claude-opus-5-5", body: `{"thinking":{"type":"enabled"}}`, effort: "xhigh", wantEffort: "xhigh", wantType: "adaptive"},
		{name: "haiku_enabled", model: "claude-haiku-5-5", body: `{"thinking":{"type":"enabled"}}`, effort: "low", wantEffort: "low", wantType: "adaptive"},
		{name: "haiku_disabled", model: "claude-haiku-5-5", body: `{"thinking":{"type":"disabled"}}`, effort: "high", wantEffort: "low", wantType: "disabled"},
		{name: "opus_off_still_unsupported", model: "claude-opus-5-5", body: `{"thinking":{"type":"disabled"}}`, wantErr: true},
		{name: "forced_choice_still_rejected", model: "claude-sonnet-5-5", body: `{"thinking":{"type":"enabled"},"tool_choice":"required"}`, wantErr: true},
		{name: "sampling_still_rejected", model: "claude-sonnet-5-5", body: `{"thinking":{"type":"enabled"},"temperature":0.5}`, wantErr: true},
		{name: "invalid_effort_still_rejected", model: "claude-sonnet-5-5", body: `{"thinking":{"type":"enabled"}}`, effort: "invalid", wantErr: true},
		{name: "other_models_unchanged", model: "claude-sonnet-4.5", body: `{"thinking":{"type":"disabled"}}`, effort: "high", wantEffort: "high", wantType: "enabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := &apicompat.ResponsesRequest{Model: tc.model, Input: json.RawMessage(`"hi"`)}
			if tc.effort != "" {
				req.Reasoning = &apicompat.ResponsesReasoning{Effort: tc.effort}
			}
			err := normalizeOpenAIAnthropicThinking([]byte(tc.body), req, tc.model)
			var out *apicompat.AnthropicRequest
			if err == nil {
				out, err = apicompat.ResponsesToAnthropicRequest(req)
			}
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantType, out.Thinking.Type)
			require.Equal(t, tc.wantEffort, out.OutputConfig.Effort)
		})
	}
	// 修复只在 OpenAI 边界生效，不能放宽原生 Anthropic 的限制。
	require.Error(t, validateClaude55Request([]byte(`{"thinking":{"type":"enabled"}}`), "claude-sonnet-5-5"))
	require.Error(t, validateClaude55Request([]byte(`{"thinking":{"type":"disabled"}}`), "claude-sonnet-5-5"))
}

// 使用 Harness 的实际字段组合经过两种 OpenAI 入口，检查最终上游参数和缓存。
func TestOpenAIAnthropicHarnessThinking(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, native := range []bool{false, true} {
		for _, chat := range []bool{false, true} {
			for _, stream := range []bool{false, true} {
				for _, tc := range []struct{ name, thinking, effort, wantType, wantEffort string }{
					{"low", "enabled", "low", "adaptive", "low"},
					{"high", "enabled", "high", "adaptive", "high"},
					{"max", "enabled", "max", "adaptive", "max"},
					{"off", "disabled", "", "between_tools", "low"},
					{"off_overrides_effort", "disabled", "high", "between_tools", "low"},
				} {
					name := tc.name
					if native {
						name += "/native"
					} else {
						name += "/bridge"
					}
					if chat {
						name += "/chat"
					} else {
						name += "/responses"
					}
					if stream {
						name += "/sse"
					} else {
						name += "/json"
					}
					t.Run(name, func(t *testing.T) {
						payload := map[string]any{"model": "public-claude", "stream": stream, "thinking": map[string]any{"type": tc.thinking}}
						path := "/v1/responses"
						if chat {
							path = "/v1/chat/completions"
							payload["messages"] = []any{map[string]any{"role": "user", "content": "hi"}}
							if tc.effort != "" {
								payload["reasoning_effort"] = tc.effort
							}
						} else {
							payload["input"] = "hi"
							if tc.effort != "" {
								payload["reasoning"] = map[string]any{"effort": tc.effort}
							}
						}
						body, err := json.Marshal(payload)
						require.NoError(t, err)
						rec := httptest.NewRecorder()
						c, _ := gin.CreateTestContext(rec)
						c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
						upstream := &httpUpstreamRecorder{resp: nativeAnthropicEventsResponse(nativeAnthropicMessageStart, nativeAnthropicTextStart, nativeAnthropicTextDelta, nativeAnthropicTextStop, nativeAnthropicTerminalDelta, nativeAnthropicMessageStop)}
						cfg := rawChatCompletionsTestConfig()
						svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
						account := &Account{ID: 86, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "fixture-token", "model_mapping": map[string]any{"public-claude": "claude-sonnet-5-5"}}}
						if native {
							account = nativeAnthropicTestAccount()
							account.Credentials["model_mapping"] = map[string]any{"public-claude": "claude-sonnet-5-5"}
							if chat {
								_, err = svc.forwardChatCompletionsViaNativeAnthropic(context.Background(), c, account, body, "")
							} else {
								_, err = svc.forwardResponsesViaNativeAnthropic(context.Background(), c, account, body, "")
							}
						} else {
							svc.SetAnthropicGateway(&GatewayService{cfg: cfg, httpUpstream: upstream, tlsFPProfileService: &TLSFingerprintProfileService{}})
							if chat {
								_, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
							} else {
								_, err = svc.Forward(context.Background(), c, account, body)
							}
						}
						require.NoError(t, err, rec.Body.String())
						require.NotNil(t, upstream.lastReq)
						sent, err := io.ReadAll(upstream.lastReq.Body)
						require.NoError(t, err)
						require.Equal(t, "claude-sonnet-5-5", gjson.GetBytes(sent, "model").String())
						require.Equal(t, tc.wantType, gjson.GetBytes(sent, "thinking.type").String())
						require.Equal(t, tc.wantEffort, gjson.GetBytes(sent, "output_config.effort").String())
						require.False(t, gjson.GetBytes(sent, "thinking.budget_tokens").Exists())
						if !native {
							_, messages, tools, system := collectCacheControlPaths(sent)
							require.NotEmpty(t, messages)
							require.LessOrEqual(t, len(messages)+len(tools)+len(system), maxCacheControlBlocks)
							for _, p := range messages {
								require.Equal(t, "5m", gjson.GetBytes(sent, p+".ttl").String())
							}
							require.Contains(t, getHeaderRaw(upstream.lastReq.Header, "user-agent"), "claude-cli/")
						}
						require.Equal(t, http.StatusOK, rec.Code)
						require.Contains(t, rec.Body.String(), "first")
					})
				}
			}
		}
	}
}
