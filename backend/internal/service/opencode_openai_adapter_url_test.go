//go:build unit

package service

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type opencodeURLTestUpstream struct {
	lastReq *http.Request
}

func (u *opencodeURLTestUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	u.lastReq = req
	return nil, nil
}

func (u *opencodeURLTestUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func opencodeURLTestAccount() *Account {
	return &Account{
		ID:       801,
		Platform: PlatformOpenCodeGo,
		Type:     AccountTypeAPIKey,
		Credentials: map[string]any{
			"api_key":      "test-key",
			"account_mode": AccountModeZen,
			"api_protocol": APIProtocolAdaptive,
		},
	}
}

func opencodeURLTestContext(body []byte) *gin.Context {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	return c
}

func TestOpenCodeAdapterDefaultURLsByAccountMode(t *testing.T) {
	tests := []struct {
		mode        string
		protocol    string
		wantBaseURL string
	}{
		{AccountModeGo, APIProtocolChatCompletions, DefaultOpenCodeGoBaseURL},
		{AccountModeGo, APIProtocolResponses, DefaultOpenCodeGoBaseURL},
		{AccountModeGo, APIProtocolAnthropic, DefaultOpenCodeGoAnthropicBaseURL},
		{AccountModeZen, APIProtocolChatCompletions, DefaultOpenCodeZenBaseURL},
		{AccountModeZen, APIProtocolResponses, DefaultOpenCodeZenBaseURL},
		{AccountModeZen, APIProtocolAnthropic, DefaultOpenCodeZenAnthropicBaseURL},
	}

	for _, tt := range tests {
		t.Run(tt.mode+"/"+tt.protocol, func(t *testing.T) {
			account := opencodeURLTestAccount()
			account.Credentials["account_mode"] = tt.mode
			account.Credentials["api_protocol"] = tt.protocol

			baseURL, err := (&OpenAIGatewayService{}).openCodeGoForwardBaseURL(account, tt.protocol)
			require.NoError(t, err)
			require.Equal(t, tt.wantBaseURL, baseURL)
		})
	}
}

func TestOpenCodeAdapterURLPrecedence(t *testing.T) {
	account := opencodeURLTestAccount()
	account.Credentials["api_protocol"] = APIProtocolChatCompletions
	account.Credentials["base_url"] = "https://base.example/v1"
	account.Credentials["api_base_urls"] = map[string]any{
		APIProtocolChatCompletions: "https://protocol.example/v1",
	}

	baseURL, err := (&OpenAIGatewayService{}).openCodeGoForwardBaseURL(account, APIProtocolChatCompletions)
	require.NoError(t, err)
	require.Equal(t, "https://protocol.example/v1", baseURL)

	delete(account.Credentials, "api_base_urls")
	baseURL, err = (&OpenAIGatewayService{}).openCodeGoForwardBaseURL(account, APIProtocolChatCompletions)
	require.NoError(t, err)
	require.Equal(t, "https://base.example/v1", baseURL)
}

func TestOpenCodeAdapterURLPolicyRejectsWithoutSending(t *testing.T) {
	tests := []struct {
		name         string
		baseURL      string
		allowedHosts []string
	}{
		{
			name:         "私网地址",
			baseURL:      "https://127.0.0.1/v1",
			allowedHosts: []string{"127.0.0.1"},
		},
		{
			name:         "未许可主机",
			baseURL:      "https://unlisted.example/v1",
			allowedHosts: []string{"allowed.example"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			account := opencodeURLTestAccount()
			account.Credentials["api_protocol"] = APIProtocolChatCompletions
			account.Credentials["base_url"] = tt.baseURL
			delete(account.Credentials, "api_base_urls")

			upstream := &opencodeURLTestUpstream{}
			svc := &OpenAIGatewayService{
				cfg: &config.Config{
					Security: config.SecurityConfig{
						URLAllowlist: config.URLAllowlistConfig{
							Enabled:       true,
							UpstreamHosts: tt.allowedHosts,
						},
					},
				},
				httpUpstream: upstream,
			}
			body := []byte(`{"model":"opencode/public","stream":false,"messages":[{"role":"user","content":"hi"}]}`)
			c := opencodeURLTestContext(body)

			result, err := svc.ForwardAsChatCompletions(t.Context(), c, account, body, "", "")
			require.Error(t, err)
			require.Nil(t, result)
			require.Nil(t, upstream.lastReq)
		})
	}
}

func TestOpenCodeAdapterMissingAPIKeyStillFailsWithoutExplicitURL(t *testing.T) {
	account := opencodeURLTestAccount()
	account.Credentials["api_protocol"] = APIProtocolChatCompletions
	account.Credentials["api_key"] = ""
	delete(account.Credentials, "base_url")
	delete(account.Credentials, "api_base_urls")

	err := ensureOpenCodeGoForwardCredentials(account)
	require.Error(t, err)
	require.Contains(t, err.Error(), "api_key")
}
