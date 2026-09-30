//go:build unit

package service

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type opencodeLocalUpstream struct {
	HTTPUpstream
	server *httptest.Server
}

func (u *opencodeLocalUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	// 只允许当前测试服务器，错误 URL 不能触发真实网络请求。
	if req.URL.Scheme+"://"+req.URL.Host != u.server.URL {
		return nil, fmt.Errorf("测试禁止连接非本地上游")
	}
	return u.server.Client().Do(req)
}

func opencodeAdapterTestContext(path string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	return c, recorder
}

func opencodeAdapterResponse(status int, contentType, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{contentType}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestEnsureOpenCodeGoForwardCredentials(t *testing.T) {
	require.NoError(t, ensureOpenCodeGoForwardCredentials(openCodeMappedTestAccount()))

	noKey := openCodeMappedTestAccount()
	noKey.Credentials["api_key"] = ""
	err := ensureOpenCodeGoForwardCredentials(noKey)
	require.Error(t, err)
	require.Contains(t, err.Error(), "api_key")
	require.NotContains(t, err.Error(), "sk-opencode")

	noURL := openCodeMappedTestAccount()
	delete(noURL.Credentials, "base_url")
	delete(noURL.Credentials, "api_base_urls")
	err = ensureOpenCodeGoForwardCredentials(noURL)
	require.Error(t, err)
	require.Contains(t, err.Error(), "base_url")

	oauth := openCodeMappedTestAccount()
	oauth.Type = AccountTypeOAuth
	require.Error(t, ensureOpenCodeGoForwardCredentials(oauth))
}

func TestForwardOpenCodeGoFromResponsesDispatchesNativeEndpoints(t *testing.T) {
	tests := []struct {
		name      string
		protocol  string
		wantURL   string
		wantModel string
	}{
		{
			name:      "Anthropic 原生协议",
			protocol:  APIProtocolAnthropic,
			wantURL:   "https://opencode.ai/zen/v1/messages",
			wantModel: "glm-5.3",
		},
		{
			name:      "Chat Completions 原生协议",
			protocol:  APIProtocolChatCompletions,
			wantURL:   "https://opencode.ai/zen/v1/chat/completions",
			wantModel: "glm-5.3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := openCodeMappedTestAccount()
			account.Credentials["api_protocol"] = tt.protocol
			body := []byte(`{"model":"opencode/glm-5.3","stream":false,"input":"hi"}`)
			response := opencodeAdapterResponse(
				http.StatusOK,
				"application/json",
				`{"id":"resp_oc","object":"response","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`,
			)
			if tt.protocol == APIProtocolAnthropic {
				response = nativeAnthropicStreamResponse()
			}
			upstream := &httpUpstreamRecorder{
				resp: response,
			}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			c, _ := opencodeAdapterTestContext("/v1/responses", body)

			result, err := svc.forwardOpenCodeGoFromResponses(context.Background(), c, account, body)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, tt.wantURL, upstream.lastReq.URL.String())
			require.Equal(t, tt.wantModel, gjson.GetBytes(upstream.lastBody, "model").String())
			if tt.protocol == APIProtocolAnthropic {
				require.Equal(t, "sk-opencode", upstream.lastReq.Header.Get("x-api-key"))
			} else {
				require.Equal(t, "Bearer sk-opencode", upstream.lastReq.Header.Get("Authorization"))
			}
		})
	}
}

