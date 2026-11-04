//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestEnsureOpenAIAnthropicMessageCacheBreakpoints(t *testing.T) {
	conversation := `"messages":[
		{"role":"user","content":[{"type":"text","text":"first"}]},
		{"role":"assistant","content":[{"type":"text","text":"reply"}]},
		{"role":"user","content":[{"type":"text","text":"second"}]},
		{"role":"assistant","content":[{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_1","content":"result"}]}
	]`
	for _, tc := range []struct {
		name      string
		model     string
		body      string
		wantPaths []string
		unchanged bool
	}{
		{name: "会话及工具结果", body: `{` + conversation + `}`, wantPaths: []string{"messages.2.content.0.cache_control", "messages.4.content.0.cache_control"}},
		{name: "字符串消息", body: `{"messages":[{"role":"user","content":"hello"}]}`, wantPaths: []string{"messages.0.content.0.cache_control"}},
		{name: "跳过思考与空文本", body: `{"messages":[{"role":"user","content":[{"type":"text","text":"hello"},{"type":"thinking","thinking":"hidden"},{"type":"redacted_thinking","data":"opaque"},{"type":"text","text":" "}]}]}`, wantPaths: []string{"messages.0.content.0.cache_control"}},
		{name: "末条只有思考时向前查找", body: `{"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":[{"type":"thinking","thinking":"hidden"}]}]}`, wantPaths: []string{"messages.0.content.0.cache_control"}},
		{name: "已有会话断点不改写", body: `{"messages":[{"role":"user","content":[{"type":"text","text":"hello","cache_control":{"type":"ephemeral","ttl":"1h"}}]},{"role":"assistant","content":"reply"}]}`, unchanged: true},
		{name: "已有自动缓存不改写", body: `{"cache_control":{"type":"ephemeral","ttl":"1h"},` + conversation + `}`, unchanged: true},
		{name: "已有工具和系统一小时断点", body: `{"tools":[{"cache_control":{"type":"ephemeral","ttl":"1h"}}],"system":[{"type":"text","text":"stable","cache_control":{"type":"ephemeral","ttl":"1h"}}],` + conversation + `}`, wantPaths: []string{"messages.2.content.0.cache_control", "messages.4.content.0.cache_control"}},
		{name: "仅剩一个断点额度", body: `{"system":[{"cache_control":{"type":"ephemeral","ttl":"5m"}},{"cache_control":{"type":"ephemeral","ttl":"5m"}},{"cache_control":{"type":"ephemeral","ttl":"5m"}}],` + conversation + `}`, wantPaths: []string{"messages.4.content.0.cache_control"}},
		{name: "断点额度已满", body: `{"system":[{"cache_control":{}},{"cache_control":{}},{"cache_control":{}},{"cache_control":{}}],` + conversation + `}`, unchanged: true},
		{name: "空消息", body: `{"messages":[]}`, unchanged: true},
		{name: "没有可缓存块", body: `{"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"hidden"},{"type":"text","text":" "}]}]}`, unchanged: true},
		{name: "国产兼容协议不注入", model: "k3", body: `{` + conversation + `}`, unchanged: true},
		{name: "OpenAI模型不注入", model: "gpt-6.1-sol", body: `{` + conversation + `}`, unchanged: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := tc.model
			if model == "" {
				model = "claude-opus-5-5"
			}
			body := []byte(tc.body)
			out := ensureOpenAIAnthropicMessageCacheBreakpoints(body, model)
			if tc.unchanged {
				require.Equal(t, tc.body, string(out))
				return
			}
			invalid, messages, tools, system := collectCacheControlPaths(out)
			require.Empty(t, invalid)
			require.ElementsMatch(t, tc.wantPaths, messages)
			require.LessOrEqual(t, len(messages)+len(tools)+len(system), maxCacheControlBlocks)
			for _, path := range messages {
				require.Equal(t, "ephemeral", gjson.GetBytes(out, path+".type").String())
				require.Equal(t, "5m", gjson.GetBytes(out, path+".ttl").String())
			}
			for _, path := range append(tools, system...) {
				require.Equal(t, gjson.GetBytes(body, path).Raw, gjson.GetBytes(out, path).Raw)
			}
			require.Equal(t, string(out), string(ensureOpenAIAnthropicMessageCacheBreakpoints(out, model)))
		})
	}
}

