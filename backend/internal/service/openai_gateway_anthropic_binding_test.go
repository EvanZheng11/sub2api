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

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// 复现 OpenAI 分组绑定 Anthropic OAuth 账号仍报 model_not_found 的真实选号路径。
func TestOpenAIGatewayBinding_SelectAnthropicClaude(t *testing.T) {
	groupID := int64(3)
	account := Account{
		ID: 86, Platform: PlatformAnthropic, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, Concurrency: 1,
		AccountGroups: []AccountGroup{{GroupID: groupID}},
		Credentials:   map[string]any{"model_mapping": map[string]any{"claude-sonnet-5-5": "claude-sonnet-5-5"}},
	}
	repo := openAIBindingAccountRepo{schedulerTestOpenAIAccountRepo{accounts: []Account{account}}}
	svc := &OpenAIGatewayService{
		accountRepo: repo, cfg: &config.Config{},
		concurrencyService: NewConcurrencyService(&schedulerTestConcurrencyCache{}),
		schedulerSnapshot: &SchedulerSnapshotService{
			accountRepo: repo, cfg: &config.Config{},
			cache: &openAISnapshotCacheStub{
				snapshotAccounts: []*Account{&account},
				accountsByID:     map[int64]*Account{86: &account},
			},
		},
	}
	scheduler := &defaultOpenAIAccountScheduler{service: svc}
	selection, _, _, _, err := scheduler.selectByLoadBalance(context.Background(), OpenAIAccountScheduleRequest{
		GroupID: &groupID, Platform: PlatformOpenAI,
		RequestedModel: "claude-sonnet-5-5", RequiredTransport: OpenAIUpstreamTransportHTTPSSE,
	})
	require.NoError(t, err)
	require.NotNil(t, selection)
	require.Equal(t, account.ID, selection.Account.ID)
	selection.ReleaseFunc()
	for _, mode := range []string{"旧选号", "旧负载均衡", "新粘性会话", "无快照回源"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			if mode == "旧选号" {
				selected, err := svc.SelectAccountForModel(ctx, &groupID, "", "claude-sonnet-5-5")
				require.NoError(t, err)
				require.Equal(t, account.ID, selected.ID)
				return
			}
			var selected *AccountSelectionResult
			var err error
			if mode == "新粘性会话" {
				svc.cache = &schedulerTestGatewayCache{sessionBindings: map[string]int64{"session": 86}}
				selected, _, err = scheduler.selectBySessionHash(ctx, OpenAIAccountScheduleRequest{
					GroupID: &groupID, Platform: PlatformOpenAI, RequestedModel: "claude-sonnet-5-5",
					SessionHash: "session", StickyAccountID: 86, RequiredTransport: OpenAIUpstreamTransportHTTPSSE,
				})
			} else {
				if mode == "无快照回源" {
					svc.schedulerSnapshot = nil
				}
				selected, err = svc.SelectAccountWithLoadAwareness(ctx, &groupID, "", "claude-sonnet-5-5", nil)
			}
			require.NoError(t, err)
			require.NotNil(t, selected)
			require.Equal(t, account.ID, selected.Account.ID)
			selected.ReleaseFunc()
		})
	}
}