func TestOpenCodeAdapterConfiguredURLs(t *testing.T) {
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolResponses, APIProtocolAnthropic} {
		for _, pinned := range []bool{false, true} {
			for _, protocolURL := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/固定=%v/分协议地址=%v", protocol, pinned, protocolURL), func(t *testing.T) {
					account := openCodeMappedTestAccount()
					account.Credentials["base_url"] = "https://fallback.example/v1"
					delete(account.Credentials, "api_base_urls")
					if pinned {
						account.Credentials["api_protocol"] = protocol
					} else {
						account.Credentials["protocol_rules"] = []any{map[string]any{"pattern": "*", "protocol": protocol}}
					}
					base := "https://fallback.example/v1"
					if protocolURL {
						base = "https://selected.example/v1"
						account.Credentials["api_base_urls"] = map[string]any{protocol: base}
					}
					for _, path := range []string{"/v1/chat/completions", "/v1/responses"} {
						body := opencodeAdapterToolRequest(path, false)
						upstream := &httpUpstreamRecorder{resp: opencodeAdapterResponse(503, "application/json", `{"error":{"message":"临时不可用"}}`)}
						svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
						c, rec := opencodeAdapterTestContext(path, body)
						_, err := opencodeAdapterForward(svc, c, account, path, body)
						var failover *UpstreamFailoverError
						require.ErrorAs(t, err, &failover)
						require.False(t, c.Writer.Written())
						require.Empty(t, rec.Body.String())
						endpoint := map[string]string{
							APIProtocolChatCompletions: "/chat/completions",
							APIProtocolResponses:       "/responses",
							APIProtocolAnthropic:       "/messages",
						}[protocol]
						require.Equal(t, base+endpoint, upstream.lastReq.URL.String())
					}
				})
			}
		}
	}
}

func opencodeAdapterToolRequest(path string, stream bool) []byte {
	if path == "/v1/chat/completions" {
		return []byte(fmt.Sprintf(`{"model":"opencode/public","stream":%v,"messages":[{"role":"user","content":"查询"}],"tools":[{"type":"function","function":{"name":"lookup","parameters":{"type":"object","properties":{}}}}],"tool_choice":"auto"}`, stream))
	}
	return []byte(fmt.Sprintf(`{"model":"opencode/public","stream":%v,"input":"查询","tools":[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{}}}],"tool_choice":"auto"}`, stream))
}

func opencodeAdapterForward(svc *OpenAIGatewayService, c *gin.Context, account *Account, path string, body []byte) (*OpenAIForwardResult, error) {
	if path == "/v1/chat/completions" {
		return svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
	}
	return svc.Forward(context.Background(), c, account, body)
}

func opencodeAdapterToolResponse(protocol string, stream bool) (string, string) {
	switch protocol {
	case APIProtocolChatCompletions:
		if !stream {
			return "application/json", `{"id":"chat_oc","object":"chat.completion","model":"native-model","choices":[{"index":0,"message":{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"key\":\"a\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`
		}
		return "text/event-stream", strings.Join([]string{
			`data: {"id":"chat_oc","object":"chat.completion.chunk","model":"native-model","choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":""}}]},"finish_reason":null}]}`,
			`data: {"id":"chat_oc","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"key\":"}}]},"finish_reason":null}]}`,
			`data: {"id":"chat_oc","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"a\"}"}}]},"finish_reason":"tool_calls"}]}`,
			`data: {"id":"chat_oc","choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`,
			`data: [DONE]`,
		}, "\n\n") + "\n\n"
	case APIProtocolAnthropic:
		// 原生 Anthropic 桥接总是读取 SSE，非流式入站由既有缓冲链组装。
		return "text/event-stream", strings.Join([]string{
			"event: message_start\n" + `data: {"type":"message_start","message":{"id":"msg_oc","type":"message","role":"assistant","model":"native-model","content":[],"usage":{"input_tokens":3,"output_tokens":0}}}`,
			"event: content_block_start\n" + `data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_1","name":"lookup","input":{}}}`,
			"event: content_block_delta\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"key\":"}}`,
			"event: content_block_delta\n" + `data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"\"a\"}"}}`,
			"event: content_block_stop\n" + `data: {"type":"content_block_stop","index":0}`,
			"event: message_delta\n" + `data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":2}}`,
			"event: message_stop\n" + `data: {"type":"message_stop"}`,
		}, "\n\n") + "\n\n"
	default:
		terminal := `{"id":"resp_oc","object":"response","model":"native-model","status":"completed","output":[{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"key\":\"a\"}","status":"completed"}],"usage":{"input_tokens":3,"output_tokens":2,"total_tokens":5}}`
		if !stream {
			return "application/json", terminal
		}
		return "text/event-stream", strings.Join([]string{
			`data: {"type":"response.created","response":{"id":"resp_oc","model":"native-model","status":"in_progress","output":[]}}`,
			`data: {"type":"response.output_item.added","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":"","status":"in_progress"}}`,
			`data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"{\"key\":"}`,
			`data: {"type":"response.function_call_arguments.delta","output_index":0,"item_id":"fc_1","delta":"\"a\"}"}`,
			`data: {"type":"response.function_call_arguments.done","output_index":0,"item_id":"fc_1","arguments":"{\"key\":\"a\"}"}`,
			`data: {"type":"response.output_item.done","output_index":0,"item":{"id":"fc_1","type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"key\":\"a\"}","status":"completed"}}`,
			`data: {"type":"response.completed","response":` + terminal + `}`,
		}, "\n\n") + "\n\n"
	}
}