// 经真实转发入口捕获出站请求；上游用内存桩，不调用付费 API。
func TestOpenAIAnthropicConversationCachePrefix(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeAPIKey} {
		for _, endpoint := range []string{"/v1/responses", "/v1/chat/completions"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s%s/stream=%t", kind, endpoint, stream), func(t *testing.T) {
					cfg := rawChatCompletionsTestConfig()
					upstream := &httpUpstreamRecorder{}
					svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
					svc.SetAnthropicGateway(&GatewayService{cfg: cfg, httpUpstream: upstream, tlsFPProfileService: &TLSFingerprintProfileService{}})
					account := &Account{ID: 86, Platform: PlatformAnthropic, Type: kind,
						Credentials: map[string]any{"access_token": "test-token", "api_key": "test-key"}}
					account.Credentials["model_mapping"] = map[string]any{"project-claude": "claude-opus-5-5"}
					turns := []map[string]any{
						{"role": "user", "content": strings.Repeat("stable history ", 200)},
						{"role": "assistant", "content": "first reply"},
						{"role": "user", "content": "second question"},
						{"role": "assistant", "content": "second reply"},
						{"role": "user", "content": "third question"},
					}
					var previous []gjson.Result
					for _, turnCount := range []int{3, 5} {
						upstream.resp = nativeAnthropicEventsResponse(nativeAnthropicMessageStart, nativeAnthropicTextStart, nativeAnthropicTextDelta, nativeAnthropicTextStop, nativeAnthropicTerminalDelta, nativeAnthropicMessageStop)
						payload := map[string]any{"model": "project-claude", "stream": stream}
						if endpoint == "/v1/responses" {
							payload["input"] = turns[:turnCount]
						} else {
							payload["messages"] = turns[:turnCount]
						}
						body, err := json.Marshal(payload)
						require.NoError(t, err)
						rec := httptest.NewRecorder()
						c, _ := gin.CreateTestContext(rec)
						c.Request = httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(body)))
						if endpoint == "/v1/responses" {
							_, err = svc.Forward(context.Background(), c, account, body)
						} else {
							_, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
						}
						require.NoError(t, err)
						invalid, messages, tools, system := collectCacheControlPaths(upstream.lastBody)
						require.Empty(t, invalid)
						require.NotEmpty(t, messages)
						require.LessOrEqual(t, len(messages)+len(tools)+len(system), maxCacheControlBlocks)
						current := gjson.GetBytes(upstream.lastBody, "messages").Array()
						require.Equal(t, "ephemeral", current[len(current)-1].Get("content.0.cache_control.type").String())
						if previous != nil {
							// 下一轮仍保留上轮末尾的断点，且断点之前的历史内容未变。
							require.Equal(t, "ephemeral", current[len(previous)-1].Get("content.0.cache_control.type").String())
							for i := range previous {
								require.JSONEq(t,
									string(stripMessageCacheControl([]byte(`{"messages":[`+previous[i].Raw+`]}`))),
									string(stripMessageCacheControl([]byte(`{"messages":[`+current[i].Raw+`]}`))))
							}
						}
						previous = current
					}
				})
			}
		}
	}
}

func TestOpenAIAnthropicToolHistoryCache(t *testing.T) {
	for _, endpoint := range []string{"/v1/responses", "/v1/chat/completions"} {
		t.Run(endpoint, func(t *testing.T) {
			cfg := rawChatCompletionsTestConfig()
			upstream := &httpUpstreamRecorder{resp: nativeAnthropicEventsResponse(nativeAnthropicMessageStart, nativeAnthropicTextStart, nativeAnthropicTextDelta, nativeAnthropicTextStop, nativeAnthropicTerminalDelta, nativeAnthropicMessageStop)}
			svc := &OpenAIGatewayService{cfg: cfg}
			svc.SetAnthropicGateway(&GatewayService{cfg: cfg, httpUpstream: upstream, tlsFPProfileService: &TLSFingerprintProfileService{}})
			account := &Account{ID: 86, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Credentials: map[string]any{"access_token": "test-token"}}
			body := []byte(`{"model":"claude-opus-5-5","input":[
				{"role":"user","content":"look up the context"},
				{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{}"},
				{"type":"function_call_output","call_id":"call_1","output":"stable tool result"}
			],"tools":[{"type":"function","name":"lookup","parameters":{"type":"object"}}]}`)
			if endpoint == "/v1/chat/completions" {
				body = []byte(`{"model":"claude-opus-5-5","messages":[
					{"role":"user","content":"look up the context"},
					{"role":"assistant","tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{}"}}]},
					{"role":"tool","tool_call_id":"call_1","content":"stable tool result"}
				],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object"}}}]}`)
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(body)))
			var err error
			if endpoint == "/v1/responses" {
				_, err = svc.Forward(context.Background(), c, account, body)
			} else {
				_, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
			}
			require.NoError(t, err)
			messages := gjson.GetBytes(upstream.lastBody, "messages").Array()
			last := messages[len(messages)-1].Get("content.0")
			require.Equal(t, "tool_result", last.Get("type").String())
			require.Equal(t, "stable tool result", last.Get("content").String())
			require.Equal(t, "5m", last.Get("cache_control.ttl").String())
		})
	}
}

func TestOpenAIAnthropicNonClaudeCacheUnchanged(t *testing.T) {
	for _, endpoint := range []string{"/v1/responses", "/v1/chat/completions"} {
		t.Run(endpoint, func(t *testing.T) {
			cfg := rawChatCompletionsTestConfig()
			upstream := &httpUpstreamRecorder{resp: nativeAnthropicEventsResponse(nativeAnthropicMessageStart, nativeAnthropicTextStart, nativeAnthropicTextDelta, nativeAnthropicTextStop, nativeAnthropicTerminalDelta, nativeAnthropicMessageStop)}
			svc := &OpenAIGatewayService{cfg: cfg}
			svc.SetAnthropicGateway(&GatewayService{cfg: cfg, httpUpstream: upstream, tlsFPProfileService: &TLSFingerprintProfileService{}})
			account := &Account{ID: 86, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "test-key"}}
			account.Credentials["model_mapping"] = map[string]any{"project-model": "k3"}
			body := []byte(`{"model":"project-model","input":"hello"}`)
			if endpoint == "/v1/chat/completions" {
				body = []byte(`{"model":"project-model","messages":[{"role":"user","content":"hello"}]}`)
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, endpoint, strings.NewReader(string(body)))
			var err error
			if endpoint == "/v1/responses" {
				_, err = svc.Forward(context.Background(), c, account, body)
			} else {
				_, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
			}
			require.NoError(t, err)
			require.Equal(t, "k3", gjson.GetBytes(upstream.lastBody, "model").String())
			invalid, messages, tools, system := collectCacheControlPaths(upstream.lastBody)
			require.Empty(t, invalid)
			require.Empty(t, messages)
			require.Empty(t, tools)
			require.Empty(t, system)
		})
	}
}