func TestOpenAIAnthropicBridgeForward(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, accountType := range []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeAPIKey} {
		for _, path := range []string{"/v1/responses", "/v1/chat/completions"} {
			for _, stream := range []bool{false, true} {
				t.Run(accountType+path+map[bool]string{false: "/json", true: "/sse"}[stream], func(t *testing.T) {
					account := &Account{
						ID: 86, Platform: PlatformAnthropic, Type: accountType,
						Credentials: map[string]any{"access_token": "test-claude-token", "api_key": "test-claude-key"},
					}
					upstream := &httpUpstreamRecorder{resp: nativeAnthropicEventsResponse(
						nativeAnthropicMessageStart, nativeAnthropicTextStart, nativeAnthropicTextDelta,
						nativeAnthropicTextStop, nativeAnthropicTerminalDelta, nativeAnthropicMessageStop,
					)}
					cfg := rawChatCompletionsTestConfig()
					svc := &OpenAIGatewayService{cfg: cfg, httpUpstream: upstream}
					svc.SetAnthropicGateway(&GatewayService{cfg: cfg, httpUpstream: upstream, tlsFPProfileService: &TLSFingerprintProfileService{}})
					payload := map[string]any{"model": "claude-sonnet-5-5", "stream": stream}
					if path == "/v1/responses" {
						payload["input"] = "hi"
					} else {
						payload["messages"] = []any{map[string]any{"role": "user", "content": "hi"}}
					}
					body, err := json.Marshal(payload)
					require.NoError(t, err)
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
					c.Request.Header.Set("Authorization", "Bearer downstream-key-not-for-upstream")
					c.Request.Header.Set("User-Agent", "opencode/test")
					var result *OpenAIForwardResult
					if path == "/v1/responses" {
						result, err = svc.Forward(context.Background(), c, account, body)
					} else {
						result, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
					}
					require.NoError(t, err)
					require.NotNil(t, result)
					require.Equal(t, "/v1/messages", upstream.lastReq.URL.Path)
					require.Equal(t, "api.anthropic.com", upstream.lastReq.URL.Host)
					require.NotContains(t, upstream.lastReq.Header.Values("Authorization"), "Bearer downstream-key-not-for-upstream")
					if accountType == AccountTypeAPIKey {
						require.Equal(t, "test-claude-key", getHeaderRaw(upstream.lastReq.Header, "x-api-key"))
					} else {
						require.Equal(t, "Bearer test-claude-token", getHeaderRaw(upstream.lastReq.Header, "authorization"))
						require.Contains(t, getHeaderRaw(upstream.lastReq.Header, "anthropic-beta"), "oauth")
						require.Contains(t, getHeaderRaw(upstream.lastReq.Header, "user-agent"), "claude-cli/")
					}
					sent, err := io.ReadAll(upstream.lastReq.Body)
					require.NoError(t, err)
					require.Equal(t, "claude-sonnet-5-5", gjson.GetBytes(sent, "model").String())
					require.True(t, gjson.GetBytes(sent, "stream").Bool())
					_, messageCachePaths, toolCachePaths, systemCachePaths := collectCacheControlPaths(sent)
					require.NotEmpty(t, messageCachePaths, "OpenAI 兼容请求必须缓存会话历史，而非只有固定前缀")
					require.LessOrEqual(t, len(messageCachePaths)+len(toolCachePaths)+len(systemCachePaths), maxCacheControlBlocks)
					for _, path := range messageCachePaths {
						require.Equal(t, "5m", gjson.GetBytes(sent, path+".ttl").String())
					}
					require.Equal(t, "/v1/messages", result.UpstreamEndpoint)
					require.Equal(t, "claude-sonnet-5-5", result.Model)
					require.Contains(t, rec.Body.String(), "first")
					if stream {
						if path == "/v1/responses" {
							require.Contains(t, rec.Body.String(), "response.completed")
						} else {
							require.Contains(t, rec.Body.String(), "[DONE]")
						}
					} else {
						require.True(t, json.Valid(rec.Body.Bytes()))
					}
				})
			}
		}
	}
}

func TestOpenAIAnthropicBindingDiagnosticsAndModels(t *testing.T) {
	groupID := int64(3)
	account := Account{ID: 86, Platform: PlatformAnthropic, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, AccountGroups: []AccountGroup{{GroupID: groupID}},
		Credentials: map[string]any{"model_mapping": map[string]any{"claude-sonnet-5-5": "claude-sonnet-5-5"}},
	}
	repo := &mockAccountRepoForPlatform{accounts: []Account{account}}
	svc := &OpenAIGatewayService{accountRepo: repo}
	require.Equal(t, ModelAvailabilityDiagnosis{HasAccountsInPool: true, HasModelSupport: true},
		svc.DiagnoseModelAvailabilityForPlatform(context.Background(), &groupID, "claude-sonnet-5-5", PlatformOpenAI))
	gateway := &GatewayService{accountRepo: repo}
	require.Contains(t, gateway.GetAvailableModels(context.Background(), &groupID, PlatformOpenAI), "claude-sonnet-5-5")
	// 线上 OAuth 账号没有模型映射：仍公布 Claude 目录，但不能抢走 GPT 请求。
	account.Credentials = nil
	repo.accounts = []Account{account}
	require.Contains(t, gateway.GetAvailableModels(context.Background(), &groupID, PlatformOpenAI), "claude-sonnet-5-5")
	require.Equal(t, ModelAvailabilityDiagnosis{HasAccountsInPool: true},
		svc.DiagnoseModelAvailabilityForPlatform(context.Background(), &groupID, "gpt-5.4", PlatformOpenAI))
	require.Equal(t, "model_not_supported", openAICompatibleAccountEligibilityFailureReason(context.Background(), &account, PlatformOpenAI, "gpt-5.4", false, OpenAIEndpointCapabilityChatCompletions))
	manifest, configured, err := svc.BuildGroupConfiguredCodexModelsManifest(context.Background(), &Group{ID: groupID, Platform: PlatformOpenAI}, "")
	require.NoError(t, err)
	require.True(t, configured)
	require.Contains(t, string(manifest.Body), "claude-sonnet-5-5")
}