func TestOpenCodeAdapterToolCalls(t *testing.T) {
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolResponses, APIProtocolAnthropic} {
		for _, path := range []string{"/v1/chat/completions", "/v1/responses"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/流式=%v", protocol, path, stream), func(t *testing.T) {
					contentType, responseBody := opencodeAdapterToolResponse(protocol, stream || path == "/v1/chat/completions" && protocol == APIProtocolResponses)
					type capturedRequest struct {
						body   []byte
						path   string
						header http.Header
					}
					requests := make(chan capturedRequest, 1)
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						requestBody, _ := io.ReadAll(r.Body)
						requests <- capturedRequest{body: requestBody, path: r.URL.Path, header: r.Header.Clone()}
						w.Header().Set("Content-Type", contentType)
						_, _ = io.WriteString(w, responseBody)
					}))
					defer server.Close()
					account := openCodeMappedTestAccount()
					account.Credentials["base_url"] = "https://unused.example/v1"
					account.Credentials["api_base_urls"] = map[string]any{protocol: server.URL + "/native/v1"}
					account.Credentials["model_mapping"] = map[string]any{"public": "native-model"}
					account.Credentials["protocol_rules"] = []any{map[string]any{"pattern": "native-*", "protocol": protocol}}
					// 探测结果不得覆盖模型协议规则。
					account.Extra = map[string]any{"openai_responses_supported": false}
					svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: &opencodeLocalUpstream{server: server}}
					body := opencodeAdapterToolRequest(path, stream)
					c, rec := opencodeAdapterTestContext(path, body)
					c.Request.Header.Set("Authorization", "Bearer 客户端凭据")
					c.Request.Header.Set("x-api-key", "客户端凭据")

					result, err := opencodeAdapterForward(svc, c, account, path, body)
					require.NoError(t, err)
					captured := <-requests
					capturedBody, capturedPath, capturedHeader := captured.body, captured.path, captured.header
					require.NotNil(t, result)
					require.Equal(t, 3, result.Usage.InputTokens)
					require.Equal(t, 2, result.Usage.OutputTokens)
					require.Equal(t, "native-model", result.UpstreamModel)
					require.Equal(t, stream, result.Stream)
					endpoint := map[string]string{
						APIProtocolChatCompletions: "/v1/chat/completions",
						APIProtocolResponses:       "/v1/responses",
						APIProtocolAnthropic:       "/v1/messages",
					}[protocol]
					require.Equal(t, "/native"+endpoint, capturedPath)
					require.Equal(t, "native-model", gjson.GetBytes(capturedBody, "model").String())
					if protocol == APIProtocolAnthropic {
						require.Equal(t, "sk-opencode", capturedHeader.Get("x-api-key"))
						require.Empty(t, capturedHeader.Get("Authorization"))
						require.Equal(t, "lookup", gjson.GetBytes(capturedBody, "tools.0.name").String())
					} else {
						require.Equal(t, "Bearer sk-opencode", capturedHeader.Get("Authorization"))
						require.Empty(t, capturedHeader.Get("x-api-key"))
						toolNamePath := "tools.0.name"
						if protocol == APIProtocolChatCompletions {
							toolNamePath = "tools.0.function.name"
						}
						require.Equal(t, "lookup", gjson.GetBytes(capturedBody, toolNamePath).String())
					}
					require.NotContains(t, string(capturedBody), "opencode/")
					if stream {
						require.Contains(t, rec.Body.String(), "call_1")
						require.Contains(t, rec.Body.String(), "lookup")
						if path == "/v1/chat/completions" {
							require.Contains(t, rec.Body.String(), `"tool_calls"`)
							require.Contains(t, rec.Body.String(), "data: [DONE]")
							var arguments strings.Builder
							for _, line := range strings.Split(rec.Body.String(), "\n") {
								if payload, ok := extractOpenAISSEDataLine(line); ok {
									for _, tool := range gjson.Get(payload, "choices.0.delta.tool_calls").Array() {
										arguments.WriteString(tool.Get("function.arguments").String())
									}
								}
							}
							require.JSONEq(t, `{"key":"a"}`, arguments.String())
						} else {
							require.Contains(t, rec.Body.String(), "response.completed")
							require.Contains(t, rec.Body.String(), "function_call")
							var arguments string
							for _, line := range strings.Split(rec.Body.String(), "\n") {
								if payload, ok := extractOpenAISSEDataLine(line); ok {
									event := gjson.Parse(payload)
									if event.Get("type").String() == "response.output_item.done" {
										arguments = event.Get("item.arguments").String()
									}
								}
							}
							require.JSONEq(t, `{"key":"a"}`, arguments)
						}
					} else {
						callPath := "output.0"
						if path == "/v1/chat/completions" {
							callPath = "choices.0.message.tool_calls.0"
							require.Equal(t, "tool_calls", gjson.Get(rec.Body.String(), "choices.0.finish_reason").String())
							require.Equal(t, "call_1", gjson.Get(rec.Body.String(), callPath+".id").String())
							callPath += ".function"
						} else {
							require.Equal(t, "call_1", gjson.Get(rec.Body.String(), callPath+".call_id").String())
						}
						require.Equal(t, "lookup", gjson.Get(rec.Body.String(), callPath+".name").String())
						require.JSONEq(t, `{"key":"a"}`, gjson.Get(rec.Body.String(), callPath+".arguments").String())
					}
				})
			}
		}
	}
}

