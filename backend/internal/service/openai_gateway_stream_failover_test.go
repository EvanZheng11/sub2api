//go:build unit

package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newStreamFailoverTestContext(method, path string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(method, path, nil)
	return c, rec
}

func newStreamFailoverHTTPResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestResponsesChatFallbackStreamEmpty200ReturnsTypedFailover(t *testing.T) {
	c, rec := newStreamFailoverTestContext(http.MethodPost, "/v1/responses")
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}

	_, err := svc.streamChatCompletionsAsResponses(
		c,
		newStreamFailoverHTTPResponse(""),
		"gpt-5.5",
		nil,
		nil,
		false,
		nil,
		"gpt-5.5",
		"gpt-5.5",
		nil,
		nil,
		time.Now(),
	)

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.False(t, c.Writer.Written())
	require.Empty(t, rec.Body.String())
}

func TestResponsesChatFallbackStreamTruncationAfterSemanticOutputIsTerminal(t *testing.T) {
	c, rec := newStreamFailoverTestContext(http.MethodPost, "/v1/responses")
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
	body := "data: {\"id\":\"chatcmpl_1\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n"

	_, err := svc.streamChatCompletionsAsResponses(
		c,
		newStreamFailoverHTTPResponse(body),
		"gpt-5.5",
		nil,
		nil,
		false,
		nil,
		"gpt-5.5",
		"gpt-5.5",
		nil,
		nil,
		time.Now(),
	)

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.NotErrorAs(t, err, &failoverErr)
	require.Contains(t, rec.Body.String(), `"delta":"hello"`)
	require.NotContains(t, rec.Body.String(), "data: [DONE]")
}

func TestResponsesChatFallbackStreamCompleteToolCallStillTerminatesNormally(t *testing.T) {
	c, rec := newStreamFailoverTestContext(http.MethodPost, "/v1/responses")
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
	body := strings.Join([]string{
		`data: {"id":"chatcmpl_tool","object":"chat.completion.chunk","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"status\"}"}}]},"finish_reason":"tool_calls"}]}`,
		"",
		`data: [DONE]`,
		"",
	}, "\n")

	_, err := svc.streamChatCompletionsAsResponses(
		c,
		newStreamFailoverHTTPResponse(body),
		"gpt-5.5",
		nil,
		nil,
		false,
		nil,
		"gpt-5.5",
		"gpt-5.5",
		nil,
		nil,
		time.Now(),
	)

	require.NoError(t, err)
	require.Contains(t, rec.Body.String(), "response.function_call_arguments.done")
	require.Contains(t, rec.Body.String(), "data: [DONE]")
}