func TestOpenAIAnthropicBindingCapabilities(t *testing.T) {
	for _, kind := range []string{AccountTypeOAuth, AccountTypeSetupToken, AccountTypeAPIKey} {
		t.Run(kind, func(t *testing.T) {
			account := &Account{Platform: PlatformAnthropic, Type: kind}
			require.True(t, BindingCapabilityAllows(PlatformOpenAI, account))
			require.True(t, account.SupportsOpenAIEndpointCapability(OpenAIEndpointCapabilityChatCompletions))
			// 协议转换只支持文本/工具，不伪装成原生 Responses、compact、嵌入或媒体能力。
			for _, capability := range []OpenAIEndpointCapability{OpenAIEndpointCapabilityResponses, OpenAIEndpointCapabilityEmbeddings, OpenAIEndpointCapabilityGrokMediaGeneration, OpenAIEndpointCapabilityLive} {
				require.False(t, account.SupportsOpenAIEndpointCapability(capability))
			}
			require.Zero(t, openAICompactSupportTier(account))
			require.False(t, BindingCapabilityAllows(PlatformGrok, account))
		})
	}
	for _, kind := range []string{AccountTypeBedrock, AccountTypeServiceAccount, AccountTypeUpstream} {
		require.False(t, BindingCapabilityAllows(PlatformOpenAI, &Account{Platform: PlatformAnthropic, Type: kind}))
	}
}

type anthropicBridgeToolUpstream struct{ httpUpstreamRecorder }

func (u *anthropicBridgeToolUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	req.Body = io.NopCloser(strings.NewReader(string(body)))
	name := gjson.GetBytes(body, "tools.0.name").String()
	start, _ := json.Marshal(map[string]any{"type": "content_block_start", "index": 0,
		"content_block": map[string]any{"type": "tool_use", "id": "toolu_roundtrip", "name": name, "input": map[string]any{}},
	})
	u.resp = nativeAnthropicEventsResponse(
		`{"type":"message_start","message":{"id":"msg_tool","type":"message","role":"assistant","model":"claude-sonnet-5-5","content":[],"usage":{"input_tokens":10,"cache_creation_input_tokens":3,"cache_read_input_tokens":7}}}`,
		string(start),
		`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"city\":\"上海\"}"}}`,
		nativeAnthropicTextStop,
		`{"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":2}}`,
		nativeAnthropicMessageStop,
	)
	return u.httpUpstreamRecorder.Do(req, proxy, id, concurrency)
}

type anthropicBridgeTokenCache struct{ ClaudeTokenCache }

func (anthropicBridgeTokenCache) GetAccessToken(context.Context, string) (string, error) {
	return "fresh-provider-token", nil
}