func TestOpenCodeAdapterInvalidConfigurationDoesNotSend(t *testing.T) {
	originalLogger := slog.Default()
	var logs bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(originalLogger)
	for _, path := range []string{"/v1/chat/completions", "/v1/responses"} {
		for _, reason := range []string{"缺密钥", "缺地址", "缺目标协议地址", "地址含凭据", "地址无效", "协议地址无效", "非APIKey", "类型错误的地址"} {
			t.Run(path+"/"+reason, func(t *testing.T) {
				account := openCodeMappedTestAccount()
				account.Credentials["api_protocol"] = APIProtocolResponses
				account.Credentials["api_key"] = "测试密钥禁止泄露"
				switch reason {
				case "缺密钥":
					account.Credentials["api_key"] = "  "
				case "缺地址":
					delete(account.Credentials, "base_url")
					delete(account.Credentials, "api_base_urls")
				case "缺目标协议地址":
					delete(account.Credentials, "base_url")
					account.Credentials["api_base_urls"] = map[string]any{APIProtocolChatCompletions: "https://unused.example"}
				case "地址含凭据":
					account.Credentials["api_base_urls"] = map[string]any{APIProtocolResponses: "https://user:地址密钥禁止泄露@upstream.example/v1?api_key=查询密钥禁止泄露"}
				case "地址无效":
					account.Credentials["base_url"] = "://地址密钥禁止泄露"
					delete(account.Credentials, "api_base_urls")
				case "协议地址无效":
					account.Credentials["api_base_urls"] = map[string]any{APIProtocolResponses: "://地址密钥禁止泄露"}
				case "非APIKey":
					account.Type = AccountTypeOAuth
				case "类型错误的地址":
					delete(account.Credentials, "base_url")
					account.Credentials["api_base_urls"] = map[string]any{APIProtocolResponses: 42}
				}
				upstream := &httpUpstreamRecorder{}
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
				body := opencodeAdapterToolRequest(path, true)
				c, rec := opencodeAdapterTestContext(path, body)
				result, err := opencodeAdapterForward(svc, c, account, path, body)
				require.Error(t, err)
				require.Nil(t, result)
				require.Nil(t, upstream.lastReq)
				require.False(t, c.Writer.Written())
				require.Empty(t, rec.Body.String())
				require.Contains(t, err.Error(), "OpenCode")
				for _, secret := range []string{"测试密钥禁止泄露", "地址密钥禁止泄露", "查询密钥禁止泄露"} {
					require.NotContains(t, err.Error(), secret)
					require.NotContains(t, logs.String(), secret)
				}
			})
		}
	}
	require.Contains(t, logs.String(), "OpenCode 转发")
}

