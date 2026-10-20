package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// stubTestRecordRepo 是最小内存仓储，只实现列表、详情与文件读取路径。
type stubTestRecordRepo struct {
	mu      sync.Mutex
	records []service.TestRecord
	files   []service.TestRecordFile
}

func (r *stubTestRecordRepo) Create(ctx context.Context, input service.TestRecordInput) (*service.TestRecord, error) {
	return nil, nil
}

func (r *stubTestRecordRepo) ListByQuestion(ctx context.Context, questionID int64, limit, offset int) ([]service.TestRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []service.TestRecord
	for _, record := range r.records {
		if record.TestQuestionID == questionID {
			out = append(out, record)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if offset >= len(out) {
		return []service.TestRecord{}, nil
	}
	end := offset + limit
	if end > len(out) {
		end = len(out)
	}
	return out[offset:end], nil
}

func (r *stubTestRecordRepo) CountByQuestion(ctx context.Context, questionID int64) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var count int64
	for _, record := range r.records {
		if record.TestQuestionID == questionID {
			count++
		}
	}
	return count, nil
}

func (r *stubTestRecordRepo) GetByID(ctx context.Context, id int64) (*service.TestRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, record := range r.records {
		if record.ID == id {
			files := make([]service.TestRecordFile, 0, 2)
			for _, file := range r.files {
				if file.RecordID == id {
					files = append(files, file)
				}
			}
			record.Files = files
			return &record, nil
		}
	}
	return nil, service.ErrTestRecordNotFound
}

func (r *stubTestRecordRepo) UpdateImageCount(ctx context.Context, id int64, imageCount int) error {
	return nil
}

func (r *stubTestRecordRepo) InsertFiles(ctx context.Context, recordID int64, files []service.TestRecordFile) error {
	return nil
}

func (r *stubTestRecordRepo) ListFiles(ctx context.Context, recordID int64) ([]service.TestRecordFile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []service.TestRecordFile
	for _, file := range r.files {
		if file.RecordID == recordID {
			out = append(out, file)
		}
	}
	return out, nil
}

func (r *stubTestRecordRepo) GetFile(ctx context.Context, id int64) (*service.TestRecordFile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, file := range r.files {
		if file.ID == id {
			return &file, nil
		}
	}
	return nil, service.ErrTestRecordFileNotFound
}

func (r *stubTestRecordRepo) PruneByQuestion(ctx context.Context, keepCount int) ([]int64, error) {
	return nil, nil
}

func (r *stubTestRecordRepo) ListRecordIDsByQuestion(ctx context.Context, questionID int64) ([]int64, error) {
	return nil, nil
}

func newTestRecordTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)

	// TestRecordService 的字段未导出，只能通过真实构造函数注入：
	// 根目录由 DATA_DIR 决定，因此这里用 t.Setenv 指向临时目录。
	base := t.TempDir()
	t.Setenv("DATA_DIR", base)
	root := filepath.Join(base, "test-records")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "1"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "1", "1.png"), []byte("img"), 0o600))

	repo := &stubTestRecordRepo{
		records: []service.TestRecord{
			{
				ID: 2, TestQuestionID: 1, AccountName: "账号B", Platform: "grok",
				Model: "grok-4.7", ReasoningEffort: "high", Status: "success",
				LatencyMs: 1234, ImageCount: 1, HasText: true, ResponseText: "结果文本",
				CreatedAt: time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC),
			},
			{
				ID: 1, TestQuestionID: 1, AccountName: "账号A", Platform: "openai",
				Model: "gpt-5", Status: "failed", ErrorMessage: "上游 502",
				LatencyMs: 800, CreatedAt: time.Date(2026, 10, 21, 11, 0, 0, 0, time.UTC),
			},
		},
		files: []service.TestRecordFile{
			{ID: 10, RecordID: 2, Kind: "image", MimeType: "image/png", Path: "1/1.png", SizeBytes: 3},
		},
	}

	h := NewTestRecordHandler(service.NewTestRecordService(repo))
	r := gin.New()
	group := r.Group("/api/v1/admin")
	group.GET("/test-questions/:id/records", h.ListByQuestion)
	group.GET("/test-records/:id", h.Get)
	group.GET("/test-records/:id/files/:file_id", h.File)
	return r
}

func TestTestRecordHandlerListsRecordsInReverseOrder(t *testing.T) {
	r := newTestRecordTestRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/admin/test-questions/1/records?page=1&page_size=20", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var envelope struct {
		Data struct {
			Items []struct {
				ID           int64   `json:"id"`
				AccountName  string  `json:"account_name"`
				Status       string  `json:"status"`
				LatencyMs    int64   `json:"latency_ms"`
				ImageCount   int     `json:"image_count"`
				HasText      bool    `json:"has_text"`
				ResponseText *string `json:"response_text"`
			} `json:"items"`
			Total int64 `json:"total"`
			Page  int   `json:"page"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.Equal(t, int64(2), envelope.Data.Total)
	require.Len(t, envelope.Data.Items, 2)
	require.Equal(t, int64(2), envelope.Data.Items[0].ID, "列表必须按时间倒序")
	require.Equal(t, "账号B", envelope.Data.Items[0].AccountName)
	require.True(t, envelope.Data.Items[0].HasText)
	require.Nil(t, envelope.Data.Items[0].ResponseText, "列表不返回完整文本")
}

func TestTestRecordHandlerGetReturnsTextAndFiles(t *testing.T) {
	r := newTestRecordTestRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/admin/test-records/2", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var envelope struct {
		Data struct {
			ResponseText string `json:"response_text"`
			Files        []struct {
				ID       int64  `json:"id"`
				Kind     string `json:"kind"`
				MimeType string `json:"mime_type"`
			} `json:"files"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.Equal(t, "结果文本", envelope.Data.ResponseText)
	require.Len(t, envelope.Data.Files, 1)
	require.Equal(t, "image/png", envelope.Data.Files[0].MimeType)
}

func TestTestRecordHandlerFileServesBinary(t *testing.T) {
	r := newTestRecordTestRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/admin/test-records/2/files/10", nil))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, "image/png", w.Header().Get("Content-Type"))
	require.Equal(t, "img", w.Body.String())
}

func TestTestRecordHandlerFileSupportsDownload(t *testing.T) {
	r := newTestRecordTestRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/admin/test-records/2/files/10?download=1", nil))
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Header().Get("Content-Disposition"), `attachment; filename="test-2-10.png"`)
}

func TestTestRecordHandlerRejectsInvalidIDs(t *testing.T) {
	r := newTestRecordTestRouter(t)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/admin/test-questions/abc/records", nil))
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/admin/test-records/999", nil))
	require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/admin/test-records/1/files/10", nil))
	require.Equal(t, http.StatusNotFound, w.Code, "文件不属于该记录时应返回 404")
}