func TestOpenAIAnthropicBridgeToolAndTokenProvider(t *testing.T) {
	for _, path := range []string{"/v1/responses", "/v1/chat/completions"} {
		for _, stream := range []bool{false, true} {
			t.Run(path+map[bool]string{true: "/sse", false: "/json"}[stream], func(t *testing.T) {
				upstream := &anthropicBridgeToolUpstream{}
				cfg := rawChatCompletionsTestConfig()
				svc := &OpenAIGatewayService{cfg: cfg}
				svc.SetAnthropicGateway(&GatewayService{cfg: cfg, httpUpstream: upstream,
					tlsFPProfileService: &TLSFingerprintProfileService{},
					claudeTokenProvider: NewClaudeTokenProvider(nil, anthropicBridgeTokenCache{}, nil),
				})
				account := &Account{ID: 86, Platform: PlatformAnthropic, Type: AccountTypeOAuth,
					Credentials: map[string]any{"access_token": "stale-token"},
				}
				payload := map[string]any{"model": "claude-sonnet-5-5", "stream": stream}
				function := map[string]any{"name": "lookup_weather", "description": "查询天气", "parameters": map[string]any{"type": "object"}}
				if path == "/v1/responses" {
					payload["input"] = "天气如何"
					payload["tools"] = []any{map[string]any{"type": "function", "name": function["name"], "parameters": function["parameters"]}}
				} else {
					payload["messages"] = []any{map[string]any{"role": "user", "content": "天气如何"}}
					payload["tools"] = []any{map[string]any{"type": "function", "function": function}}
				}
				body, err := json.Marshal(payload)
				require.NoError(t, err)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
				var result *OpenAIForwardResult
				if path == "/v1/responses" {
					result, err = svc.Forward(context.Background(), c, account, body)
				} else {
					result, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
				}
				require.NoError(t, err)
				require.Equal(t, "Bearer fresh-provider-token", getHeaderRaw(upstream.lastReq.Header, "authorization"))
				require.Contains(t, rec.Body.String(), "lookup_weather")
				require.Contains(t, rec.Body.String(), "上海")
				// OpenAI input_tokens 含缓存部分；Anthropic input_tokens 仅为未缓存部分。
				require.Equal(t, 20, result.Usage.InputTokens)
				require.Equal(t, 2, result.Usage.OutputTokens)
				require.Equal(t, 3, result.Usage.CacheCreationInputTokens)
				require.Equal(t, 7, result.Usage.CacheReadInputTokens)
				require.Equal(t, "claude-sonnet-5-5", result.BillingModel)
			})
		}
	}
}

func TestOpenAIAnthropicBridgeRefusesMissingAdapterAndCompact(t *testing.T) {
	account := &Account{Platform: PlatformAnthropic, Type: AccountTypeOAuth}
	body := []byte(`{"model":"claude-sonnet-5-5","input":"hi"}`)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
	svc := &OpenAIGatewayService{}
	_, err := svc.Forward(context.Background(), c, account, body)
	require.Error(t, err)
	svc.SetAnthropicGateway(&GatewayService{})
	MarkOpenAINativeCompactionV2(c)
	_, err = svc.Forward(context.Background(), c, account, body)
	require.ErrorIs(t, err, ErrNoAvailableCompactAccounts)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestOpenAIAnthropicBridgeMappedAlias(t *testing.T) {
	for _, path := range []string{"/v1/responses", "/v1/chat/completions"} {
		t.Run(path, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: nativeAnthropicEventsResponse(nativeAnthropicMessageStart, nativeAnthropicTextStart, nativeAnthropicTextDelta, nativeAnthropicTextStop, nativeAnthropicTerminalDelta, nativeAnthropicMessageStop)}
			cfg := rawChatCompletionsTestConfig()
			svc := &OpenAIGatewayService{cfg: cfg}
			svc.SetAnthropicGateway(&GatewayService{cfg: cfg, httpUpstream: upstream, tlsFPProfileService: &TLSFingerprintProfileService{}})
			account := &Account{ID: 86, Platform: PlatformAnthropic, Type: AccountTypeOAuth,
				Credentials: map[string]any{"access_token": "test-token", "model_mapping": map[string]any{"project-claude": "claude-sonnet-5-5"}},
			}
			body := []byte(`{"model":"project-claude","input":"hi"}`)
			if path == "/v1/chat/completions" {
				body = []byte(`{"model":"project-claude","messages":[{"role":"user","content":"hi"}]}`)
			}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
			var result *OpenAIForwardResult
			var err error
			if path == "/v1/responses" {
				result, err = svc.Forward(context.Background(), c, account, body)
			} else {
				result, err = svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
			}
			require.NoError(t, err)
			require.Equal(t, "claude-sonnet-5-5", gjson.GetBytes(upstream.lastBody, "model").String())
			require.Equal(t, "project-claude", result.Model)
			require.Equal(t, "claude-sonnet-5-5", result.BillingModel)
			require.Contains(t, rec.Body.String(), `"model":"project-claude"`)
		})
	}
}
