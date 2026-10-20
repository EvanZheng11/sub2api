package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// fakeTestQuestionRepo 是内存版仓储，用于校验 handler + service 的 CRUD 合同。
type fakeTestQuestionRepo struct {
	mu    sync.Mutex
	next  int64
	items map[int64]service.TestQuestion
}

func newFakeTestQuestionRepo() *fakeTestQuestionRepo {
	return &fakeTestQuestionRepo{items: map[int64]service.TestQuestion{}}
}

func (r *fakeTestQuestionRepo) List(ctx context.Context) ([]service.TestQuestion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]service.TestQuestion, 0, len(r.items))
	for _, item := range r.items {
		out = append(out, item)
	}
	// 与 SQL 仓储一致：updated_at desc, id desc
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	return out, nil
}

func (r *fakeTestQuestionRepo) GetByID(ctx context.Context, id int64) (*service.TestQuestion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.items[id]
	if !ok {
		return nil, service.ErrTestQuestionNotFound
	}
	return &item, nil
}

func (r *fakeTestQuestionRepo) Create(ctx context.Context, input service.TestQuestionInput) (*service.TestQuestion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	now := time.Now().UTC()
	item := service.TestQuestion{
		ID:        r.next,
		Title:     input.Title,
		Prompt:    input.Prompt,
		Type:      input.Type,
		CreatedAt: now,
		UpdatedAt: now,
	}
	r.items[item.ID] = item
	return &item, nil
}

func (r *fakeTestQuestionRepo) Update(ctx context.Context, id int64, input service.TestQuestionInput) (*service.TestQuestion, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.items[id]
	if !ok {
		return nil, service.ErrTestQuestionNotFound
	}
	item.Title = input.Title
	item.Prompt = input.Prompt
	item.Type = input.Type
	item.UpdatedAt = time.Now().UTC()
	r.items[id] = item
	return &item, nil
}

func (r *fakeTestQuestionRepo) Delete(ctx context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.items[id]; !ok {
		return service.ErrTestQuestionNotFound
	}
	delete(r.items, id)
	return nil
}

func newTestQuestionTestRouter() (*gin.Engine, *fakeTestQuestionRepo) {
	gin.SetMode(gin.TestMode)
	repo := newFakeTestQuestionRepo()
	h := NewTestQuestionHandler(service.NewTestQuestionService(repo))
	r := gin.New()
	group := r.Group("/api/v1/admin")
	group.GET("/test-questions", h.List)
	group.POST("/test-questions", h.Create)
	group.PUT("/test-questions/:id", h.Update)
	group.DELETE("/test-questions/:id", h.Delete)
	return r, repo
}

func doTestQuestionRequest(t *testing.T, r *gin.Engine, method, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeTestQuestionEnvelope(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var envelope map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	return envelope
}

func TestTestQuestionHandlerCRUDLifecycle(t *testing.T) {
	r, _ := newTestQuestionTestRouter()

	w := doTestQuestionRequest(t, r, http.MethodPost, "/api/v1/admin/test-questions", map[string]any{
		"title":  "  鹈鹕骑自行车  ",
		"prompt": "  画一只骑自行车的鹈鹕  ",
		"type":   "file",
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	envelope := decodeTestQuestionEnvelope(t, w)
	require.Equal(t, float64(0), envelope["code"])
	created := envelope["data"].(map[string]any)
	id := created["id"].(float64)
	require.Greater(t, id, float64(0))
	// 应用层 trim 后再返回
	require.Equal(t, "鹈鹕骑自行车", created["title"])
	require.Equal(t, "画一只骑自行车的鹈鹕", created["prompt"])
	require.Equal(t, "file", created["type"])
	_, err := time.Parse(time.RFC3339Nano, created["created_at"].(string))
	require.NoError(t, err, "created_at 需要是 ISO 时间")
	_, err = time.Parse(time.RFC3339Nano, created["updated_at"].(string))
	require.NoError(t, err, "updated_at 需要是 ISO 时间")

	w = doTestQuestionRequest(t, r, http.MethodGet, "/api/v1/admin/test-questions", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	envelope = decodeTestQuestionEnvelope(t, w)
	list, ok := envelope["data"].([]any)
	require.True(t, ok, "GET data 必须是数组")
	require.Len(t, list, 1)

	w = doTestQuestionRequest(t, r, http.MethodPut, "/api/v1/admin/test-questions/1", map[string]any{
		"title":  "新的标题",
		"prompt": "新的提示词",
		"type":   "text",
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	updated := decodeTestQuestionEnvelope(t, w)["data"].(map[string]any)
	require.Equal(t, "新的标题", updated["title"])
	require.Equal(t, "text", updated["type"])

	w = doTestQuestionRequest(t, r, http.MethodDelete, "/api/v1/admin/test-questions/1", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = doTestQuestionRequest(t, r, http.MethodGet, "/api/v1/admin/test-questions", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Len(t, decodeTestQuestionEnvelope(t, w)["data"], 0)

	// 已删除的题目再删除应返回 404
	w = doTestQuestionRequest(t, r, http.MethodDelete, "/api/v1/admin/test-questions/1", nil)
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
}

func TestTestQuestionHandlerRejectsInvalidInput(t *testing.T) {
	r, _ := newTestQuestionTestRouter()

	cases := []struct {
		name string
		body map[string]any
	}{
		{name: "非法类型", body: map[string]any{"title": "t", "prompt": "p", "type": "video"}},
		{name: "缺少类型", body: map[string]any{"title": "t", "prompt": "p"}},
		{name: "空标题", body: map[string]any{"title": "   ", "prompt": "p", "type": "text"}},
		{name: "空提示词", body: map[string]any{"title": "t", "prompt": "  ", "type": "text"}},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			w := doTestQuestionRequest(t, r, http.MethodPost, "/api/v1/admin/test-questions", tt.body)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		})
	}

	w := doTestQuestionRequest(t, r, http.MethodPut, "/api/v1/admin/test-questions/999", map[string]any{
		"title":  "t",
		"prompt": "p",
		"type":   "image",
	})
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

	w = doTestQuestionRequest(t, r, http.MethodPut, "/api/v1/admin/test-questions/abc", map[string]any{
		"title":  "t",
		"prompt": "p",
		"type":   "image",
	})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
}

func TestTestQuestionHandlerListOrderedByUpdatedAtDesc(t *testing.T) {
	r, repo := newTestQuestionTestRouter()

	first, err := repo.Create(context.Background(), service.TestQuestionInput{Title: "第一题", Prompt: "p1", Type: "text"})
	require.NoError(t, err)
	second, err := repo.Create(context.Background(), service.TestQuestionInput{Title: "第二题", Prompt: "p2", Type: "text"})
	require.NoError(t, err)
	// 更新第一题后应排在最前
	_, err = repo.Update(context.Background(), first.ID, service.TestQuestionInput{Title: "第一题改", Prompt: "p1", Type: "text"})
	require.NoError(t, err)

	w := doTestQuestionRequest(t, r, http.MethodGet, "/api/v1/admin/test-questions", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	list := decodeTestQuestionEnvelope(t, w)["data"].([]any)
	require.Len(t, list, 2)
	require.Equal(t, "第一题改", list[0].(map[string]any)["title"])
	require.Equal(t, "第二题", list[1].(map[string]any)["title"])
	require.NotEqual(t, second.ID, first.ID)
}