func TestOpenCodeAdapterFailsOverBeforeFirstPacket(t *testing.T) {
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolResponses, APIProtocolAnthropic} {
		for _, path := range []string{"/v1/chat/completions", "/v1/responses"} {
			t.Run(protocol+"/"+path, func(t *testing.T) {
				keys := make(chan string, 2)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					key := r.Header.Get("Authorization")
					if protocol == APIProtocolAnthropic {
						key = r.Header.Get("x-api-key")
					}
					keys <- key
					if strings.Contains(key, "failed-fixture") {
						w.Header().Set("Content-Type", "text/plain")
						w.WriteHeader(http.StatusServiceUnavailable)
						_, _ = io.WriteString(w, "临时不可用")
						return
					}
					contentType, response := opencodeAdapterToolResponse(protocol, true)
					w.Header().Set("Content-Type", contentType)
					_, _ = io.WriteString(w, response)
				}))
				defer server.Close()
				account := openCodeMappedTestAccount()
				account.Credentials["api_key"] = "failed-fixture"
				account.Credentials["api_protocol"] = protocol
				account.Credentials["api_base_urls"] = map[string]any{protocol: server.URL + "/v1"}
				account.Credentials["model_mapping"] = map[string]any{"public": "native-model"}
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: &opencodeLocalUpstream{server: server}}
				body := opencodeAdapterToolRequest(path, true)
				c, rec := opencodeAdapterTestContext(path, body)
				_, err := opencodeAdapterForward(svc, c, account, path, body)
				var failover *UpstreamFailoverError
				require.ErrorAs(t, err, &failover)
				require.Equal(t, http.StatusServiceUnavailable, failover.StatusCode)
				require.True(t, failover.ShouldRetryNextAccount())
				require.False(t, c.Writer.Written())
				require.Empty(t, rec.Body.String())
				// 使用同一客户端上下文模拟 handler 已有的下一账号转发。
				healthy := openCodeMappedTestAccount()
				healthy.ID = account.ID + 1
				healthy.Credentials["api_key"] = "healthy-fixture"
				healthy.Credentials["api_protocol"] = protocol
				healthy.Credentials["api_base_urls"] = account.Credentials["api_base_urls"]
				healthy.Credentials["model_mapping"] = account.Credentials["model_mapping"]
				result, err := opencodeAdapterForward(svc, c, healthy, path, body)
				require.NoError(t, err)
				require.NotNil(t, result)
				require.Contains(t, rec.Body.String(), "lookup")
				require.NotContains(t, rec.Body.String(), "临时不可用")
				require.Len(t, keys, 2)
				require.Contains(t, <-keys, "failed-fixture")
				require.Contains(t, <-keys, "healthy-fixture")
			})
		}
	}
}

func TestOpenCodeAdapterModelMappingPriority(t *testing.T) {
	account := openCodeMappedTestAccount()
	account.Credentials["model_mapping"] = map[string]any{
		"opencode/public": "opencode-go/explicit-model",
		"public":          "opencode_go/normalized-model",
		"other*":          "mapped-wildcard",
	}
	for _, tt := range []struct {
		model string
		want  string
	}{
		{"opencode/public", "explicit-model"},
		{"opencode-go/public", "normalized-model"},
		{"opencode_go/public", "normalized-model"},
		{"opencode/other-model", "mapped-wildcard"},
		{"opencode/GLM-5.3", "glm-5.3"},
	} {
		t.Run(tt.model, func(t *testing.T) {
			require.Equal(t, tt.want, resolveOpenAIForwardModel(account, tt.model, "不应使用的分组模型"))
		})
	}
}

