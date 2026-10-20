//go:build unit

package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 未挂 recorder 时 sendEvent 行为不变，不会 panic。
func TestSendEventWithoutRecorderIsNoop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &AccountTestService{}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = newTestHistoryRequest()

	require.NotPanics(t, func() {
		svc.sendEvent(c, TestEvent{Type: "content", Text: "x"})
	})
}

// 挂上 recorder 后，sendEvent 发出的事件会被累积。
func TestSendEventFeedsRecorder(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &AccountTestService{}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = newTestHistoryRequest()

	recorder := newAccountTestRecorder(TestRecordInput{TestQuestionID: 1}, testHistoryNow())
	c.Set(accountTestRecorderContextKey, recorder)

	svc.sendEvent(c, TestEvent{Type: "content", Text: "片段"})
	svc.sendEvent(c, TestEvent{Type: "error", Error: "上游 502"})
	svc.sendEvent(c, TestEvent{Type: "test_complete", Success: true})

	input, _ := recorder.finish(testHistoryNow())
	require.Equal(t, "片段", input.ResponseText)
	require.Equal(t, "上游 502", input.ErrorMessage)
	// error 优先于 test_complete，仍判定为失败
	require.Equal(t, TestRecordStatusFailed, input.Status)

	svc2 := &AccountTestService{}
	c2, _ := gin.CreateTestContext(httptest.NewRecorder())
	c2.Request = newTestHistoryRequest()
	recorder2 := newAccountTestRecorder(TestRecordInput{TestQuestionID: 1}, testHistoryNow())
	c2.Set(accountTestRecorderContextKey, recorder2)
	svc2.sendEvent(c2, TestEvent{Type: "content", Text: "成功"})
	svc2.sendEvent(c2, TestEvent{Type: "test_complete", Success: true})

	input2, _ := recorder2.finish(testHistoryNow())
	require.Equal(t, "成功", input2.ResponseText)
	require.Equal(t, TestRecordStatusSuccess, input2.Status)
}

// 被抑制的 test_complete 不会被 sendEvent 发出，也不应计入 recorder。
func TestSuppressedCompletionIsNotRecorded(t *testing.T) {
	gin.SetMode(gin.TestMode)
	svc := &AccountTestService{}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = newTestHistoryRequest()
	c.Set(accountTestSuppressCompletionContextKey, true)

	recorder := newAccountTestRecorder(TestRecordInput{TestQuestionID: 1}, testHistoryNow())
	c.Set(accountTestRecorderContextKey, recorder)

	svc.sendEvent(c, TestEvent{Type: "test_complete", Success: true})

	input, _ := recorder.finish(testHistoryNow())
	require.Equal(t, TestRecordStatusFailed, input.Status)
}

func newTestHistoryRequest() *http.Request {
	req, _ := http.NewRequest(http.MethodPost, "/", nil)
	return req.WithContext(context.Background())
}

func testHistoryNow() time.Time {
	return time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC)
}
