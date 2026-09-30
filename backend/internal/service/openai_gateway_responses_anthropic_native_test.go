//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestNativeAnthropicResponsesStreamEmptyTerminalEventsFailoverWithoutCommit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name   string
		events []string
	}{
		{name: "message_stop", events: []string{nativeAnthropicMessageStart, nativeAnthropicMessageStop}},
		{name: "terminal_message_delta", events: []string{nativeAnthropicMessageStart, nativeAnthropicTerminalDelta}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"model":"k3","stream":true,"input":"hi"}`)
			upstream := &httpUpstreamRecorder{resp: nativeAnthropicEventsResponse(tc.events...)}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

			_, err := svc.forwardResponsesViaNativeAnthropic(
				context.Background(), c, nativeAnthropicTestAccount(), body, "",
			)

			var failoverErr *UpstreamFailoverError
			require.ErrorAs(t, err, &failoverErr)
			require.False(t, c.Writer.Written())
			require.Empty(t, rec.Body.String())
		})
	}
}

func TestNativeAnthropicResponsesStreamToolOutputIsSemantic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"k3","stream":true,"input":"查天气"}`)
	toolStart := `{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"toolu_1","name":"lookup","input":{}}}`
	toolDelta := `{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"city\":\"shanghai\"}"}}`
	upstream := &httpUpstreamRecorder{resp: nativeAnthropicEventsResponse(
		nativeAnthropicMessageStart,
		toolStart,
		toolDelta,
		nativeAnthropicTextStop,
		nativeAnthropicTerminalDelta,
		nativeAnthropicMessageStop,
	)}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	_, err := svc.forwardResponsesViaNativeAnthropic(
		context.Background(), c, nativeAnthropicTestAccount(), body, "",
	)

	require.NoError(t, err)
	require.Contains(t, rec.Body.String(), "lookup")
	require.Contains(t, rec.Body.String(), "shanghai")
	require.Contains(t, rec.Body.String(), "response.completed")
}

func TestNativeAnthropicResponsesStreamTerminalUsageAfterSemanticOutputIsCompatible(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"k3","stream":true,"input":"hi"}`)
	upstream := &httpUpstreamRecorder{resp: nativeAnthropicEventsResponse(
		nativeAnthropicMessageStart,
		nativeAnthropicTextStart,
		nativeAnthropicTextDelta,
		nativeAnthropicTextStop,
		nativeAnthropicTerminalDelta,
	)}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	_, err := svc.forwardResponsesViaNativeAnthropic(
		context.Background(), c, nativeAnthropicTestAccount(), body, "",
	)

	require.NoError(t, err)
	require.Contains(t, rec.Body.String(), `"first"`)
	require.Contains(t, rec.Body.String(), "response.completed")
}

func TestNativeAnthropicResponsesStreamTruncatedAfterFirstPacketDoesNotFailOver(t *testing.T) {
	gin.SetMode(gin.TestMode)
	body := []byte(`{"model":"k3","stream":true,"input":"hi"}`)
	upstream := &httpUpstreamRecorder{resp: nativeAnthropicEventsResponse(
		nativeAnthropicMessageStart,
		nativeAnthropicTextStart,
		nativeAnthropicTextDelta,
	)}
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(string(body)))
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	_, err := svc.forwardResponsesViaNativeAnthropic(
		context.Background(), c, nativeAnthropicTestAccount(), body, "",
	)

	var failoverErr *UpstreamFailoverError
	require.Error(t, err)
	require.False(t, errors.As(err, &failoverErr))
	require.Contains(t, rec.Body.String(), `"first"`)
	require.NotContains(t, rec.Body.String(), "response.completed")
}