func TestOpenCodeAdapterParallelToolOutputs(t *testing.T) {
	for _, protocol := range []string{APIProtocolChatCompletions, APIProtocolResponses, APIProtocolAnthropic} {
		for _, path := range []string{"/v1/chat/completions", "/v1/responses"} {
			t.Run(protocol+"/"+path, func(t *testing.T) {
				body := []byte(`{"model":"opencode/public","stream":false,"input":[{"type":"function_call","call_id":"call_1","name":"lookup","arguments":"{\"key\":\"a\"}"},{"type":"function_call","call_id":"call_2","name":"lookup","arguments":"{\"key\":\"b\"}"},{"type":"function_call_output","call_id":"call_1","output":"结果a"},{"type":"function_call_output","call_id":"call_2","output":"结果b"}]}`)
				if path == "/v1/chat/completions" {
					body = []byte(`{"model":"opencode/public","stream":false,"messages":[{"role":"assistant","content":null,"tool_calls":[{"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"key\":\"a\"}"}},{"id":"call_2","type":"function","function":{"name":"lookup","arguments":"{\"key\":\"b\"}"}}]},{"role":"tool","tool_call_id":"call_1","content":"结果a"},{"role":"tool","tool_call_id":"call_2","content":"结果b"}]}`)
				}
				requests := make(chan []byte, 1)
				contentType, response := opencodeAdapterToolResponse(protocol, path == "/v1/chat/completions" && protocol == APIProtocolResponses)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					captured, _ := io.ReadAll(r.Body)
					requests <- captured
					w.Header().Set("Content-Type", contentType)
					_, _ = io.WriteString(w, response)
				}))
				defer server.Close()
				account := openCodeMappedTestAccount()
				account.Credentials["api_protocol"] = protocol
				account.Credentials["api_base_urls"] = map[string]any{protocol: server.URL + "/v1"}
				account.Credentials["model_mapping"] = map[string]any{"public": "native-model"}
				svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: &opencodeLocalUpstream{server: server}}
				c, _ := opencodeAdapterTestContext(path, body)
				_, err := opencodeAdapterForward(svc, c, account, path, body)
				require.NoError(t, err)
				captured := <-requests
				require.Contains(t, string(captured), "结果a")
				require.Contains(t, string(captured), "结果b")
				switch protocol {
				case APIProtocolChatCompletions:
					require.Len(t, gjson.GetBytes(captured, "messages.0.tool_calls").Array(), 2)
					require.Equal(t, "call_1", gjson.GetBytes(captured, "messages.1.tool_call_id").String())
					require.Equal(t, "call_2", gjson.GetBytes(captured, "messages.2.tool_call_id").String())
				case APIProtocolResponses:
					require.Equal(t, "function_call_output", gjson.GetBytes(captured, "input.2.type").String())
					require.Equal(t, "call_1", gjson.GetBytes(captured, "input.2.call_id").String())
					require.Equal(t, "call_2", gjson.GetBytes(captured, "input.3.call_id").String())
				case APIProtocolAnthropic:
					require.Equal(t, "tool_use", gjson.GetBytes(captured, "messages.0.content.0.type").String())
					require.Equal(t, "call_1", gjson.GetBytes(captured, "messages.1.content.0.tool_use_id").String())
					require.Equal(t, "call_2", gjson.GetBytes(captured, "messages.1.content.1.tool_use_id").String())
				}
			})
		}
	}
}

func TestOpenCodeAdapterResponsesStreamErrorBeforeOutput(t *testing.T) {
	for _, path := range []string{"/v1/chat/completions", "/v1/responses"} {
		t.Run(path, func(t *testing.T) {
			account := openCodeMappedTestAccount()
			account.Credentials["api_protocol"] = APIProtocolResponses
			body := opencodeAdapterToolRequest(path, true)
			c, rec := opencodeAdapterTestContext(path, body)
			upstream := &httpUpstreamRecorder{resp: opencodeAdapterResponse(200, "text/event-stream",
				`data: {"type":"response.failed","response":{"id":"resp_failed","status":"failed","error":{"type":"server_error","code":"server_is_overloaded","message":"临时不可用"},"output":[]}}`+"\n\n")}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			_, err := opencodeAdapterForward(svc, c, account, path, body)
			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover)
			require.False(t, c.Writer.Written())
			require.Empty(t, rec.Body.String())
		})
	}
}

func TestForwardOpenCodeGoFromResponsesKeepsNativeResponsesOnCommonPath(t *testing.T) {
	account := openCodeMappedTestAccount()
	account.Credentials["api_protocol"] = APIProtocolResponses
	body := []byte(`{"model":"opencode/glm-5.3","stream":false,"input":"hi"}`)
	c, _ := opencodeAdapterTestContext("/v1/responses", body)

	result, err := (&OpenAIGatewayService{}).forwardOpenCodeGoFromResponses(context.Background(), c, account, body)
	require.NoError(t, err)
	require.Nil(t, result)
}