func TestResponsesNativeAnthropicStreamTruncationBeforeSemanticOutputReturnsTypedFailover(t *testing.T) {
	c, rec := newStreamFailoverTestContext(http.MethodPost, "/v1/responses")
	svc := newNativeAnthropicHangTestService(5)
	body := strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"glm-4.7","usage":{"input_tokens":10}}}`,
		"",
	}, "\n")

	_, err := svc.handleResponsesStreamingFromNativeAnthropic(
		newStreamFailoverHTTPResponse(body),
		c,
		"glm-4.7",
		"glm-4.7",
		"glm-4.7",
		nil,
		time.Now(),
		apicompat.ResponsesClientToolMapping{},
	)

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.False(t, c.Writer.Written())
	require.Empty(t, rec.Body.String())
}

func TestResponsesNativeAnthropicStreamTruncationAfterSemanticOutputIsTerminal(t *testing.T) {
	c, rec := newStreamFailoverTestContext(http.MethodPost, "/v1/responses")
	svc := newNativeAnthropicHangTestService(5)
	body := strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"glm-4.7","usage":{"input_tokens":10}}}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
		"",
	}, "\n")

	_, err := svc.handleResponsesStreamingFromNativeAnthropic(
		newStreamFailoverHTTPResponse(body),
		c,
		"glm-4.7",
		"glm-4.7",
		"glm-4.7",
		nil,
		time.Now(),
		apicompat.ResponsesClientToolMapping{},
	)

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.NotErrorAs(t, err, &failoverErr)
	require.Contains(t, rec.Body.String(), "hello")
	require.NotContains(t, rec.Body.String(), "response.completed")
}

func TestResponsesNativeAnthropicStreamMessageStopCompletesNormally(t *testing.T) {
	c, rec := newStreamFailoverTestContext(http.MethodPost, "/v1/responses")
	svc := newNativeAnthropicHangTestService(5)

	_, err := svc.handleResponsesStreamingFromNativeAnthropic(
		newStreamFailoverHTTPResponse(miniAnthropicSSEStream()),
		c,
		"glm-4.7",
		"glm-4.7",
		"glm-4.7",
		nil,
		time.Now(),
		apicompat.ResponsesClientToolMapping{},
	)

	require.NoError(t, err)
	require.Contains(t, rec.Body.String(), "response.output_text.delta")
	require.Contains(t, rec.Body.String(), "response.completed")
}

func TestResponsesNativeAnthropicStreamIdleBeforeSemanticOutputReturnsTypedFailover(t *testing.T) {
	c, rec := newStreamFailoverTestContext(http.MethodPost, "/v1/responses")
	svc := newNativeAnthropicHangTestService(1)
	pr, _ := io.Pipe()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       pr,
	}

	_, err := svc.handleResponsesStreamingFromNativeAnthropic(
		resp,
		c,
		"glm-4.7",
		"glm-4.7",
		"glm-4.7",
		nil,
		time.Now(),
		apicompat.ResponsesClientToolMapping{},
	)

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.False(t, c.Writer.Written())
	require.Empty(t, rec.Body.String())
}

func TestChatNativeAnthropicStreamMessageStopCompletesNormally(t *testing.T) {
	c, rec := newStreamFailoverTestContext(http.MethodPost, "/v1/chat/completions")
	svc := newNativeAnthropicHangTestService(5)

	_, err := svc.handleCCStreamingFromNativeAnthropic(
		newStreamFailoverHTTPResponse(miniAnthropicSSEStream()),
		c,
		"glm-4.7",
		"glm-4.7",
		"glm-4.7",
		nil,
		time.Now(),
	)

	require.NoError(t, err)
	require.Contains(t, rec.Body.String(), "Hello")
	require.Contains(t, rec.Body.String(), "data: [DONE]")
}

func TestChatNativeAnthropicStreamTruncationBeforeSemanticOutputReturnsTypedFailover(t *testing.T) {
	c, rec := newStreamFailoverTestContext(http.MethodPost, "/v1/chat/completions")
	svc := newNativeAnthropicHangTestService(5)
	body := strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"glm-4.7","usage":{"input_tokens":10}}}`,
		"",
	}, "\n")

	_, err := svc.handleCCStreamingFromNativeAnthropic(
		newStreamFailoverHTTPResponse(body),
		c,
		"glm-4.7",
		"glm-4.7",
		"glm-4.7",
		nil,
		time.Now(),
	)

	var failoverErr *UpstreamFailoverError
	require.ErrorAs(t, err, &failoverErr)
	require.False(t, c.Writer.Written())
	require.Empty(t, rec.Body.String())
}

func TestChatNativeAnthropicStreamTruncationAfterSemanticOutputIsTerminal(t *testing.T) {
	c, rec := newStreamFailoverTestContext(http.MethodPost, "/v1/chat/completions")
	svc := newNativeAnthropicHangTestService(5)
	body := strings.Join([]string{
		"event: message_start",
		`data: {"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"glm-4.7","usage":{"input_tokens":10}}}`,
		"",
		"event: content_block_start",
		`data: {"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
		"",
		"event: content_block_delta",
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}`,
		"",
	}, "\n")

	_, err := svc.handleCCStreamingFromNativeAnthropic(
		newStreamFailoverHTTPResponse(body),
		c,
		"glm-4.7",
		"glm-4.7",
		"glm-4.7",
		nil,
		time.Now(),
	)

	require.Error(t, err)
	var failoverErr *UpstreamFailoverError
	require.NotErrorAs(t, err, &failoverErr)
	require.Contains(t, rec.Body.String(), "hello")
	require.NotContains(t, rec.Body.String(), "data: [DONE]")
}
