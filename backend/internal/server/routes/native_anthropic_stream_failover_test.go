//go:build unit

package routes

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/handler"
	servermiddleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type nativeAnthropicRoutesAccountRepo struct {
	service.AccountRepository
	accounts []service.Account
}

func (r nativeAnthropicRoutesAccountRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	for i := range r.accounts {
		if r.accounts[i].ID == id {
			account := r.accounts[i]
			return &account, nil
		}
	}
	return nil, service.ErrNoAvailableAccounts
}

func (r nativeAnthropicRoutesAccountRepo) ListSchedulableByGroupIDAndPlatform(_ context.Context, _ int64, platform string) ([]service.Account, error) {
	return r.accountsForPlatform(platform), nil
}

func (r nativeAnthropicRoutesAccountRepo) ListSchedulableByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.accountsForPlatform(platform), nil
}

func (r nativeAnthropicRoutesAccountRepo) ListSchedulableUngroupedByPlatform(_ context.Context, platform string) ([]service.Account, error) {
	return r.accountsForPlatform(platform), nil
}

func (r nativeAnthropicRoutesAccountRepo) accountsForPlatform(platform string) []service.Account {
	out := make([]service.Account, 0, len(r.accounts))
	for _, account := range r.accounts {
		if account.Platform == platform {
			out = append(out, account)
		}
	}
	return out
}

type nativeAnthropicRoutesUpstream struct {
	service.HTTPUpstream
	mu         sync.Mutex
	responses  []*http.Response
	accountIDs []int64
}

func (u *nativeAnthropicRoutesUpstream) Do(_ *http.Request, _ string, accountID int64, _ int) (*http.Response, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.accountIDs = append(u.accountIDs, accountID)
	if len(u.responses) == 0 {
		return nil, io.EOF
	}
	response := u.responses[0]
	u.responses = u.responses[1:]
	return response, nil
}

func (u *nativeAnthropicRoutesUpstream) calls() []int64 {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]int64(nil), u.accountIDs...)
}

func nativeAnthropicRoutesResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func nativeAnthropicRoutesEmptyResponse() *http.Response {
	return nativeAnthropicRoutesResponse("")
}

func nativeAnthropicRoutesTextResponse() *http.Response {
	return nativeAnthropicRoutesResponse(strings.Join([]string{
		`event: message_start`,
		`data: {"type":"message_start","message":{"id":"msg_route","type":"message","role":"assistant","model":"k3","content":[],"stop_reason":null,"usage":{"input_tokens":1,"output_tokens":0}}}`,
		"",
		`event: content_block_start`,
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"route-ok"}}`,
		"",
		`event: content_block_stop`,
		`data: {"type":"content_block_stop","index":0}`,
		"",
		`event: message_delta`,
		`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`,
		"",
		`event: message_stop`,
		`data: {"type":"message_stop"}`,
		"",
	}, "\n"))
}

func newNativeAnthropicRoutesRouter(t *testing.T, upstream service.HTTPUpstream) *gin.Engine {
	t.Helper()
	accounts := []service.Account{
		{
			ID: 1, Name: "native-route-1", Platform: service.PlatformKimi,
			Type: service.AccountTypeAPIKey, Status: service.StatusActive,
			Schedulable: true, Priority: 0,
			Credentials: map[string]any{
				"api_key":      "key-1",
				"api_protocol": service.APIProtocolAnthropic,
				"api_base_urls": map[string]any{
					service.APIProtocolAnthropic: "http://anthropic.example",
				},
			},
		},
		{
			ID: 2, Name: "native-route-2", Platform: service.PlatformKimi,
			Type: service.AccountTypeAPIKey, Status: service.StatusActive,
			Schedulable: true, Priority: 1,
			Credentials: map[string]any{
				"api_key":      "key-2",
				"api_protocol": service.APIProtocolAnthropic,
				"api_base_urls": map[string]any{
					service.APIProtocolAnthropic: "http://anthropic.example",
				},
			},
		},
	}
	group := &service.Group{
		ID: 3134, Platform: service.PlatformKimi,
		Status: service.StatusActive, Hydrated: true,
	}
	accountRepo := nativeAnthropicRoutesAccountRepo{accounts: accounts}
	cfg := &config.Config{
		RunMode: config.RunModeSimple,
		Gateway: config.GatewayConfig{
			MaxAccountSwitches: 10,
			MaxBodySize:        1024 * 1024,
			TextMaxBodySize:    1024 * 1024,
		},
		Security: config.SecurityConfig{
			URLAllowlist: config.URLAllowlistConfig{AllowInsecureHTTP: true},
		},
	}
	gatewayService := service.NewOpenAIGatewayService(
		accountRepo,
		nil, nil, nil, nil, nil, nil,
		cfg,
		nil, nil, nil, nil, nil,
		upstream,
		nil, nil, nil, nil, nil, nil, nil, nil,
	)
	billingService := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
	t.Cleanup(billingService.Stop)
	openAIHandler := handler.NewOpenAIGatewayHandler(
		gatewayService,
		service.NewConcurrencyService(nil),
		billingService,
		service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg),
		nil, nil, nil, nil, cfg,
	)

	apiKey := &service.APIKey{
		ID: 100, UserID: 101, GroupID: &group.ID, Group: group,
		User: &service.User{ID: 101, Status: service.StatusActive},
	}
	router := gin.New()
	RegisterGatewayRoutes(
		router,
		&handler.Handlers{
			Gateway:       &handler.GatewayHandler{},
			OpenAIGateway: openAIHandler,
			AsyncImage:    handler.NewAsyncImageHandler(nil, nil),
		},
		servermiddleware.APIKeyAuthMiddleware(func(c *gin.Context) {
			c.Set(string(servermiddleware.ContextKeyAPIKey), apiKey)
			c.Set(string(servermiddleware.ContextKeyUser), servermiddleware.AuthSubject{UserID: apiKey.UserID})
			c.Next()
		}),
		nil, nil, nil, nil, nil, cfg,
	)
	return router
}

func TestNativeAnthropicRoutesHandlerFailoverDoesNotMixAccounts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name string
		path string
		body string
	}{
		{
			name: "chat_completions",
			path: "/v1/chat/completions",
			body: `{"model":"k3","stream":true,"messages":[{"role":"user","content":"hi"}]}`,
		},
		{
			name: "responses",
			path: "/v1/responses",
			body: `{"model":"k3","stream":true,"input":"hi"}`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &nativeAnthropicRoutesUpstream{
				responses: []*http.Response{
					nativeAnthropicRoutesEmptyResponse(),
					nativeAnthropicRoutesTextResponse(),
				},
			}
			router := newNativeAnthropicRoutesRouter(t, upstream)
			req := httptest.NewRequest(http.MethodPost, tc.path, bytes.NewBufferString(tc.body))
			req.Header.Set("Authorization", "Bearer route-test-key")
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			require.Equal(t, []int64{1, 2}, upstream.calls(), "status=%d body=%s", rec.Code, rec.Body.String())
			require.Equal(t, http.StatusOK, rec.Code)
			require.Contains(t, rec.Body.String(), "route-ok")
			require.NotContains(t, rec.Body.String(), "response.failed")
			if tc.name == "chat_completions" {
				require.Contains(t, rec.Body.String(), "[DONE]")
			} else {
				require.Contains(t, rec.Body.String(), "response.completed")
			}
		})
	}
}
