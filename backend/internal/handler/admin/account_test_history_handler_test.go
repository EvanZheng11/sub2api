package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// accountTestHistoryRepo 只提供 GetByID，其余方法由内嵌接口兜底（不会被调用）。
type accountTestHistoryRepo struct {
	service.AccountRepository
	account *service.Account
}

func (r *accountTestHistoryRepo) GetByID(_ context.Context, id int64) (*service.Account, error) {
	if r.account != nil && r.account.ID == id {
		return r.account, nil
	}
	return nil, service.ErrAccountNotFound
}

// accountTestHistoryQuestions 是题库查询的最小假实现。
type accountTestHistoryQuestions struct {
	err error
}

func (q accountTestHistoryQuestions) GetByID(_ context.Context, id int64) (*service.TestQuestion, error) {
	if q.err != nil {
		return nil, q.err
	}
	return &service.TestQuestion{ID: id, Title: "测试题目", Prompt: "hi"}, nil
}

// setupAccountTestHistoryRouter 用真实的 AccountTestService 组装路由，只替换仓储与题库。
func setupAccountTestHistoryRouter(t *testing.T, account *service.Account, questions service.TestQuestionLookup) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	svc := service.NewAccountTestService(
		&accountTestHistoryRepo{account: account},
		nil, nil, nil, nil, nil, nil, nil,
		questions,
		nil,
	)
	handler := NewAccountHandler(nil, nil, nil, nil, nil, nil, nil, nil, svc, nil, nil, nil, nil, nil)

	router := gin.New()
	router.POST("/api/v1/admin/accounts/:id/test", handler.Test)
	return router
}

func newAccountTestHistoryAccount() *service.Account {
	// API Key 账号且无凭据：会走到 testClaudeAccountConnection 的“无 API Key”分支，
	// 也就是“已经写出 SSE 之后才返回错误”的场景。
	return &service.Account{
		ID:       7,
		Platform: service.PlatformAnthropic,
		Type:     service.AccountTypeAPIKey,
	}
}

// 尚未写出 SSE 时返回 infraerrors：handler 必须映射成对应状态码，并把 reason 透出给前端。
func TestAccountTestQuestionNotFoundReturns404(t *testing.T) {
	router := setupAccountTestHistoryRouter(t, newAccountTestHistoryAccount(), accountTestHistoryQuestions{err: service.ErrTestQuestionNotFound})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/7/test", strings.NewReader(`{"test_question_id":99,"prompt":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
	var body struct {
		Code   int    `json:"code"`
		Reason string `json:"reason"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, http.StatusNotFound, body.Code)
	require.Equal(t, "TEST_QUESTION_NOT_FOUND", body.Reason)
}

// SSE 已经开始写出后返回错误：不能再写第二份响应体，状态码保持 SSE 开始时（200）。
func TestAccountTestErrorAfterSSEWrittenKeepsStreamResponse(t *testing.T) {
	router := setupAccountTestHistoryRouter(t, newAccountTestHistoryAccount(), accountTestHistoryQuestions{})

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/7/test", strings.NewReader(`{"prompt":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	require.NotPanics(t, func() { router.ServeHTTP(w, req) })

	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), "data: ")
	// 不应追加标准错误响应体（handler 已放弃改状态码）。
	require.NotContains(t, w.Body.String(), `"reason"`)
	require.NotContains(t, w.Body.String(), `"code":404`)
}
