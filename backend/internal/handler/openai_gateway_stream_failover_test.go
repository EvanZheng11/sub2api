//go:build unit

package handler

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai_compat"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type responsesStreamFailoverUpstream struct {
	service.HTTPUpstream
	mu         sync.Mutex
	responses  []*http.Response
	accountIDs []int64
}

func (u *responsesStreamFailoverUpstream) Do(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.accountIDs = append(u.accountIDs, accountID)
	if len(u.responses) == 0 {
		return nil, io.EOF
	}
	resp := u.responses[0]
	u.responses = u.responses[1:]
	return resp, nil
}

func (u *responsesStreamFailoverUpstream) calls() []int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]int64(nil), u.accountIDs...)
}

func newResponsesStreamFailoverHandler(t *testing.T, upstream service.HTTPUpstream) *OpenAIGatewayHandler {
	t.Helper()
	accounts := []service.Account{
		{
			ID: 1, Name: "responses-stream-1", Platform: service.PlatformOpenAI,
			Type: service.AccountTypeAPIKey, Status: service.StatusActive,
			Schedulable: true, Priority: 0,
			Credentials: map[string]any{
				"api_key":  "key-1",
				"base_url": "http://upstream.example",
			},
			Extra: map[string]any{
				openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceChatCompletions),
			},
		},
		{
			ID: 2, Name: "responses-stream-2", Platform: service.PlatformOpenAI,
			Type: service.AccountTypeAPIKey, Status: service.StatusActive,
			Schedulable: true, Priority: 1,
			Credentials: map[string]any{
				"api_key":  "key-2",
				"base_url": "http://upstream.example",
			},
			Extra: map[string]any{
				openai_compat.ExtraKeyResponsesMode: string(openai_compat.ResponsesSupportModeForceChatCompletions),
			},
		},
	}
	accountRepo := openAIImagesFailoverAccountRepo{accounts: accounts}
	cfg := &config.Config{
		RunMode: config.RunModeSimple,
		Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{
				AllowInsecureHTTP: true,
			},
		},
	}
	gatewayService := service.NewOpenAIGatewayService(
		accountRepo,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		cfg,
		nil,
		nil,
		nil,
		nil,
		nil,
		upstream,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	billingService := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingService.Stop)
	handler := NewOpenAIGatewayHandler(
		gatewayService,
		service.NewConcurrencyService(nil),
		billingService,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg),
		nil,
		nil,
		nil,
		nil,
		cfg,
	)
	handler.maxAccountSwitches = 10
	return handler
}

func newResponsesStreamFailoverContext(body string) (*gin.Context, *httptest.ResponseRecorder) {
	groupID := int64(3133)
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/responses",
		bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	c.Set(string(middleware2.ContextKeyAPIKey), &service.APIKey{
		ID:      100,
		GroupID: &groupID,
		Group: &service.Group{
			ID:       groupID,
			Platform: service.PlatformOpenAI,
		},
		User: &service.User{ID: 101},
	})
	c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 101})
	return c, rec
}

func emptyResponsesStreamResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader("")),
	}
}

func completeChatFallbackResponse() *http.Response {
	body := strings.Join([]string{
		`data: {"id":"chatcmpl_second","object":"chat.completion.chunk","model":"gpt-5.5","choices":[{"index":0,"delta":{"content":"second"}}]}`,
		"",
		`data: {"id":"chatcmpl_second","object":"chat.completion.chunk","model":"gpt-5.5","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func truncatedChatFallbackResponse() *http.Response {
	body := `data: {"id":"chatcmpl_first","object":"chat.completion.chunk","model":"gpt-5.5","choices":[{"index":0,"delta":{"content":"first"}}]}` + "\n\n"
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestOpenAIResponsesHandlerEmpty200FailsOverWithoutMixedStream(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &responsesStreamFailoverUpstream{
		responses: []*http.Response{emptyResponsesStreamResponse(), completeChatFallbackResponse()},
	}
	handler := newResponsesStreamFailoverHandler(t, upstream)
	c, rec := newResponsesStreamFailoverContext(`{"model":"gpt-5.5","stream":true,"input":"hello"}`)

	handler.Responses(c)

	require.Equal(t, []int64{1, 2}, upstream.calls())
	require.Equal(t, http.StatusOK, rec.Code)
	require.Contains(t, rec.Body.String(), `"delta":"second"`)
	require.NotContains(t, rec.Body.String(), `"delta":"first"`)
	require.Equal(t, 1, strings.Count(rec.Body.String(), `"delta":"second"`))
	require.Contains(t, rec.Body.String(), "data: [DONE]")
}

func TestOpenAIResponsesHandlerTruncatedAfterSemanticOutputDoesNotFailOver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &responsesStreamFailoverUpstream{
		responses: []*http.Response{truncatedChatFallbackResponse(), completeChatFallbackResponse()},
	}
	handler := newResponsesStreamFailoverHandler(t, upstream)
	c, rec := newResponsesStreamFailoverContext(`{"model":"gpt-5.5","stream":true,"input":"hello"}`)

	handler.Responses(c)

	require.Equal(t, []int64{1}, upstream.calls())
	require.Contains(t, rec.Body.String(), `"delta":"first"`)
	require.NotContains(t, rec.Body.String(), `"delta":"second"`)
	require.NotContains(t, rec.Body.String(), "data: [DONE]")
}

func TestOpenAIResponsesHandlerPreOutputEOFCanSwitchAccount(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &responsesStreamFailoverUpstream{
		responses: []*http.Response{emptyResponsesStreamResponse(), completeChatFallbackResponse()},
	}
	handler := newResponsesStreamFailoverHandler(t, upstream)
	c, rec := newResponsesStreamFailoverContext(`{"model":"gpt-5.5","stream":true,"input":"hello"}`)

	handler.Responses(c)

	require.Equal(t, []int64{1, 2}, upstream.calls())
	require.NotContains(t, rec.Body.String(), "response.failed")
	require.NotContains(t, rec.Body.String(), `"delta":"first"`)
	require.Contains(t, rec.Body.String(), `"delta":"second"`)
}

func TestOpenAIResponsesHandlerNormalFallbackStreamKeepsSingleTerminal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &responsesStreamFailoverUpstream{
		responses: []*http.Response{completeChatFallbackResponse()},
	}
	handler := newResponsesStreamFailoverHandler(t, upstream)
	c, rec := newResponsesStreamFailoverContext(`{"model":"gpt-5.5","stream":true,"input":"hello"}`)

	handler.Responses(c)

	require.Equal(t, []int64{1}, upstream.calls())
	require.Equal(t, 1, strings.Count(rec.Body.String(), "data: [DONE]"))
	require.Equal(t, 1, strings.Count(rec.Body.String(), `"delta":"second"`))
}
