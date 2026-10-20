# 账号测试历史记录 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在测试题库页面的题目操作列新增「测试历史」按钮，弹框按时间倒序分页展示该题目的历史测试记录（账号、模型、思考强度、测试时长、状态、结果按钮），结果按钮可查看文本、查看图片、下载文件、查看错误。

**Architecture:** 后端在 `AccountTestService.sendEvent` 出口挂一个挂在 `gin.Context` 上的 recorder 累积 SSE 结果，`TestAccountConnection` 的 `defer` 里统一落库（文本入库、媒体落磁盘、每题裁剪最近 20 条）。前端在题库页操作列加按钮，弹出 `TestRecordHistoryModal` 表格弹框，文件提取逻辑从 `AccountTestModal.vue` 抽到 `utils/testArtifact.ts` 共用。

**Tech Stack:** Go 1.26 + Gin + raw SQL（`database/sql`）+ PostgreSQL + wire；Vue 3 `<script setup>` + TypeScript + Tailwind + vitest。

**设计文档:** `docs/superpowers/specs/2026-10-21-account-test-history-design.md`

**工作目录:** `/home/andy/app/sub2api-deploy/src/worktrees/wt_test_history`（分支 `worktrees/wt_test_history`，下文所有路径相对于该目录）

**重要约束（每个任务都适用）:**
- 项目要求页面文案与注释用中文。
- 后端新表走 raw SQL，不引入 ent/GORM 实体。
- 后端分层：接口定义在 service 包 → repository 实现 → handler 只依赖 service；wire 注册 4 处 + 重新生成 `cmd/server/wire_gen.go`。
- 前端不新增 UI 库，只用现有 `BaseDialog` / `DataTable` / `Pagination` / `EmptyState` / `Icon` / `useClipboard`。
- 严禁使用 `v-html` 渲染模型输出。

---

## 文件清单

**新增：**

| 路径 | 职责 |
| --- | --- |
| `backend/migrations/247_account_test_records.sql` | 建 `account_test_records` + `account_test_record_files` 两张表、约束、索引 |
| `backend/migrations/account_test_records_migration_test.go` | 迁移 SQL 形状断言 |
| `backend/internal/service/account_test_record.go` | 领域类型、仓储接口、recorder、落盘与裁剪服务 |
| `backend/internal/service/account_test_record_test.go` | recorder 与服务单测（`//go:build unit`） |
| `backend/internal/service/account_test_service_history_test.go` | sendEvent 接入 recorder 的回归单测 |
| `backend/internal/repository/account_test_record_repo.go` | raw SQL 仓储实现 |
| `backend/internal/handler/admin/test_record_handler.go` | 列表 / 详情 / 文件三个管理端接口 |
| `backend/internal/handler/admin/test_record_handler_test.go` | handler 合同测试（stub 仓储） |
| `frontend/src/utils/testArtifact.ts` | 从模型输出提取可下载文件的共享工具 |
| `frontend/src/utils/__tests__/testArtifact.spec.ts` | 提取逻辑单测 |
| `frontend/src/api/admin/testRecords.ts` | 测试记录 API 客户端与类型 |
| `frontend/src/components/admin/testquestion/TestRecordHistoryModal.vue` | 历史记录表格弹框 |
| `frontend/src/components/admin/testquestion/__tests__/TestRecordHistoryModal.spec.ts` | 弹框渲染与结果按钮测试 |

**修改：**

| 路径 | 改动 |
| --- | --- |
| `backend/internal/service/account_test_service.go` | `AccountTestOptions` 加两个字段、`sendEvent` 加捕获、`TestAccountConnection` 建 recorder + defer 落库、`AccountTestService` 加两个依赖 |
| `backend/internal/service/test_question_service.go` | 加 `GetByID`、加 `TestRecordStore` 依赖、删除题目时清理记录文件 |
| `backend/internal/service/wire.go` | 注册 `NewTestRecordService` |
| `backend/internal/repository/wire.go` | 注册 `NewTestRecordRepository` |
| `backend/internal/handler/wire.go` | 注入 `*admin.TestRecordHandler` |
| `backend/internal/handler/handler.go` | `AdminHandlers` 加 `TestRecord` 字段 |
| `backend/internal/handler/admin/account_handler.go` | `TestAccountRequest` 加 `test_question_id` 并传入 opts |
| `backend/internal/handler/admin/test_question_handler_test.go` | `NewTestQuestionService` 多一个参数 |
| `backend/internal/server/routes/admin.go` | 题目路由加 `/:id/records`，新增 `registerTestRecordRoutes` |
| `backend/cmd/server/wire_gen.go` | 重新生成 |
| `frontend/src/api/admin/testQuestions.ts` | 加 `records()` |
| `frontend/src/api/admin/index.ts` | 汇总导出 `testRecords` |
| `frontend/src/views/admin/TestQuestionsView.vue` | 操作列加「测试历史」按钮 + 挂载弹框 |
| `frontend/src/views/admin/__tests__/TestQuestionsView.spec.ts` | 补按钮与弹框测试 |
| `frontend/src/components/admin/account/AccountTestModal.vue` | 改用共享工具；请求体带 `test_question_id` |
| `frontend/src/i18n/locales/zh/admin/testQuestions.ts` | 加 `history` 命名空间 |
| `frontend/src/i18n/locales/en/admin/testQuestions.ts` | 加 `history` 命名空间 |

---

### Task 1: 数据库迁移

**Files:**
- Create: `backend/migrations/247_account_test_records.sql`
- Test: `backend/migrations/account_test_records_migration_test.go`

- [ ] **Step 1: 写失败的迁移形状测试**

创建 `backend/migrations/account_test_records_migration_test.go`：

```go
package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 测试历史迁移必须建两张表、状态与耗时约束、题目倒序索引，并正确声明外键删除动作。
func TestAccountTestRecordsMigrationShape(t *testing.T) {
	content, err := FS.ReadFile("247_account_test_records.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")

	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS account_test_records")
	require.Contains(t, sql, "test_question_id BIGINT NOT NULL REFERENCES test_questions(id) ON DELETE CASCADE")
	require.Contains(t, sql, "account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL")
	require.Contains(t, sql, "question_title VARCHAR(200) NOT NULL DEFAULT ''")
	require.Contains(t, sql, "reasoning_effort VARCHAR(20) NOT NULL DEFAULT ''")
	require.Contains(t, sql, "response_text TEXT NOT NULL DEFAULT ''")
	require.Contains(t, sql, "response_truncated BOOLEAN NOT NULL DEFAULT FALSE")
	require.Contains(t, sql, "latency_ms BIGINT NOT NULL DEFAULT 0")
	require.Contains(t, sql, "CHECK (status IN ('success', 'failed'))")
	require.Contains(t, sql, "CHECK (latency_ms >= 0)")
	require.Contains(t, sql, "CREATE INDEX IF NOT EXISTS idx_account_test_records_question_created")
	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS account_test_record_files")
	require.Contains(t, sql, "record_id BIGINT NOT NULL REFERENCES account_test_records(id) ON DELETE CASCADE")
	require.Contains(t, sql, "path VARCHAR(500) NOT NULL")
	require.Contains(t, sql, "CHECK (kind IN ('image', 'audio', 'video'))")
	require.Contains(t, sql, "CREATE INDEX IF NOT EXISTS idx_account_test_record_files_record")
	// 迁移只建表建索引，不写任何种子数据
	require.NotContains(t, sql, "INSERT INTO")
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd backend && go test ./migrations/ -run TestAccountTestRecordsMigrationShape`
Expected: FAIL —— `open 247_account_test_records.sql: file does not exist`

- [ ] **Step 3: 写迁移 SQL**

创建 `backend/migrations/247_account_test_records.sql`：

```sql
-- 账号测试历史记录：题目维度的测试结果。
-- 文本结果入库，图片/音频/视频由应用层落盘，库中只存相对路径。
-- 保留条数由应用层在每次入库后裁剪（每题保留最近 N 条），本迁移不做清理。
CREATE TABLE IF NOT EXISTS account_test_records (
    id BIGSERIAL PRIMARY KEY,
    test_question_id BIGINT NOT NULL REFERENCES test_questions(id) ON DELETE CASCADE,
    question_title VARCHAR(200) NOT NULL DEFAULT '',
    account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL,
    account_name VARCHAR(200) NOT NULL DEFAULT '',
    platform VARCHAR(50) NOT NULL DEFAULT '',
    model VARCHAR(200) NOT NULL DEFAULT '',
    reasoning_effort VARCHAR(20) NOT NULL DEFAULT '',
    status VARCHAR(16) NOT NULL DEFAULT 'success',
    error_message TEXT NOT NULL DEFAULT '',
    response_text TEXT NOT NULL DEFAULT '',
    response_truncated BOOLEAN NOT NULL DEFAULT FALSE,
    image_count INTEGER NOT NULL DEFAULT 0,
    latency_ms BIGINT NOT NULL DEFAULT 0,
    started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    finished_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT account_test_records_status_check
        CHECK (status IN ('success', 'failed')),
    CONSTRAINT account_test_records_latency_check
        CHECK (latency_ms >= 0)
);

-- 列表固定按题目过滤并按 created_at desc, id desc 排序。
CREATE INDEX IF NOT EXISTS idx_account_test_records_question_created
    ON account_test_records (test_question_id, created_at DESC, id DESC);

-- 媒体文件元数据，二进制内容不落库。
CREATE TABLE IF NOT EXISTS account_test_record_files (
    id BIGSERIAL PRIMARY KEY,
    record_id BIGINT NOT NULL REFERENCES account_test_records(id) ON DELETE CASCADE,
    kind VARCHAR(16) NOT NULL,
    mime_type VARCHAR(100) NOT NULL DEFAULT '',
    path VARCHAR(500) NOT NULL,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT account_test_record_files_kind_check
        CHECK (kind IN ('image', 'audio', 'video'))
);

CREATE INDEX IF NOT EXISTS idx_account_test_record_files_record
    ON account_test_record_files (record_id);
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd backend && go test ./migrations/`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add backend/migrations/247_account_test_records.sql backend/migrations/account_test_records_migration_test.go
git commit -m "feat: 新增账号测试历史记录迁移"
```

---

### Task 2: 领域类型、仓储接口与结果 recorder

**Files:**
- Create: `backend/internal/service/account_test_record.go`
- Test: `backend/internal/service/account_test_record_test.go`

本任务只写纯逻辑（类型 + 累积 + 截断 + 路径解析），不碰数据库与磁盘，供 Task 3 复用。

- [ ] **Step 1: 写失败的测试**

创建 `backend/internal/service/account_test_record_test.go`：

```go
//go:build unit

package service

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newTestRecorder() *accountTestRecorder {
	return newAccountTestRecorder(TestRecordInput{
		TestQuestionID:  7,
		QuestionTitle:   "鹈鹕骑自行车",
		AccountID:       42,
		AccountName:     "grok-账号",
		Platform:        "grok",
		Model:           "grok-4.7",
		ReasoningEffort: "high",
	}, time.Date(2026, 10, 21, 10, 0, 0, 0, time.UTC))
}

func TestAccountTestRecorderAccumulatesContentAndMedia(t *testing.T) {
	recorder := newTestRecorder()

	recorder.capture(TestEvent{Type: "content", Text: "你好"})
	recorder.capture(TestEvent{Type: "content", Text: "世界"})
	recorder.capture(TestEvent{Type: "image", ImageURL: "data:image/png;base64,aGVsbG8="})
	recorder.capture(TestEvent{Type: "test_complete", Success: true})

	input, media := recorder.finish(time.Date(2026, 10, 21, 10, 0, 3, 0, time.UTC))

	require.Equal(t, "你好世界", input.ResponseText)
	require.False(t, input.ResponseTruncated)
	require.Equal(t, TestRecordStatusSuccess, input.Status)
	require.Equal(t, int64(3000), input.LatencyMs)
	require.Len(t, media, 1)
	require.Equal(t, TestRecordFileKindImage, media[0].Kind)
	require.Equal(t, "image/png", media[0].MimeType)
	require.Equal(t, []byte("hello"), media[0].Data)
}

func TestAccountTestRecorderMarksFailedOnErrorEvent(t *testing.T) {
	recorder := newTestRecorder()
	recorder.capture(TestEvent{Type: "error", Error: "上游 502"})

	input, _ := recorder.finish(time.Now())

	require.Equal(t, TestRecordStatusFailed, input.Status)
	require.Equal(t, "上游 502", input.ErrorMessage)
}

func TestAccountTestRecorderMarksFailedWithoutCompletion(t *testing.T) {
	recorder := newTestRecorder()
	recorder.capture(TestEvent{Type: "content", Text: "半截输出"})

	input, _ := recorder.finish(time.Now())

	require.Equal(t, TestRecordStatusFailed, input.Status)
}

func TestAccountTestRecorderIgnoresIncompleteCompletion(t *testing.T) {
	recorder := newTestRecorder()
	recorder.capture(TestEvent{Type: "test_complete", Success: false})

	input, _ := recorder.finish(time.Now())

	require.Equal(t, TestRecordStatusFailed, input.Status)
}

func TestAccountTestRecorderTruncatesLongResponse(t *testing.T) {
	recorder := newTestRecorder()
	recorder.capture(TestEvent{Type: "content", Text: strings.Repeat("A", maxTestRecordResponseBytes+100)})
	recorder.capture(TestEvent{Type: "test_complete", Success: true})

	input, _ := recorder.finish(time.Now())

	require.True(t, input.ResponseTruncated)
	require.Len(t, input.ResponseText, maxTestRecordResponseBytes)
}

func TestAccountTestRecorderCapsMediaFiles(t *testing.T) {
	recorder := newTestRecorder()
	for i := 0; i < maxTestRecordMediaFiles+3; i++ {
		recorder.capture(TestEvent{Type: "image", ImageURL: "data:image/png;base64,aGVsbG8="})
	}

	_, media := recorder.finish(time.Now())

	require.Len(t, media, maxTestRecordMediaFiles)
}

func TestAccountTestRecorderSkipsMalformedDataURL(t *testing.T) {
	recorder := newTestRecorder()
	recorder.capture(TestEvent{Type: "image", ImageURL: "not-a-data-url"})
	recorder.capture(TestEvent{Type: "video", VideoURL: "data:video/mp4;base64,%%%"})

	_, media := recorder.finish(time.Now())

	require.Empty(t, media)
}

func TestDecodeTestRecordDataURL(t *testing.T) {
	mime, payload, ok := decodeTestRecordDataURL("data:text/plain;base64,aGVsbG8=")
	require.True(t, ok)
	require.Equal(t, "text/plain", mime)
	require.Equal(t, []byte("hello"), payload)

	mime, payload, ok = decodeTestRecordDataURL("data:text/plain,hello%20world")
	require.True(t, ok)
	require.Equal(t, "text/plain", mime)
	require.Equal(t, []byte("hello world"), payload)

	_, _, ok = decodeTestRecordDataURL("data:text/plain;base64,%%%")
	require.False(t, ok)
}

func TestSafeTestRecordFilePathRejectsEscape(t *testing.T) {
	root := t.TempDir()

	full, err := safeTestRecordFilePath(root, "12/1.png")
	require.NoError(t, err)
	require.Equal(t, filepath.ToSlash(filepath.Join(root, "12", "1.png")), filepath.ToSlash(full))

	_, err = safeTestRecordFilePath(root, "../../etc/passwd")
	require.ErrorIs(t, err, ErrTestRecordFilePathInvalid)

	_, err = safeTestRecordFilePath(root, "/etc/passwd")
	require.ErrorIs(t, err, ErrTestRecordFilePathInvalid)

	_, err = safeTestRecordFilePath(root, "  ")
	require.ErrorIs(t, err, ErrTestRecordFilePathInvalid)
}

func TestTestRecordMediaExtension(t *testing.T) {
	require.Equal(t, "png", testRecordMediaExtension("image/png"))
	require.Equal(t, "jpg", testRecordMediaExtension("image/jpeg"))
	require.Equal(t, "mp4", testRecordMediaExtension("video/mp4"))
	require.Equal(t, "bin", testRecordMediaExtension("application/x-unknown"))
	require.Equal(t, "bin", testRecordMediaExtension(""))
}
```

import 块需要 `path/filepath`（`TestSafeTestRecordFilePathRejectsEscape` 用到），最终为：

```go
import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd backend && go test -tags=unit ./internal/service/ -run 'TestAccountTestRecorder|TestDecodeTestRecordDataURL|TestSafeTestRecordFilePath|TestTestRecordMediaExtension'`
Expected: FAIL —— `undefined: newAccountTestRecorder`、`undefined: TestRecordInput` 等编译错误

- [ ] **Step 3: 写实现**

创建 `backend/internal/service/account_test_record.go`：

```go
package service

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// accountTestRecorderContextKey 是 recorder 挂在 gin.Context 上的键名。
const accountTestRecorderContextKey = "account_test_record_recorder"

// 测试记录状态。
const (
	TestRecordStatusSuccess = "success"
	TestRecordStatusFailed  = "failed"
)

// 测试记录文件的媒体类型。
const (
	TestRecordFileKindImage = "image"
	TestRecordFileKindAudio = "audio"
	TestRecordFileKindVideo = "video"
)

const (
	// testRecordKeepPerQuestion 每题保留的记录条数，超出后由服务层裁剪并删除磁盘文件。
	testRecordKeepPerQuestion = 20
	// maxTestRecordResponseBytes 单条记录文本结果的字节上限，超出按字节安全截断。
	maxTestRecordResponseBytes = 64 * 1024
	// maxTestRecordMediaFiles 单条记录最多落盘的媒体文件数，防止单次测试产出过量文件。
	maxTestRecordMediaFiles = 8
)

var (
	ErrTestRecordNotFound        = infraerrors.NotFound("TEST_RECORD_NOT_FOUND", "测试记录不存在")
	ErrTestRecordFileNotFound    = infraerrors.NotFound("TEST_RECORD_FILE_NOT_FOUND", "测试结果文件不存在")
	ErrTestRecordFilePathInvalid = infraerrors.BadRequest("TEST_RECORD_FILE_PATH_INVALID", "测试结果文件路径不合法")
)

// testRecordMediaExtensions 把常见媒体 MIME 映射为落盘扩展名，未知类型统一用 bin。
var testRecordMediaExtensions = map[string]string{
	"image/png":       "png",
	"image/jpeg":      "jpg",
	"image/jpg":       "jpg",
	"image/gif":       "gif",
	"image/webp":      "webp",
	"image/svg+xml":   "svg",
	"image/avif":      "avif",
	"image/bmp":       "bmp",
	"audio/mpeg":      "mp3",
	"audio/mp4":       "m4a",
	"audio/wav":       "wav",
	"audio/x-wav":     "wav",
	"audio/ogg":       "ogg",
	"audio/webm":      "weba",
	"video/mp4":       "mp4",
	"video/webm":      "webm",
	"video/quicktime": "mov",
}

// TestRecord 是一次账号测试的历史记录。
// 列表接口不返回 ResponseText（omitempty），详情接口返回 Files。
type TestRecord struct {
	ID                int64           `json:"id"`
	TestQuestionID    int64           `json:"test_question_id"`
	QuestionTitle     string          `json:"question_title"`
	AccountID         *int64          `json:"account_id"`
	AccountName       string          `json:"account_name"`
	Platform          string          `json:"platform"`
	Model             string          `json:"model"`
	ReasoningEffort   string          `json:"reasoning_effort"`
	Status            string          `json:"status"`
	ErrorMessage      string          `json:"error_message"`
	ResponseText      string          `json:"response_text,omitempty"`
	HasText           bool            `json:"has_text"`
	ResponseTruncated bool            `json:"response_truncated"`
	ImageCount        int             `json:"image_count"`
	LatencyMs         int64           `json:"latency_ms"`
	StartedAt         time.Time       `json:"started_at"`
	FinishedAt        time.Time       `json:"finished_at"`
	CreatedAt         time.Time       `json:"created_at"`
	Files             []TestRecordFile `json:"files,omitempty"`
}

// TestRecordFile 是一条记录落盘的媒体文件元数据。
type TestRecordFile struct {
	ID        int64     `json:"id"`
	RecordID  int64     `json:"record_id"`
	Kind      string    `json:"kind"`
	MimeType  string    `json:"mime_type"`
	Path      string    `json:"-"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
}

// TestRecordInput 是写入一条测试记录的入参。
type TestRecordInput struct {
	TestQuestionID    int64
	QuestionTitle     string
	AccountID         int64
	AccountName       string
	Platform          string
	Model             string
	ReasoningEffort   string
	Status            string
	ErrorMessage      string
	ResponseText      string
	ResponseTruncated bool
	LatencyMs         int64
	StartedAt         time.Time
	FinishedAt        time.Time
}

// TestRecordMedia 是待落盘的媒体内容。
type TestRecordMedia struct {
	Kind     string
	MimeType string
	Data     []byte
}

// TestRecordFileContent 是文件接口要返回的内容。
type TestRecordFileContent struct {
	MimeType string
	FileName string
	Data     []byte
}

// TestRecordRepository 测试记录仓储，使用轻量 raw SQL 实现。
type TestRecordRepository interface {
	Create(ctx context.Context, input TestRecordInput) (*TestRecord, error)
	ListByQuestion(ctx context.Context, questionID int64, limit, offset int) ([]TestRecord, error)
	CountByQuestion(ctx context.Context, questionID int64) (int64, error)
	GetByID(ctx context.Context, id int64) (*TestRecord, error)
	UpdateImageCount(ctx context.Context, id int64, imageCount int) error
	InsertFiles(ctx context.Context, recordID int64, files []TestRecordFile) error
	ListFiles(ctx context.Context, recordID int64) ([]TestRecordFile, error)
	GetFile(ctx context.Context, id int64) (*TestRecordFile, error)
	PruneByQuestion(ctx context.Context, keepCount int) ([]int64, error)
	ListRecordIDsByQuestion(ctx context.Context, questionID int64) ([]int64, error)
}

// TestRecordWriter 是账号测试写入历史所需的最小依赖。
type TestRecordWriter interface {
	Record(ctx context.Context, input TestRecordInput, media []TestRecordMedia) error
}

// TestRecordStore 是题库删除时清理历史所需的最小依赖。
type TestRecordStore interface {
	DeleteQuestionRecords(ctx context.Context, questionID int64) error
}

// TestQuestionLookup 是账号测试校验题目是否仍存在所需的最小依赖。
type TestQuestionLookup interface {
	GetByID(ctx context.Context, id int64) (*TestQuestion, error)
}

// ---- recorder：只做内存累积，不做 I/O ----

// accountTestRecorder 从 sendEvent 累积一次测试的结果，由 finish 产出可落库的快照。
type accountTestRecorder struct {
	mu        sync.Mutex
	input     TestRecordInput
	text      strings.Builder
	media     []TestRecordMedia
	succeeded bool
	failed    bool
	errorText string
}

func newAccountTestRecorder(input TestRecordInput, startedAt time.Time) *accountTestRecorder {
	input.StartedAt = startedAt
	return &accountTestRecorder{input: input}
}

// accountTestRecorderFromContext 取出挂在 gin.Context 上的 recorder，没有时返回 nil。
func accountTestRecorderFromContext(c *gin.Context) *accountTestRecorder {
	if c == nil {
		return nil
	}
	value, ok := c.Get(accountTestRecorderContextKey)
	if !ok {
		return nil
	}
	recorder, _ := value.(*accountTestRecorder)
	return recorder
}

func (r *accountTestRecorder) capture(event TestEvent) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	switch event.Type {
	case "content":
		r.text.WriteString(event.Text)
	case "image":
		r.appendMedia(TestRecordFileKindImage, event.MimeType, event.ImageURL)
	case "audio":
		r.appendMedia(TestRecordFileKindAudio, event.MimeType, event.AudioURL)
	case "video":
		r.appendMedia(TestRecordFileKindVideo, event.MimeType, event.VideoURL)
	case "error":
		r.failed = true
		if strings.TrimSpace(event.Error) != "" {
			r.errorText = event.Error
		}
	case "test_complete":
		if event.Success {
			r.succeeded = true
		}
	}
}

func (r *accountTestRecorder) appendMedia(kind, mimeType, dataURL string) {
	if len(r.media) >= maxTestRecordMediaFiles {
		return
	}
	decodedMime, payload, ok := decodeTestRecordDataURL(dataURL)
	if !ok || len(payload) == 0 {
		return
	}
	if strings.TrimSpace(mimeType) == "" {
		mimeType = decodedMime
	}
	r.media = append(r.media, TestRecordMedia{Kind: kind, MimeType: mimeType, Data: payload})
}

// finish 结束累积并产出落库快照：既没有 error 也没有成功完成时判定为失败。
func (r *accountTestRecorder) finish(finishedAt time.Time) (TestRecordInput, []TestRecordMedia) {
	r.mu.Lock()
	defer r.mu.Unlock()

	input := r.input
	input.ResponseText, input.ResponseTruncated = truncateTestRecordResponse(r.text.String())
	input.FinishedAt = finishedAt
	input.LatencyMs = finishedAt.Sub(input.StartedAt).Milliseconds()
	if input.LatencyMs < 0 {
		input.LatencyMs = 0
	}
	switch {
	case r.failed:
		input.Status = TestRecordStatusFailed
	case r.succeeded:
		input.Status = TestRecordStatusSuccess
	default:
		input.Status = TestRecordStatusFailed
	}
	if r.errorText != "" {
		input.ErrorMessage = r.errorText
	}

	media := make([]TestRecordMedia, len(r.media))
	copy(media, r.media)
	return input, media
}

// truncateTestRecordResponse 按字节上限截断文本，并保证结果仍是合法 UTF-8。
func truncateTestRecordResponse(text string) (string, bool) {
	if len(text) <= maxTestRecordResponseBytes {
		return text, false
	}
	truncated := text[:maxTestRecordResponseBytes]
	for len(truncated) > 0 && !utf8.ValidString(truncated) {
		truncated = truncated[:len(truncated)-1]
	}
	return truncated, true
}

// decodeTestRecordDataURL 解析 data URL，返回 MIME 与二进制内容。
func decodeTestRecordDataURL(dataURL string) (string, []byte, bool) {
	if !strings.HasPrefix(dataURL, "data:") {
		return "", nil, false
	}
	rest := dataURL[len("data:"):]
	comma := strings.Index(rest, ",")
	if comma < 0 {
		return "", nil, false
	}
	meta := rest[:comma]
	payloadPart := rest[comma+1:]
	isBase64 := strings.HasSuffix(meta, ";base64")
	mime := strings.TrimSuffix(meta, ";base64")
	if strings.TrimSpace(mime) == "" {
		mime = "application/octet-stream"
	}
	if isBase64 {
		raw, err := base64.StdEncoding.DecodeString(payloadPart)
		if err != nil {
			return "", nil, false
		}
		return mime, raw, true
	}
	decoded, err := url.QueryUnescape(payloadPart)
	if err != nil {
		return "", nil, false
	}
	return mime, []byte(decoded), true
}

// testRecordMediaExtension 把 MIME 映射为落盘扩展名，未知类型统一 bin。
func testRecordMediaExtension(mimeType string) string {
	if ext, ok := testRecordMediaExtensions[strings.ToLower(strings.TrimSpace(mimeType))]; ok {
		return ext
	}
	return "bin"
}

// safeTestRecordFilePath 把库中存的相对路径拼到根目录，并拒绝越界路径。
func safeTestRecordFilePath(root, rel string) (string, error) {
	trimmed := strings.TrimSpace(rel)
	if trimmed == "" || filepath.IsAbs(trimmed) {
		return "", ErrTestRecordFilePathInvalid
	}
	cleaned := filepath.Clean(trimmed)
	if cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", ErrTestRecordFilePathInvalid
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", ErrTestRecordFilePathInvalid
	}
	absFull, err := filepath.Abs(filepath.Join(root, cleaned))
	if err != nil {
		return "", ErrTestRecordFilePathInvalid
	}
	if absFull != absRoot && !strings.HasPrefix(absFull, absRoot+string(filepath.Separator)) {
		return "", ErrTestRecordFilePathInvalid
	}
	return absFull, nil
}

// resolveTestRecordRootDir 解析测试结果落盘根目录，沿用插件目录的约定：DATA_DIR 或 ./data。
func resolveTestRecordRootDir() string {
	base := strings.TrimSpace(os.Getenv("DATA_DIR"))
	if base == "" {
		base = "./data"
	}
	return filepath.Join(base, "test-records")
}

// recordRelativePath 生成 <record_id>/<序号>.<扩展名> 形式的相对路径。
func recordRelativePath(recordID int64, index int, mimeType string) string {
	return filepath.Join(
		strconv.FormatInt(recordID, 10),
		fmt.Sprintf("%d.%s", index, testRecordMediaExtension(mimeType)),
	)
}

// TestRecordService 测试记录业务逻辑：落盘、入库、裁剪与读取。
type TestRecordService struct {
	repo TestRecordRepository
	root string
	keep int
}

func NewTestRecordService(repo TestRecordRepository) *TestRecordService {
	return &TestRecordService{
		repo: repo,
		root: resolveTestRecordRootDir(),
		keep: testRecordKeepPerQuestion,
	}
}

func (s *TestRecordService) recordDir(recordID int64) string {
	return filepath.Join(s.root, strconv.FormatInt(recordID, 10))
}

// Record 写入一条测试记录：先入库拿到 ID，再落盘媒体，最后更新图片数并裁剪。
func (s *TestRecordService) Record(ctx context.Context, input TestRecordInput, media []TestRecordMedia) error {
	input.QuestionTitle = strings.TrimSpace(input.QuestionTitle)
	input.AccountName = strings.TrimSpace(input.AccountName)
	input.Model = strings.TrimSpace(input.Model)
	if input.StartedAt.IsZero() {
		input.StartedAt = time.Now()
	}
	if input.FinishedAt.IsZero() {
		input.FinishedAt = input.StartedAt
	}
	if input.Status != TestRecordStatusSuccess && input.Status != TestRecordStatusFailed {
		input.Status = TestRecordStatusFailed
	}

	created, err := s.repo.Create(ctx, input)
	if err != nil {
		return err
	}

	files := s.writeMedia(ctx, created.ID, media)
	if len(files) > 0 {
		if err := s.repo.InsertFiles(ctx, created.ID, files); err != nil {
			return err
		}
	}
	if err := s.repo.UpdateImageCount(ctx, created.ID, countImageFiles(files)); err != nil {
		return err
	}
	return s.prune(ctx)
}

func (s *TestRecordService) writeMedia(ctx context.Context, recordID int64, media []TestRecordMedia) []TestRecordFile {
	if len(media) == 0 {
		return nil
	}
	dir := s.recordDir(recordID)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		logger.FromContext(ctx).Warn("account_test_record.media_dir_failed",
			zap.Int64("record_id", recordID), zap.Error(err))
		return nil
	}

	files := make([]TestRecordFile, 0, len(media))
	for i, item := range media {
		rel := recordRelativePath(recordID, i+1, item.MimeType)
		full := filepath.Join(s.root, rel)
		if err := os.WriteFile(full, item.Data, 0o600); err != nil {
			logger.FromContext(ctx).Warn("account_test_record.media_write_failed",
				zap.Int64("record_id", recordID), zap.Int("index", i+1), zap.Error(err))
			break
		}
		files = append(files, TestRecordFile{
			RecordID:  recordID,
			Kind:      item.Kind,
			MimeType:  item.MimeType,
			Path:      rel,
			SizeBytes: int64(len(item.Data)),
		})
	}
	return files
}

func countImageFiles(files []TestRecordFile) int {
	count := 0
	for _, file := range files {
		if file.Kind == TestRecordFileKindImage {
			count++
		}
	}
	return count
}

func (s *TestRecordService) prune(ctx context.Context) error {
	pruned, err := s.repo.PruneByQuestion(ctx, s.keep)
	if err != nil {
		return err
	}
	for _, recordID := range pruned {
		s.removeRecordDir(recordID)
	}
	return nil
}

func (s *TestRecordService) removeRecordDir(recordID int64) {
	if recordID <= 0 {
		return
	}
	if err := os.RemoveAll(s.recordDir(recordID)); err != nil {
		logger.FromContext(context.Background()).Warn("account_test_record.remove_dir_failed",
			zap.Int64("record_id", recordID), zap.Error(err))
	}
}

// ListByQuestion 分页列出某题目的测试记录，按 created_at desc, id desc。
func (s *TestRecordService) ListByQuestion(ctx context.Context, questionID int64, page, pageSize int) ([]TestRecord, int64, error) {
	if questionID <= 0 {
		return nil, 0, ErrTestRecordNotFound
	}
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	total, err := s.repo.CountByQuestion(ctx, questionID)
	if err != nil {
		return nil, 0, err
	}
	items, err := s.repo.ListByQuestion(ctx, questionID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	if items == nil {
		items = []TestRecord{}
	}
	return items, total, nil
}

// GetByID 返回单条记录详情，附带文件元数据列表。
func (s *TestRecordService) GetByID(ctx context.Context, id int64) (*TestRecord, error) {
	if id <= 0 {
		return nil, ErrTestRecordNotFound
	}
	record, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	files, err := s.repo.ListFiles(ctx, id)
	if err != nil {
		return nil, err
	}
	if files == nil {
		files = []TestRecordFile{}
	}
	record.Files = files
	return record, nil
}

// OpenFile 读取一条记录下指定文件的二进制内容。
func (s *TestRecordService) OpenFile(ctx context.Context, recordID, fileID int64) (*TestRecordFileContent, error) {
	if recordID <= 0 || fileID <= 0 {
		return nil, ErrTestRecordFileNotFound
	}
	file, err := s.repo.GetFile(ctx, fileID)
	if err != nil {
		return nil, err
	}
	if file.RecordID != recordID {
		return nil, ErrTestRecordFileNotFound
	}
	full, err := safeTestRecordFilePath(s.root, file.Path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(full) //nolint:gosec // 路径已在 safeTestRecordFilePath 中限制在根目录内
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrTestRecordFileNotFound
		}
		return nil, err
	}
	return &TestRecordFileContent{
		MimeType: file.MimeType,
		FileName: fmt.Sprintf("test-%d-%d.%s", recordID, fileID, testRecordMediaExtension(file.MimeType)),
		Data:     data,
	}, nil
}

// DeleteQuestionRecords 在题目被删除前清理其历史记录占用的磁盘目录。
// 数据库行由外键 ON DELETE CASCADE 负责删除。
func (s *TestRecordService) DeleteQuestionRecords(ctx context.Context, questionID int64) error {
	if questionID <= 0 {
		return nil
	}
	ids, err := s.repo.ListRecordIDsByQuestion(ctx, questionID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		s.removeRecordDir(id)
	}
	return nil
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd backend && gofmt -w internal/service/account_test_record.go internal/service/account_test_record_test.go && go test -tags=unit ./internal/service/ -run 'TestAccountTestRecorder|TestDecodeTestRecordDataURL|TestSafeTestRecordFilePath|TestTestRecordMediaExtension' -v`
Expected: PASS —— 9 个测试全部 ok

- [ ] **Step 5: 提交**

```bash
git add backend/internal/service/account_test_record.go backend/internal/service/account_test_record_test.go
git commit -m "feat: 新增测试记录领域类型、仓储接口与结果 recorder"
```

---

### Task 3: 落盘与裁剪的服务层测试

**Files:**
- Modify: `backend/internal/service/account_test_record_test.go`

- [ ] **Step 1: 追加失败的测试**

在 `account_test_record_test.go` 末尾追加以下内容（stub 仓储 + 服务层行为）：

```go
// fakeTestRecordRepo 是内存版仓储，用于验证落盘、裁剪与读取行为。
type fakeTestRecordRepo struct {
	mu       sync.Mutex
	nextID   int64
	nextFile int64
	records  map[int64]TestRecord
	files    map[int64]TestRecordFile
}

func newFakeTestRecordRepo() *fakeTestRecordRepo {
	return &fakeTestRecordRepo{records: map[int64]TestRecord{}, files: map[int64]TestRecordFile{}}
}

func (r *fakeTestRecordRepo) Create(ctx context.Context, input TestRecordInput) (*TestRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	record := TestRecord{
		ID:                r.nextID,
		TestQuestionID:    input.TestQuestionID,
		QuestionTitle:     input.QuestionTitle,
		AccountName:       input.AccountName,
		Platform:          input.Platform,
		Model:             input.Model,
		ReasoningEffort:   input.ReasoningEffort,
		Status:            input.Status,
		ErrorMessage:      input.ErrorMessage,
		ResponseText:      input.ResponseText,
		HasText:           input.ResponseText != "",
		ResponseTruncated: input.ResponseTruncated,
		LatencyMs:         input.LatencyMs,
		StartedAt:         input.StartedAt,
		FinishedAt:        input.FinishedAt,
		CreatedAt:         time.Now().UTC(),
	}
	r.records[record.ID] = record
	return &record, nil
}

func (r *fakeTestRecordRepo) ListByQuestion(ctx context.Context, questionID int64, limit, offset int) ([]TestRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var all []TestRecord
	for _, record := range r.records {
		if record.TestQuestionID == questionID {
			all = append(all, record)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].ID > all[j].ID
		}
		return all[i].CreatedAt.After(all[j].CreatedAt)
	})
	if offset >= len(all) {
		return []TestRecord{}, nil
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	return all[offset:end], nil
}

func (r *fakeTestRecordRepo) CountByQuestion(ctx context.Context, questionID int64) (int64, error) {
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

func (r *fakeTestRecordRepo) GetByID(ctx context.Context, id int64) (*TestRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[id]
	if !ok {
		return nil, ErrTestRecordNotFound
	}
	return &record, nil
}

func (r *fakeTestRecordRepo) UpdateImageCount(ctx context.Context, id int64, imageCount int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	record, ok := r.records[id]
	if !ok {
		return ErrTestRecordNotFound
	}
	record.ImageCount = imageCount
	r.records[id] = record
	return nil
}

func (r *fakeTestRecordRepo) InsertFiles(ctx context.Context, recordID int64, files []TestRecordFile) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, file := range files {
		r.nextFile++
		file.ID = r.nextFile
		file.RecordID = recordID
		file.CreatedAt = time.Now().UTC()
		r.files[file.ID] = file
	}
	return nil
}

func (r *fakeTestRecordRepo) ListFiles(ctx context.Context, recordID int64) ([]TestRecordFile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []TestRecordFile
	for _, file := range r.files {
		if file.RecordID == recordID {
			out = append(out, file)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (r *fakeTestRecordRepo) GetFile(ctx context.Context, id int64) (*TestRecordFile, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	file, ok := r.files[id]
	if !ok {
		return nil, ErrTestRecordFileNotFound
	}
	return &file, nil
}

func (r *fakeTestRecordRepo) PruneByQuestion(ctx context.Context, keepCount int) ([]int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	byQuestion := map[int64][]int64{}
	for id, record := range r.records {
		byQuestion[record.TestQuestionID] = append(byQuestion[record.TestQuestionID], id)
	}
	var pruned []int64
	for _, ids := range byQuestion {
		sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
		if keepCount < 0 {
			keepCount = 0
		}
		for _, id := range ids[keepCount:] {
			for fileID, file := range r.files {
				if file.RecordID == id {
					delete(r.files, fileID)
				}
			}
			delete(r.records, id)
			pruned = append(pruned, id)
		}
	}
	return pruned, nil
}

func (r *fakeTestRecordRepo) ListRecordIDsByQuestion(ctx context.Context, questionID int64) ([]int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []int64
	for id, record := range r.records {
		if record.TestQuestionID == questionID {
			out = append(out, id)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out, nil
}

func newTestRecordService(t *testing.T, keep int) (*TestRecordService, *fakeTestRecordRepo) {
	t.Helper()
	repo := newFakeTestRecordRepo()
	return &TestRecordService{repo: repo, root: t.TempDir(), keep: keep}, repo
}

func TestTestRecordServiceRecordWritesMediaAndCountsImages(t *testing.T) {
	svc, repo := newTestRecordService(t, 20)

	err := svc.Record(context.Background(), TestRecordInput{
		TestQuestionID: 1,
		QuestionTitle:  "题目",
		Status:         TestRecordStatusSuccess,
		StartedAt:      time.Now(),
	}, []TestRecordMedia{
		{Kind: TestRecordFileKindImage, MimeType: "image/png", Data: []byte("png-bytes")},
		{Kind: TestRecordFileKindVideo, MimeType: "video/mp4", Data: []byte("mp4-bytes")},
	})
	require.NoError(t, err)

	record, err := repo.GetByID(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 1, record.ImageCount, "只有图片计入 image_count")

	files, err := repo.ListFiles(context.Background(), 1)
	require.NoError(t, err)
	require.Len(t, files, 2)

	data, err := os.ReadFile(filepath.Join(svc.root, files[0].Path))
	require.NoError(t, err)
	require.Equal(t, []byte("png-bytes"), data)
	require.Equal(t, "1/1.png", filepath.ToSlash(files[0].Path))
	require.Equal(t, "1/2.mp4", filepath.ToSlash(files[1].Path))
}

func TestTestRecordServiceRecordSurvivesMediaWriteFailure(t *testing.T) {
	svc, repo := newTestRecordService(t, 20)
	// 把落盘根目录换成普通文件，让建目录必然失败
	svc.root = filepath.Join(t.TempDir(), "not-a-dir")
	require.NoError(t, os.WriteFile(svc.root, []byte("x"), 0o600))

	err := svc.Record(context.Background(), TestRecordInput{
		TestQuestionID: 1,
		Status:         TestRecordStatusSuccess,
	}, []TestRecordMedia{{Kind: TestRecordFileKindImage, MimeType: "image/png", Data: []byte("x")}})
	require.NoError(t, err, "写盘失败不应阻断入库")

	record, err := repo.GetByID(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, 0, record.ImageCount)
}

func TestTestRecordServicePrunesOldRecordsAndFiles(t *testing.T) {
	svc, repo := newTestRecordService(t, 2)

	for i := 0; i < 3; i++ {
		require.NoError(t, svc.Record(context.Background(), TestRecordInput{
			TestQuestionID: 1,
			Status:         TestRecordStatusSuccess,
		}, []TestRecordMedia{{Kind: TestRecordFileKindImage, MimeType: "image/png", Data: []byte("x")}}))
	}

	_, total, err := svc.ListByQuestion(context.Background(), 1, 1, 20)
	require.NoError(t, err)
	require.Equal(t, int64(2), total)

	items, err := repo.ListByQuestion(context.Background(), 1, 20, 0)
	require.NoError(t, err)
	require.Len(t, items, 2)
	_, err = os.Stat(filepath.Join(svc.root, "1"))
	require.True(t, os.IsNotExist(err), "被裁剪记录的磁盘目录应被删除")
}

func TestTestRecordServiceGetByIDIncludesFiles(t *testing.T) {
	svc, _ := newTestRecordService(t, 20)
	require.NoError(t, svc.Record(context.Background(), TestRecordInput{
		TestQuestionID: 1,
		Status:         TestRecordStatusFailed,
		ErrorMessage:   "上游 502",
		ResponseText:   "部分输出",
	}, nil))

	record, err := svc.GetByID(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, "上游 502", record.ErrorMessage)
	require.Equal(t, "部分输出", record.ResponseText)
	require.True(t, record.HasText)
	require.NotNil(t, record.Files)
	require.Empty(t, record.Files)
}

func TestTestRecordServiceOpenFileRejectsForeignRecord(t *testing.T) {
	svc, _ := newTestRecordService(t, 20)
	require.NoError(t, svc.Record(context.Background(), TestRecordInput{
		TestQuestionID: 1, Status: TestRecordStatusSuccess,
	}, []TestRecordMedia{{Kind: TestRecordFileKindImage, MimeType: "image/png", Data: []byte("x")}}))

	_, err := svc.OpenFile(context.Background(), 999, 1)
	require.ErrorIs(t, err, ErrTestRecordFileNotFound)
}

func TestTestRecordServiceOpenFileReturnsContent(t *testing.T) {
	svc, _ := newTestRecordService(t, 20)
	require.NoError(t, svc.Record(context.Background(), TestRecordInput{
		TestQuestionID: 1, Status: TestRecordStatusSuccess,
	}, []TestRecordMedia{{Kind: TestRecordFileKindImage, MimeType: "image/png", Data: []byte("x")}}))

	content, err := svc.OpenFile(context.Background(), 1, 1)
	require.NoError(t, err)
	require.Equal(t, "image/png", content.MimeType)
	require.Equal(t, []byte("x"), content.Data)
	require.Equal(t, "test-1-1.png", content.FileName)
}

func TestTestRecordServiceDeleteQuestionRecordsRemovesDirs(t *testing.T) {
	svc, _ := newTestRecordService(t, 20)
	require.NoError(t, svc.Record(context.Background(), TestRecordInput{
		TestQuestionID: 5, Status: TestRecordStatusSuccess,
	}, []TestRecordMedia{{Kind: TestRecordFileKindImage, MimeType: "image/png", Data: []byte("x")}}))

	require.NoError(t, svc.DeleteQuestionRecords(context.Background(), 5))

	_, err := os.Stat(filepath.Join(svc.root, "1"))
	require.True(t, os.IsNotExist(err))
	require.NoError(t, svc.DeleteQuestionRecords(context.Background(), 0))
}
```

把该文件的 import 块补齐为：

```go
import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)
```

- [ ] **Step 2: 运行测试确认通过**

Run: `cd backend && gofmt -w internal/service/account_test_record_test.go && go test -tags=unit ./internal/service/ -run 'TestTestRecordService' -v`
Expected: PASS —— 7 个测试 ok

若 `TestTestRecordServiceRecordSurvivesMediaWriteFailure` 失败，检查 `writeMedia` 是否在 `os.MkdirAll` 失败时返回 nil 而不是继续写文件。

- [ ] **Step 3: 提交**

```bash
git add backend/internal/service/account_test_record_test.go
git commit -m "test: 覆盖测试记录落盘、裁剪与读取"
```

---

### Task 4: raw SQL 仓储

**Files:**
- Create: `backend/internal/repository/account_test_record_repo.go`

- [ ] **Step 1: 写实现**

创建 `backend/internal/repository/account_test_record_repo.go`：

```go
package repository

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// 列表查询不取 response_text，只取是否非空的布尔值，避免大 payload。
const accountTestRecordListSQL = `
SELECT id, test_question_id, question_title, account_id, account_name, platform, model,
       reasoning_effort, status, error_message, (response_text <> '') AS has_text,
       response_truncated, image_count, latency_ms, created_at
FROM account_test_records`

// 详情查询额外取完整文本与起止时间。
const accountTestRecordDetailSQL = `
SELECT id, test_question_id, question_title, account_id, account_name, platform, model,
       reasoning_effort, status, error_message, response_text, (response_text <> '') AS has_text,
       response_truncated, image_count, latency_ms, started_at, finished_at, created_at
FROM account_test_records`

const accountTestRecordFileSelectSQL = `
SELECT id, record_id, kind, mime_type, path, size_bytes, created_at
FROM account_test_record_files`

type accountTestRecordRepository struct {
	db *sql.DB
}

// NewTestRecordRepository 使用轻量 raw SQL 实现测试记录仓储（不引入新的 ORM 实体）。
func NewTestRecordRepository(db *sql.DB) service.TestRecordRepository {
	return &accountTestRecordRepository{db: db}
}

func (r *accountTestRecordRepository) Create(ctx context.Context, input service.TestRecordInput) (*service.TestRecord, error) {
	item, err := scanAccountTestRecordDetail(r.db.QueryRowContext(ctx, `
INSERT INTO account_test_records (
    test_question_id, question_title, account_id, account_name, platform, model,
    reasoning_effort, status, error_message, response_text, response_truncated,
    latency_ms, started_at, finished_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
RETURNING id, test_question_id, question_title, account_id, account_name, platform, model,
          reasoning_effort, status, error_message, response_text, (response_text <> '') AS has_text,
          response_truncated, image_count, latency_ms, started_at, finished_at, created_at`,
		input.TestQuestionID, input.QuestionTitle, nullableAccountID(input.AccountID),
		input.AccountName, input.Platform, input.Model, input.ReasoningEffort,
		input.Status, input.ErrorMessage, input.ResponseText, input.ResponseTruncated,
		input.LatencyMs, input.StartedAt, input.FinishedAt))
	if err != nil {
		return nil, translatePersistenceError(err, nil, nil)
	}
	return item, nil
}

func nullableAccountID(accountID int64) any {
	if accountID <= 0 {
		return nil
	}
	return accountID
}

func (r *accountTestRecordRepository) ListByQuestion(ctx context.Context, questionID int64, limit, offset int) ([]service.TestRecord, error) {
	rows, err := r.db.QueryContext(ctx, accountTestRecordListSQL+`
 WHERE test_question_id = $1
 ORDER BY created_at DESC, id DESC
 LIMIT $2 OFFSET $3`, questionID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.TestRecord, 0, 16)
	for rows.Next() {
		item, err := scanAccountTestRecordList(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *accountTestRecordRepository) CountByQuestion(ctx context.Context, questionID int64) (int64, error) {
	var total int64
	err := r.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM account_test_records WHERE test_question_id = $1`,
		questionID).Scan(&total)
	return total, err
}

func (r *accountTestRecordRepository) GetByID(ctx context.Context, id int64) (*service.TestRecord, error) {
	item, err := scanAccountTestRecordDetail(r.db.QueryRowContext(ctx,
		accountTestRecordDetailSQL+` WHERE id = $1`, id))
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrTestRecordNotFound, nil)
	}
	return item, nil
}

func (r *accountTestRecordRepository) UpdateImageCount(ctx context.Context, id int64, imageCount int) error {
	_, err := r.db.ExecContext(ctx,
		`UPDATE account_test_records SET image_count = $2 WHERE id = $1`, id, imageCount)
	return err
}

func (r *accountTestRecordRepository) InsertFiles(ctx context.Context, recordID int64, files []service.TestRecordFile) error {
	if len(files) == 0 {
		return nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO account_test_record_files (record_id, kind, mime_type, path, size_bytes)
VALUES ($1, $2, $3, $4, $5)`)
	if err != nil {
		return err
	}
	defer func() { _ = stmt.Close() }()

	for _, file := range files {
		if _, err := stmt.ExecContext(ctx, recordID, file.Kind, file.MimeType, file.Path, file.SizeBytes); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *accountTestRecordRepository) ListFiles(ctx context.Context, recordID int64) ([]service.TestRecordFile, error) {
	rows, err := r.db.QueryContext(ctx, accountTestRecordFileSelectSQL+`
 WHERE record_id = $1
 ORDER BY id ASC`, recordID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	files := make([]service.TestRecordFile, 0, 4)
	for rows.Next() {
		file, err := scanAccountTestRecordFile(rows)
		if err != nil {
			return nil, err
		}
		files = append(files, *file)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return files, nil
}

func (r *accountTestRecordRepository) GetFile(ctx context.Context, id int64) (*service.TestRecordFile, error) {
	file, err := scanAccountTestRecordFile(r.db.QueryRowContext(ctx,
		accountTestRecordFileSelectSQL+` WHERE id = $1`, id))
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrTestRecordFileNotFound, nil)
	}
	return file, nil
}

// PruneByQuestion 按题目保留最近 keepCount 条，返回被删除的记录 ID 供调用方清理磁盘文件。
func (r *accountTestRecordRepository) PruneByQuestion(ctx context.Context, keepCount int) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `
DELETE FROM account_test_records
WHERE id IN (
    SELECT id FROM (
        SELECT id, ROW_NUMBER() OVER (
            PARTITION BY test_question_id ORDER BY created_at DESC, id DESC
        ) AS rn
        FROM account_test_records
    ) ranked
    WHERE rn > $1
)
RETURNING id`, keepCount)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	pruned := make([]int64, 0, 4)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		pruned = append(pruned, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return pruned, nil
}

func (r *accountTestRecordRepository) ListRecordIDsByQuestion(ctx context.Context, questionID int64) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id FROM account_test_records WHERE test_question_id = $1 ORDER BY id ASC`, questionID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

type accountTestRecordScanner interface {
	Scan(dest ...any) error
}

func scanAccountTestRecordList(row accountTestRecordScanner) (*service.TestRecord, error) {
	var item service.TestRecord
	var accountID sql.NullInt64
	if err := row.Scan(
		&item.ID, &item.TestQuestionID, &item.QuestionTitle, &accountID, &item.AccountName,
		&item.Platform, &item.Model, &item.ReasoningEffort, &item.Status, &item.ErrorMessage,
		&item.HasText, &item.ResponseTruncated, &item.ImageCount, &item.LatencyMs, &item.CreatedAt,
	); err != nil {
		return nil, err
	}
	assignNullableAccountID(&item, accountID)
	return &item, nil
}

func scanAccountTestRecordDetail(row accountTestRecordScanner) (*service.TestRecord, error) {
	var item service.TestRecord
	var accountID sql.NullInt64
	if err := row.Scan(
		&item.ID, &item.TestQuestionID, &item.QuestionTitle, &accountID, &item.AccountName,
		&item.Platform, &item.Model, &item.ReasoningEffort, &item.Status, &item.ErrorMessage,
		&item.ResponseText, &item.HasText, &item.ResponseTruncated, &item.ImageCount, &item.LatencyMs,
		&item.StartedAt, &item.FinishedAt, &item.CreatedAt,
	); err != nil {
		return nil, err
	}
	assignNullableAccountID(&item, accountID)
	return &item, nil
}

func scanAccountTestRecordFile(row accountTestRecordScanner) (*service.TestRecordFile, error) {
	var file service.TestRecordFile
	if err := row.Scan(
		&file.ID, &file.RecordID, &file.Kind, &file.MimeType, &file.Path, &file.SizeBytes, &file.CreatedAt,
	); err != nil {
		return nil, err
	}
	return &file, nil
}

func assignNullableAccountID(item *service.TestRecord, accountID sql.NullInt64) {
	if !accountID.Valid {
		return
	}
	value := accountID.Int64
	item.AccountID = &value
}
```

- [ ] **Step 2: 编译校验**

Run: `cd backend && go build ./internal/repository/`
Expected: 无错误输出

- [ ] **Step 3: 提交**

```bash
git add backend/internal/repository/account_test_record_repo.go
git commit -m "feat: 新增测试记录 raw SQL 仓储"
```

---

### Task 5: recorder 接入账号测试链路

**Files:**
- Modify: `backend/internal/service/account_test_service.go`（`AccountTestOptions` 约 74-88 行、`AccountTestService` 结构与 `NewAccountTestService` 约 261 行、`TestAccountConnection` 约 408-432 行、`sendEvent` 约 3482-3496 行）
- Test: `backend/internal/service/account_test_service_history_test.go`（新建）

- [ ] **Step 1: 写失败的测试**

创建 `backend/internal/service/account_test_service_history_test.go`：

```go
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
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd backend && go test -tags=unit ./internal/service/ -run 'TestSendEvent|TestSuppressedCompletion' -v`
Expected: FAIL —— `TestSendEventFeedsRecorder` 中 recorder 不会累积事件（`input.ResponseText` 为空，且成功用例的 `input2.Status` 为 failed 而非 success）

`TestSuppressedCompletionIsNotRecorded` 是防回归用例：在 Step 5 之前它就会通过，这是预期的，它保证捕获逻辑不会被放到抑制判断之前。

- [ ] **Step 3: 扩展 `AccountTestOptions`**

把 `account_test_service.go` 约 74-88 行的结构改为：

```go
// AccountTestOptions carries optional media for admin connectivity tests.
// ImageDataURL / AudioDataURL are full data URLs (data:<mime>;base64,...).
// ReasoningEffort 会在写入任何 SSE 前由 resolveAccountTestReasoningEffort 校验。
// TestQuestionID > 0 时本次测试会写入该题目的测试历史（TestQuestionTitle 由 service 侧查库回填）。
type AccountTestOptions struct {
	ImageDataURL      string
	AudioDataURL      string
	ReasoningEffort   string
	TestQuestionID    int64
	TestQuestionTitle string
}
```

- [ ] **Step 4: 给 `AccountTestService` 加两个依赖**

在 `AccountTestService` 结构体字段末尾追加：

```go
	testQuestions    TestQuestionLookup
	testRecordWriter TestRecordWriter
```

把构造函数改为（在现有参数末尾追加两个）：

```go
func NewAccountTestService(
	accountRepo AccountRepository,
	geminiTokenProvider *GeminiTokenProvider,
	claudeTokenProvider *ClaudeTokenProvider,
	grokTokenProvider *GrokTokenProvider,
	antigravityGatewayService *AntigravityGatewayService,
	httpUpstream HTTPUpstream,
	cfg *config.Config,
	tlsFPProfileService *TLSFingerprintProfileService,
	testQuestions TestQuestionLookup,
	testRecordWriter TestRecordWriter,
) *AccountTestService {
	return &AccountTestService{
		accountRepo:               accountRepo,
		geminiTokenProvider:       geminiTokenProvider,
		claudeTokenProvider:       claudeTokenProvider,
		grokTokenProvider:         grokTokenProvider,
		antigravityGatewayService: antigravityGatewayService,
		httpUpstream:              httpUpstream,
		cfg:                       cfg,
		tlsFPProfileService:       tlsFPProfileService,
		testQuestions:             testQuestions,
		testRecordWriter:          testRecordWriter,
	}
}
```

- [ ] **Step 5: `sendEvent` 增加捕获**

把 `sendEvent` 改为（在 `json.Marshal` 之前捕获，保证只记录真正写给客户端的事件）：

```go
func (s *AccountTestService) sendEvent(c *gin.Context, event TestEvent) {
	if event.Type == "test_complete" {
		if suppress, ok := c.Get(accountTestSuppressCompletionContextKey); ok {
			if suppressCompletion, _ := suppress.(bool); suppressCompletion {
				return
			}
		}
	}
	// 测试历史：只累积真正写给客户端的事件，被抑制的事件不计入结果。
	if recorder := accountTestRecorderFromContext(c); recorder != nil {
		recorder.capture(event)
	}
	eventJSON, _ := json.Marshal(event)
	if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", eventJSON); err != nil {
		log.Printf("failed to write SSE event: %v", err)
		return
	}
	c.Writer.Flush()
}
```

- [ ] **Step 6: `TestAccountConnection` 建 recorder 并落库**

在 `testOpts.ReasoningEffort = effort` 与 `validateAccountTestCompactMode` 校验块之后、`account.IsSyntheticUITest()` 分支之前插入：

```go
	// 测试历史：只有带题目 ID 的测试才落库，定时任务与旧客户端不受影响。
	if testOpts.TestQuestionID > 0 {
		if s.testQuestions == nil {
			return s.sendErrorAndEnd(c, "测试题库不可用")
		}
		question, err := s.testQuestions.GetByID(ctx, testOpts.TestQuestionID)
		if err != nil {
			// 题目可能已被删除：在任何 SSE 写入前返回，handler 会映射为 404。
			logAccountTestError(c, err.Error())
			return err
		}
		testOpts.TestQuestionTitle = question.Title
		recorder := newAccountTestRecorder(TestRecordInput{
			TestQuestionID:  question.ID,
			QuestionTitle:   question.Title,
			AccountID:       account.ID,
			AccountName:     account.Name,
			Platform:        account.Platform,
			Model:           modelID,
			ReasoningEffort: effort,
		}, time.Now())
		c.Set(accountTestRecorderContextKey, recorder)
		defer s.persistAccountTestRecord(recorder)
	}
```

在 `sendErrorAndEnd` 之后追加：

```go
// persistAccountTestRecord 在测试结束时写入历史记录；失败只记日志，不影响测试结果。
func (s *AccountTestService) persistAccountTestRecord(recorder *accountTestRecorder) {
	if s.testRecordWriter == nil || recorder == nil {
		return
	}
	input, media := recorder.finish(time.Now())
	// 用 Background：响应已开始写出后请求 context 可能已被取消。
	ctx := context.Background()
	if err := s.testRecordWriter.Record(ctx, input, media); err != nil {
		logger.FromContext(ctx).Warn("account_test_record.persist_failed",
			zap.Int64("test_question_id", input.TestQuestionID),
			zap.Int64("account_id", input.AccountID),
			zap.Error(err))
		return
	}
	logger.FromContext(ctx).Info("account_test_record.persisted",
		zap.Int64("test_question_id", input.TestQuestionID),
		zap.Int64("account_id", input.AccountID),
		zap.String("status", input.Status),
		zap.Int64("latency_ms", input.LatencyMs),
		zap.Int("media_count", len(media)))
}
```

确认 `account_test_service.go` 的 import 里有 `context`、`time`、`github.com/Wei-Shaw/sub2api/internal/pkg/logger`、`go.uber.org/zap`；缺哪个补哪个。

- [ ] **Step 7: 给题库 service 加 `GetByID`**

在 `backend/internal/service/test_question_service.go` 的 `List` 之后追加：

```go
func (s *TestQuestionService) GetByID(ctx context.Context, id int64) (*TestQuestion, error) {
	if id <= 0 {
		return nil, ErrTestQuestionNotFound
	}
	return s.repo.GetByID(ctx, id)
}
```

- [ ] **Step 8: 运行测试确认通过**

Run: `cd backend && go test -tags=unit ./internal/service/ -run 'TestSendEvent|TestSuppressedCompletion' -v`
Expected: PASS —— 3 个测试 ok

- [ ] **Step 9: 全量编译校验（预期失败）**

Run: `cd backend && go build ./...`
Expected: FAIL —— `cmd/server/wire_gen.go` 尚未重新生成，`NewAccountTestService` 调用参数不匹配。这是预期错误，Task 6 修复。

- [ ] **Step 10: 提交**

```bash
git add backend/internal/service/account_test_service.go backend/internal/service/test_question_service.go backend/internal/service/account_test_service_history_test.go
git commit -m "feat: 账号测试结果接入测试历史 recorder"
```

---

### Task 6: 管理端接口、路由与依赖注入

**Files:**
- Create: `backend/internal/handler/admin/test_record_handler.go`
- Create: `backend/internal/handler/admin/test_record_handler_test.go`
- Modify: `backend/internal/handler/admin/account_handler.go`
- Modify: `backend/internal/handler/admin/test_question_handler_test.go`
- Modify: `backend/internal/service/test_question_service.go`
- Modify: `backend/internal/server/routes/admin.go`
- Modify: `backend/internal/handler/wire.go`、`backend/internal/handler/handler.go`、`backend/internal/service/wire.go`、`backend/internal/repository/wire.go`
- Regenerate: `backend/cmd/server/wire_gen.go`

- [ ] **Step 1: 写失败的 handler 测试**

创建 `backend/internal/handler/admin/test_record_handler_test.go`：

```go
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
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd backend && go test ./internal/handler/admin/ -run TestTestRecordHandler`
Expected: FAIL —— `undefined: NewTestRecordHandler`

- [ ] **Step 3: 写 handler**

创建 `backend/internal/handler/admin/test_record_handler.go`：

```go
package admin

import (
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// TestRecordHandler 提供测试历史的列表、详情与结果文件读取。
type TestRecordHandler struct {
	testRecordService *service.TestRecordService
}

func NewTestRecordHandler(testRecordService *service.TestRecordService) *TestRecordHandler {
	return &TestRecordHandler{testRecordService: testRecordService}
}

// ListByQuestion 返回某题目的测试历史，按时间倒序分页。
// GET /api/v1/admin/test-questions/:id/records
func (h *TestRecordHandler) ListByQuestion(c *gin.Context) {
	questionID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || questionID <= 0 {
		response.BadRequest(c, "题目 ID 不合法")
		return
	}

	page, pageSize := response.ParsePagination(c)
	items, total, err := h.testRecordService.ListByQuestion(c.Request.Context(), questionID, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Paginated(c, items, total, page, pageSize)
}

// Get 返回单条测试记录详情，含完整文本与文件元数据。
// GET /api/v1/admin/test-records/:id
func (h *TestRecordHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "记录 ID 不合法")
		return
	}

	record, err := h.testRecordService.GetByID(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, record)
}

// File 返回测试结果文件的二进制内容，?download=1 时作为附件下载。
// GET /api/v1/admin/test-records/:id/files/:file_id
func (h *TestRecordHandler) File(c *gin.Context) {
	recordID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || recordID <= 0 {
		response.BadRequest(c, "记录 ID 不合法")
		return
	}
	fileID, err := strconv.ParseInt(c.Param("file_id"), 10, 64)
	if err != nil || fileID <= 0 {
		response.BadRequest(c, "文件 ID 不合法")
		return
	}

	content, err := h.testRecordService.OpenFile(c.Request.Context(), recordID, fileID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if c.Query("download") == "1" {
		c.Header("Content-Disposition", `attachment; filename="`+content.FileName+`"`)
	}
	c.Data(http.StatusOK, content.MimeType, content.Data)
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd backend && go test ./internal/handler/admin/ -run TestTestRecordHandler -v`
Expected: PASS —— 5 个测试 ok

- [ ] **Step 5: 请求体加 `test_question_id`**

在 `account_handler.go` 的 `TestAccountRequest` 里追加字段：

```go
type TestAccountRequest struct {
	ModelID         string `json:"model_id"`
	Prompt          string `json:"prompt"`
	Mode            string `json:"mode"`
	ReasoningEffort string `json:"reasoning_effort"`
	ImageDataURL    string `json:"image_data_url"`
	AudioDataURL    string `json:"audio_data_url"`
	// TestQuestionID > 0 时本次测试会写入该题目的测试历史。
	TestQuestionID  int64  `json:"test_question_id"`
}
```

在 `Test` 方法构造 `service.AccountTestOptions{...}` 的那一行追加：

```go
		TestQuestionID: req.TestQuestionID,
```

- [ ] **Step 6: 题库删除时清理历史文件**

把 `test_question_service.go` 的结构体与构造函数改为：

```go
type TestQuestionService struct {
	repo    TestQuestionRepository
	records TestRecordStore
}

func NewTestQuestionService(repo TestQuestionRepository, records TestRecordStore) *TestQuestionService {
	return &TestQuestionService{repo: repo, records: records}
}
```

把 `Delete` 改为：

```go
func (s *TestQuestionService) Delete(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrTestQuestionNotFound
	}
	// 先清理该题目的历史结果文件；数据库行由外键级联删除。
	// 清理失败只记日志，不阻断删除。
	if s.records != nil {
		if err := s.records.DeleteQuestionRecords(ctx, id); err != nil {
			logger.FromContext(ctx).Warn("test_question.record_cleanup_failed",
				zap.Int64("question_id", id), zap.Error(err))
		}
	}
	return s.repo.Delete(ctx, id)
}
```

import 补上 `"github.com/Wei-Shaw/sub2api/internal/pkg/logger"` 与 `"go.uber.org/zap"`。

- [ ] **Step 7: 注册路由**

在 `routes/admin.go` 的 `registerTestQuestionRoutes` 中，`testQuestions.GET("", h.Admin.TestQuestion.List)` 之后加一行：

```go
		testQuestions.GET("/:id/records", h.Admin.TestRecord.ListByQuestion)
```

新增函数：

```go
func registerTestRecordRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	testRecords := admin.Group("/test-records")
	{
		testRecords.GET("/:id", h.Admin.TestRecord.Get)
		testRecords.GET("/:id/files/:file_id", h.Admin.TestRecord.File)
	}
}
```

在 `RegisterAdminRoutes` 中 `registerTestQuestionRoutes(admin, h)` 之后加：

```go
		registerTestRecordRoutes(admin, h)
```

- [ ] **Step 8: wire 注册**

`backend/internal/service/wire.go`：在 `NewTestQuestionService,` 之后加 `NewTestRecordService,`。

`backend/internal/repository/wire.go`：在 `NewTestQuestionRepository, // 题库仓储（raw SQL）` 之后加 `NewTestRecordRepository, // 测试记录仓储（raw SQL）`。

`backend/internal/handler/handler.go`：在 `TestQuestion           *admin.TestQuestionHandler` 之后加 `TestRecord             *admin.TestRecordHandler`。

`backend/internal/handler/wire.go`：
- `ProvideAdminHandlers` 参数列表在 `testQuestionHandler *admin.TestQuestionHandler,` 之后加 `testRecordHandler *admin.TestRecordHandler,`
- 返回值字面量在 `TestQuestion:           testQuestionHandler,` 之后加 `TestRecord:             testRecordHandler,`
- ProviderSet 在 `admin.NewTestQuestionHandler,` 之后加 `admin.NewTestRecordHandler,`

- [ ] **Step 9: 重新生成 wire_gen.go**

Run: `cd backend && go generate ./cmd/server`
Expected: 生成成功，`cmd/server/wire_gen.go` 出现 `NewTestRecordRepository` / `NewTestRecordService` / `NewTestRecordHandler` 调用，且 `NewTestQuestionService` 多传一个参数。

若本机没有安装 wire 工具导致生成失败，手工编辑 `cmd/server/wire_gen.go`：在题库三行附近补上等价的三行，并把 `service.NewTestQuestionService(testQuestionRepository)` 改为 `service.NewTestQuestionService(testQuestionRepository, testRecordService)`，其中 `testRecordService := service.NewTestRecordService(testRecordRepository)`。

- [ ] **Step 10: 修现有题库测试的构造函数调用**

把 `test_question_handler_test.go` 中的 `service.NewTestQuestionService(repo)` 改为 `service.NewTestQuestionService(repo, nil)`。

- [ ] **Step 11: 编译与测试**

Run: `cd backend && go build ./... && go test ./internal/handler/admin/ ./migrations/ && go test -tags=unit ./internal/service/ -run 'TestTestRecordService|TestAccountTestRecorder|TestSendEvent|TestSuppressedCompletion|TestDecodeTestRecordDataURL|TestSafeTestRecordFilePath|TestTestRecordMediaExtension'`
Expected: 全部通过

Run: `cd backend && grep -rn "NewAccountTestService(" --include=*.go | grep -v "func NewAccountTestService"`
Expected: 每个调用点都已补齐新参数；若有测试文件里的旧调用，按同样方式补 `nil, nil`

- [ ] **Step 12: 提交**

```bash
git add backend/internal/handler backend/internal/service/test_question_service.go backend/internal/service/wire.go backend/internal/repository/wire.go backend/internal/server/routes/admin.go backend/cmd/server/wire_gen.go
git commit -m "feat: 新增测试历史管理端接口与路由"
```

---

### Task 7: 前端共享的文件提取工具

**Files:**
- Create: `frontend/src/utils/testArtifact.ts`
- Create: `frontend/src/utils/__tests__/testArtifact.spec.ts`
- Modify: `frontend/src/components/admin/account/AccountTestModal.vue`

- [ ] **Step 1: 写失败的测试**

创建 `frontend/src/utils/__tests__/testArtifact.spec.ts`：

```ts
import { describe, expect, it } from 'vitest'
import { buildTestArtifact, sanitizeTestArtifactBaseName } from '../testArtifact'

describe('buildTestArtifact', () => {
  it('从多个代码块中优先提取 SVG', () => {
    const text = ['```json', '{"a":1}', '```', '```svg', '<svg viewBox="0 0 10 10"></svg>', '```'].join('\n')

    const artifact = buildTestArtifact(text, '题目')

    expect(artifact).not.toBeNull()
    expect(artifact?.fileName).toBe('题目.svg')
    expect(artifact?.mimeType).toBe('image/svg+xml')
    expect(artifact?.isSvg).toBe(true)
    expect(artifact?.content).toBe('<svg viewBox="0 0 10 10"></svg>')
  })

  it('识别 xml 围栏里的 SVG 根文档', () => {
    const artifact = buildTestArtifact('```xml\n<?xml version="1.0"?>\n<svg><rect/></svg>\n```', '题目')

    expect(artifact?.fileName).toBe('题目.svg')
  })

  it('HTML 文档内嵌 SVG 时保留整份 HTML', () => {
    const artifact = buildTestArtifact('```html\n<html><body><svg/></body></html>\n```', '题目')

    expect(artifact?.fileName).toBe('题目.html')
    expect(artifact?.mimeType).toBe('text/html')
  })

  it('JS 字符串里的 <svg> 不误判为 SVG', () => {
    const artifact = buildTestArtifact('```js\nconst s = "<svg/>"\n```', '题目')

    expect(artifact?.fileName).toBe('题目.js')
  })

  it('无代码块时回退为 txt', () => {
    const artifact = buildTestArtifact('这就是一段普通回答', '题目')

    expect(artifact?.fileName).toBe('题目.txt')
    expect(artifact?.mimeType).toBe('text/plain')
    expect(artifact?.isSvg).toBe(false)
  })

  it('空白输入返回 null', () => {
    expect(buildTestArtifact('   ', '题目')).toBeNull()
  })

  it('未知语言安全回退 txt', () => {
    const artifact = buildTestArtifact('```weirdlang\nsome content\n```', '题目')

    expect(artifact?.fileName).toBe('题目.txt')
  })
})

describe('sanitizeTestArtifactBaseName', () => {
  it('替换非法文件名字符并限制长度', () => {
    expect(sanitizeTestArtifactBaseName('a/b:c*d?e"f<g>h i')).toBe('a-b-c-d-e-f-g-h-i')
    expect(sanitizeTestArtifactBaseName('x'.repeat(120))).toHaveLength(80)
  })

  it('空值回退到 test-result', () => {
    expect(sanitizeTestArtifactBaseName('   ')).toBe('test-result')
  })
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd frontend && pnpm vitest run src/utils/__tests__/testArtifact.spec.ts`
Expected: FAIL —— `Failed to resolve import "../testArtifact"`

- [ ] **Step 3: 写实现**

创建 `frontend/src/utils/testArtifact.ts`：

```ts
/**
 * 账号测试结果的文件提取工具。
 * 从模型输出中识别可下载的代码文件（SVG/HTML/MD/…），供账号测试弹框与测试历史弹框共用。
 */

const artifactMimeTypes: Record<string, string> = {
  svg: 'image/svg+xml',
  html: 'text/html',
  htm: 'text/html',
  md: 'text/markdown',
  markdown: 'text/markdown',
  json: 'application/json',
  csv: 'text/csv',
  xml: 'application/xml',
  css: 'text/css',
  js: 'text/javascript',
  javascript: 'text/javascript',
  ts: 'text/typescript',
  typescript: 'text/typescript',
  yaml: 'application/yaml',
  yml: 'application/yaml',
  py: 'text/x-python',
  python: 'text/x-python',
  go: 'text/x-go',
  sh: 'text/x-shellscript',
  bash: 'text/x-shellscript',
  txt: 'text/plain',
  text: 'text/plain'
}

export interface TestArtifact {
  fileName: string
  mimeType: string
  content: string
  isSvg: boolean
  objectUrl: string
}

const fenceLanguage = (rawLanguage: string): string =>
  rawLanguage.trim().toLowerCase().split(/\s+/)[0] || ''

// 从任意文本中提取完整的 <svg>...</svg>，避免把前后说明文字写进 SVG 导致预览损坏。
const extractSvgRoot = (raw: string): string => {
  const startMatch = /<svg[\s>]/i.exec(raw)
  if (!startMatch) return ''
  const fromStart = raw.slice(startMatch.index)
  const endMatch = /<\/svg\s*>/i.exec(fromStart)
  if (!endMatch) return ''
  return fromStart.slice(0, endMatch.index + endMatch[0].length).trim()
}

// 去掉前导 XML 声明/注释/doctype（及注释间距），用于判断内容真正的根元素。
const stripProlog = (content: string): string => {
  let body = content.trim()
  for (;;) {
    const next = body
      .replace(/^<\?xml[\s\S]*?\?>/i, '')
      .replace(/^<!--[\s\S]*?-->/i, '')
      .replace(/^<!doctype[^>]*>/i, '')
      .trim()
    if (next === body) return body
    body = next
  }
}

// 根元素是 <svg> 才算 SVG 文档：HTML 文档里内嵌 <svg> 不能被截成 SVG。
const isSvgRootDocument = (content: string): boolean => /^<svg[\s>/]/i.test(stripProlog(content))

// 根元素是 <html>（doctype 已剥除）判定为 HTML 文档，需要整体保留。
const isHtmlRootDocument = (content: string): boolean => /^<html[\s>]/i.test(stripProlog(content))

// 只有 svg/xml/空/txt/text 及未知语言才可能是 SVG；明确写了 html/js/json 等已知类型整体保留。
const isSvgCandidateLanguage = (language: string): boolean =>
  language === '' || language === 'svg' || language === 'xml' || language === 'txt' || language === 'text' || !artifactMimeTypes[language]

const normalizeArtifactLanguage = (rawLanguage: string, content: string): string => {
  const language = fenceLanguage(rawLanguage)
  if (language && !isSvgCandidateLanguage(language)) return language
  if (language === 'svg' || isSvgRootDocument(content)) return 'svg'
  if (isHtmlRootDocument(content)) return 'html'
  if (language) return language
  return 'txt'
}

/** 把题目标题转成安全的文件名主干。 */
export function sanitizeTestArtifactBaseName(raw: string): string {
  return (
    raw
      .trim()
      .replace(/[\\/:*?"<>|\s]+/g, '-')
      .slice(0, 80) || 'test-result'
  )
}

/**
 * 从模型输出中提取可下载的文件；无法提取时返回 null。
 * baseName 用于生成文件名，一般传题目标题。
 */
export function buildTestArtifact(rawText: string, baseName: string): TestArtifact | null {
  const text = rawText.trim()
  if (!text) return null

  const fences: Array<{ language: string; content: string }> = []
  const fencePattern = /```([^\n`]*)\r?\n([\s\S]*?)```/g
  let match: RegExpExecArray | null
  while ((match = fencePattern.exec(text)) !== null) {
    fences.push({ language: match[1], content: match[2].trim() })
  }

  let selected: { language: string; content: string } | null = null
  // 多代码块时优先 SVG；但明确写了 html/js/json 等语言、或整份 HTML 文档（即便内嵌 <svg>）都不算 SVG 块。
  const svgFence = fences.find((fence) => {
    const language = fenceLanguage(fence.language)
    if (fence.content.length === 0 || !isSvgCandidateLanguage(language)) return false
    return (
      language === 'svg' ||
      isSvgRootDocument(fence.content) ||
      (extractSvgRoot(fence.content) !== '' && !isHtmlRootDocument(fence.content))
    )
  })
  if (svgFence) {
    selected = {
      language: 'svg',
      content: extractSvgRoot(svgFence.content) || svgFence.content
    }
  } else if (fences.length > 0) {
    selected = fences[0]
  } else {
    const svg = isHtmlRootDocument(text) ? '' : extractSvgRoot(text)
    selected = svg ? { language: 'svg', content: svg } : { language: 'txt', content: text }
  }

  if (!selected.content) return null

  const language = normalizeArtifactLanguage(selected.language, selected.content)
  // 已知代码/文本类型按原扩展名；未知语言安全回退 txt，不引入二进制类型。
  const extension = artifactMimeTypes[language] ? language : 'txt'
  const mimeType = artifactMimeTypes[extension] || 'text/plain'
  const isSvg = extension === 'svg'

  let objectUrl = ''
  if (isSvg && typeof URL.createObjectURL === 'function') {
    objectUrl = URL.createObjectURL(new Blob([selected.content], { type: mimeType }))
  }

  return {
    fileName: `${sanitizeTestArtifactBaseName(baseName)}.${extension}`,
    mimeType,
    content: selected.content,
    isSvg,
    objectUrl
  }
}

/** 释放 buildTestArtifact 为 SVG 创建的对象 URL。 */
export function revokeTestArtifact(artifact: TestArtifact | null): void {
  if (artifact?.objectUrl && typeof URL.revokeObjectURL === 'function') {
    URL.revokeObjectURL(artifact.objectUrl)
  }
}

/** 触发浏览器下载。 */
export function downloadTestArtifact(artifact: TestArtifact): void {
  if (typeof URL.createObjectURL !== 'function') return
  const url = URL.createObjectURL(new Blob([artifact.content], { type: artifact.mimeType }))
  const link = document.createElement('a')
  link.href = url
  link.download = artifact.fileName
  link.rel = 'noopener'
  link.click()
  window.setTimeout(() => URL.revokeObjectURL(url), 0)
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd frontend && pnpm vitest run src/utils/__tests__/testArtifact.spec.ts`
Expected: PASS —— 9 个测试 ok

- [ ] **Step 5: 改造 AccountTestModal 使用共享工具**

在 `AccountTestModal.vue` 中：

1. 删除本地 `interface TestArtifact`（约 503-509 行）。
2. 删除本地 `artifactMimeTypes`、`fenceLanguage`、`extractSvgRoot`、`stripProlog`、`isSvgRootDocument`、`isHtmlRootDocument`、`isSvgCandidateLanguage`、`normalizeArtifactLanguage`（约 1059-1131 行）。
3. 把 `buildArtifactFromText`、`clearArtifact`、`downloadArtifact`（约 1133-1224 行）替换为：

```ts
const buildArtifactFromText = (rawText: string) => {
  const built = buildTestArtifact(rawText, selectedQuestion.value?.title || 'test-result')
  if (!built) {
    artifactError.value = t('admin.accounts.fileArtifact.none')
    return
  }
  artifact.value = built
  artifactError.value = ''
  console.info('[账号测试] 已从响应中提取文件', built.fileName)
}

const clearArtifact = () => {
  revokeTestArtifact(artifact.value)
  artifact.value = null
  artifactError.value = ''
}

const downloadArtifact = () => {
  const current = artifact.value
  if (!current) return
  if (typeof URL.createObjectURL !== 'function') {
    artifactError.value = t('admin.accounts.fileArtifact.none')
    return
  }
  downloadTestArtifact(current)
}
```

4. 在 `<script setup>` 的 import 区加：

```ts
import {
  buildTestArtifact,
  downloadTestArtifact,
  revokeTestArtifact,
  type TestArtifact
} from '@/utils/testArtifact'
```

5. 删除前先执行 `grep -n "artifactMimeTypes\|normalizeArtifactLanguage\|extractSvgRoot\|isHtmlRootDocument\|isSvgRootDocument\|isSvgCandidateLanguage\|fenceLanguage\|stripProlog" frontend/src/components/admin/account/AccountTestModal.vue`，确认删除范围内没有其他引用；有引用则把该引用改为从 `@/utils/testArtifact` 引入对应导出。

- [ ] **Step 6: 运行账号测试弹框的既有测试**

Run: `cd frontend && pnpm vitest run src/components/admin/account/__tests__/`
Expected: PASS —— `AccountTestModal.questions.spec.ts` 等 10 个用例全绿，证明抽取没有改变行为

- [ ] **Step 7: 类型检查**

Run: `cd frontend && pnpm run typecheck`
Expected: 无错误

- [ ] **Step 8: 提交**

```bash
git add frontend/src/utils/testArtifact.ts frontend/src/utils/__tests__/testArtifact.spec.ts frontend/src/components/admin/account/AccountTestModal.vue
git commit -m "refactor: 抽出账号测试结果文件提取工具"
```

---

### Task 8: 前端 API 客户端

**Files:**
- Create: `frontend/src/api/admin/testRecords.ts`
- Modify: `frontend/src/api/admin/testQuestions.ts`
- Modify: `frontend/src/api/admin/index.ts`

- [ ] **Step 1: 写 API 客户端**

创建 `frontend/src/api/admin/testRecords.ts`：

```ts
/**
 * 管理员测试历史 API
 * 记录由账号测试在服务端落库，本模块只做只读查询与结果文件读取。
 */

import { apiClient } from '../client'

export type TestRecordStatus = 'success' | 'failed'
export type TestRecordFileKind = 'image' | 'audio' | 'video'

export interface TestRecordFile {
  id: number
  kind: TestRecordFileKind
  mime_type: string
  size_bytes: number
}

export interface TestRecord {
  id: number
  test_question_id: number
  question_title: string
  account_id: number | null
  account_name: string
  platform: string
  model: string
  reasoning_effort: string
  status: TestRecordStatus
  error_message: string
  response_text?: string
  has_text: boolean
  response_truncated: boolean
  image_count: number
  latency_ms: number
  started_at: string
  finished_at: string
  created_at: string
  files?: TestRecordFile[]
}

export interface TestRecordPage {
  items: TestRecord[]
  total: number
  page: number
  page_size: number
  pages: number
}

/** 分页查询某道题目的测试历史（按时间倒序） */
export async function listByQuestion(
  questionId: number,
  page = 1,
  pageSize = 20
): Promise<TestRecordPage> {
  const { data } = await apiClient.get<TestRecordPage>(
    `/admin/test-questions/${questionId}/records`,
    { params: { page, page_size: pageSize } }
  )
  return data
}

/** 查询单条测试记录详情（含完整文本与文件元数据） */
export async function get(id: number): Promise<TestRecord> {
  const { data } = await apiClient.get<TestRecord>(`/admin/test-records/${id}`)
  return data
}

/** 读取结果文件二进制内容 */
export async function fetchFile(recordId: number, fileId: number): Promise<Blob> {
  const response = await apiClient.get(`/admin/test-records/${recordId}/files/${fileId}`, {
    responseType: 'blob'
  })
  return response.data as Blob
}

export const testRecordsAPI = {
  listByQuestion,
  get,
  fetchFile
}
```

- [ ] **Step 2: 在题库 API 上暴露 records()**

把 `testQuestions.ts` 的 import 改为：

```ts
import { apiClient } from '../client'
import { testRecordsAPI, type TestRecordPage } from './testRecords'
```

追加函数并改导出对象：

```ts
/** 分页查询某道题目的测试历史 */
export async function records(questionId: number, page = 1, pageSize = 20): Promise<TestRecordPage> {
  return testRecordsAPI.listByQuestion(questionId, page, pageSize)
}

export const testQuestionsAPI = {
  list,
  create,
  update,
  remove,
  records
}
```

- [ ] **Step 3: 在 admin 索引里汇总导出**

在 `frontend/src/api/admin/index.ts` 中 import `testRecordsAPI`，并在导出的 `adminAPI` 对象里 `testQuestions: testQuestionsAPI,` 之后加一行：

```ts
  testRecords: testRecordsAPI,
```

- [ ] **Step 4: 类型检查**

Run: `cd frontend && pnpm run typecheck`
Expected: 无错误

- [ ] **Step 5: 提交**

```bash
git add frontend/src/api/admin/testRecords.ts frontend/src/api/admin/testQuestions.ts frontend/src/api/admin/index.ts
git commit -m "feat: 新增测试历史前端 API 客户端"
```

---

### Task 9: i18n 文案

**Files:**
- Modify: `frontend/src/i18n/locales/zh/admin/testQuestions.ts`
- Modify: `frontend/src/i18n/locales/en/admin/testQuestions.ts`

- [ ] **Step 1: 补中文文案**

在 `zh/admin/testQuestions.ts` 的 `testQuestions` 对象里，`form: {...}` 之后追加同级 `history` 命名空间：

```ts
    history: {
      action: '测试历史',
      title: '测试历史',
      loadFailed: '加载测试历史失败',
      detailFailed: '加载测试记录详情失败',
      empty: '暂无测试记录',
      emptyHint: '这道题目还没有执行过账号测试。',
      columns: {
        createdAt: '记录时间',
        account: '账号',
        model: '模型',
        reasoningEffort: '思考强度',
        latency: '测试时长',
        status: '状态',
        result: '结果'
      },
      status: {
        success: '成功',
        failed: '失败'
      },
      viewText: '查看文本',
      viewImages: '查看图片',
      downloadFile: '下载文件',
      viewError: '查看错误',
      truncated: '内容较长，仅保留前 64KB。',
      noArtifact: '未从响应中提取到可下载文件。',
      textResult: '文本结果',
      errorDetail: '错误详情',
      imagesResult: '图片结果',
      downloadFailed: '下载结果失败',
      imageLoadFailed: '加载图片失败'
    }
```

- [ ] **Step 2: 补英文文案**

在 `en/admin/testQuestions.ts` 中追加同名同结构：

```ts
    history: {
      action: 'Test History',
      title: 'Test History',
      loadFailed: 'Failed to load test history',
      detailFailed: 'Failed to load test record detail',
      empty: 'No test records',
      emptyHint: 'This question has not been used for an account test yet.',
      columns: {
        createdAt: 'Recorded At',
        account: 'Account',
        model: 'Model',
        reasoningEffort: 'Reasoning Effort',
        latency: 'Duration',
        status: 'Status',
        result: 'Result'
      },
      status: {
        success: 'Success',
        failed: 'Failed'
      },
      viewText: 'View Text',
      viewImages: 'View Images',
      downloadFile: 'Download File',
      viewError: 'View Error',
      truncated: 'Content is long, only the first 64KB is kept.',
      noArtifact: 'No downloadable file was found in the response.',
      textResult: 'Text Result',
      errorDetail: 'Error Detail',
      imagesResult: 'Images',
      downloadFailed: 'Failed to download the result',
      imageLoadFailed: 'Failed to load the image'
    }
```

- [ ] **Step 3: 校验 key 对齐**

Run: `cd frontend && diff <(grep -o "^      [a-zA-Z]*:" src/i18n/locales/zh/admin/testQuestions.ts) <(grep -o "^      [a-zA-Z]*:" src/i18n/locales/en/admin/testQuestions.ts) && echo "zh/en keys 对齐"`
Expected: 输出 `zh/en keys 对齐`

- [ ] **Step 4: 提交**

```bash
git add frontend/src/i18n/locales/zh/admin/testQuestions.ts frontend/src/i18n/locales/en/admin/testQuestions.ts
git commit -m "feat: 补充测试历史中英文案"
```

---

### Task 10: 测试历史弹框组件

**Files:**
- Create: `frontend/src/components/admin/testquestion/TestRecordHistoryModal.vue`
- Test: `frontend/src/components/admin/testquestion/__tests__/TestRecordHistoryModal.spec.ts`

弹框 props 为 `show: boolean` 与 `question: TestQuestion | null`（复用 `@/api/admin/testQuestions` 的类型）。

- [ ] **Step 1: 写失败的测试**

创建 `frontend/src/components/admin/testquestion/__tests__/TestRecordHistoryModal.spec.ts`：

```ts
import { flushPromises, mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import { defineComponent, h } from 'vue'
import TestRecordHistoryModal from '../TestRecordHistoryModal.vue'
import type { TestRecord } from '@/api/admin/testRecords'
import type { TestQuestion } from '@/api/admin/testQuestions'

const { listByQuestion, get, fetchFile, showError, copyToClipboard } = vi.hoisted(() => ({
  listByQuestion: vi.fn(),
  get: vi.fn(),
  fetchFile: vi.fn(),
  showError: vi.fn(),
  copyToClipboard: vi.fn()
}))

vi.mock('@/api/admin/testRecords', () => ({
  testRecordsAPI: { listByQuestion, get, fetchFile }
}))

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({ showError })
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard })
}))

vi.mock('vue-i18n', async (importOriginal) => ({
  ...(await importOriginal<typeof import('vue-i18n')>()),
  useI18n: () => ({ t: (key: string) => key })
}))

const DataTableStub = defineComponent({
  props: { data: { type: Array, default: () => [] } },
  setup(props, { slots }) {
    return () =>
      h('div', [
        ...(props.data as Array<Record<string, unknown>>).length === 0
          ? [slots.empty?.()]
          : (props.data as Array<Record<string, unknown>>).map((row) =>
              h('div', { class: 'record-row' }, [
                h('span', { class: 'record-account' }, String(row.account_name)),
                slots['cell-actions']?.({ row })
              ])
            )
      ])
  }
})

const BaseDialogStub = defineComponent({
  props: { show: Boolean, title: String },
  setup(props, { slots }) {
    return () =>
      props.show
        ? h('div', { class: 'dialog-stub' }, [
            slots.default?.(),
            slots.footer?.()
          ])
        : null
  }
})

const PaginationStub = defineComponent({
  props: { page: Number, total: Number, pageSize: Number },
  emits: ['update:page'],
  setup(props, { emit }) {
    return () =>
      h('button', { class: 'next-page', onClick: () => emit('update:page', (props.page ?? 1) + 1) }, 'next')
  }
})

function question(): TestQuestion {
  return {
    id: 9,
    title: '鹈鹕骑自行车',
    prompt: '画一只骑自行车的鹈鹕',
    type: 'file',
    created_at: '2026-10-20T00:00:00Z',
    updated_at: '2026-10-20T00:00:00Z'
  }
}

function record(overrides: Partial<TestRecord> = {}): TestRecord {
  return {
    id: 1,
    test_question_id: 9,
    question_title: '鹈鹕骑自行车',
    account_id: 3,
    account_name: 'grok-账号',
    platform: 'grok',
    model: 'grok-4.7',
    reasoning_effort: 'high',
    status: 'success',
    error_message: '',
    has_text: true,
    response_truncated: false,
    image_count: 1,
    latency_ms: 1234,
    started_at: '2026-10-21T12:00:00Z',
    finished_at: '2026-10-21T12:00:01Z',
    created_at: '2026-10-21T12:00:01Z',
    ...overrides
  }
}

function mountModal() {
  return mount(TestRecordHistoryModal, {
    props: { show: true, question: question() },
    global: {
      stubs: {
        DataTable: DataTableStub,
        BaseDialog: BaseDialogStub,
        Pagination: PaginationStub,
        EmptyState: { template: '<div class="empty-stub"></div>' },
        Icon: true
      }
    }
  })
}

describe('测试历史弹框', () => {
  beforeEach(() => {
    listByQuestion.mockReset().mockResolvedValue({
      items: [
        record({ id: 2, account_name: '成功账号', image_count: 1 }),
        record({
          id: 1,
          account_name: '失败账号',
          status: 'failed',
          error_message: '上游 502',
          has_text: false,
          image_count: 0
        })
      ],
      total: 2,
      page: 1,
      page_size: 20,
      pages: 1
    })
    get.mockReset().mockResolvedValue(
      record({
        id: 2,
        response_text: '```svg\n<svg></svg>\n```',
        files: [{ id: 10, kind: 'image', mime_type: 'image/png', size_bytes: 3 }]
      })
    )
    fetchFile.mockReset().mockResolvedValue(new Blob(['img']))
    showError.mockReset()
    copyToClipboard.mockReset()
    Object.defineProperty(URL, 'createObjectURL', { value: vi.fn(() => 'blob:mock'), configurable: true })
    Object.defineProperty(URL, 'revokeObjectURL', { value: vi.fn(), configurable: true })
  })

  it('打开时按题目倒序加载第一页并渲染结果按钮', async () => {
    const wrapper = mountModal()
    await flushPromises()

    expect(listByQuestion).toHaveBeenCalledWith(9, 1, 20)
    expect(wrapper.text()).toContain('成功账号')
    expect(wrapper.text()).toContain('失败账号')
    expect(wrapper.findAll('.record-row')).toHaveLength(2)

    const titles = wrapper.findAll('button').map((b) => b.attributes('title'))
    expect(titles).toContain('admin.testQuestions.history.viewText')
    expect(titles).toContain('admin.testQuestions.history.viewImages')
    expect(titles).toContain('admin.testQuestions.history.downloadFile')
    expect(titles).toContain('admin.testQuestions.history.viewError')
  })

  it('翻页时按新页码重新请求', async () => {
    const wrapper = mountModal()
    await flushPromises()

    await wrapper.find('.next-page').trigger('click')
    await flushPromises()

    expect(listByQuestion).toHaveBeenLastCalledWith(9, 2, 20)
  })

  it('点击查看文本打开详情并可复制与下载提取文件', async () => {
    const wrapper = mountModal()
    await flushPromises()

    const viewText = wrapper
      .findAll('button')
      .find((b) => b.attributes('title') === 'admin.testQuestions.history.viewText')
    await viewText!.trigger('click')
    await flushPromises()

    expect(get).toHaveBeenCalledWith(2)
    expect(wrapper.find('.detail-text').text()).toContain('<svg>')

    await wrapper.find('.copy-text').trigger('click')
    expect(copyToClipboard).toHaveBeenCalled()

    await wrapper.find('.detail-download').trigger('click')
    expect(URL.createObjectURL).toHaveBeenCalled()
  })

  it('点击查看错误展示错误文本', async () => {
    get.mockResolvedValueOnce(
      record({ status: 'failed', error_message: '上游 502', response_text: '' })
    )
    const wrapper = mountModal()
    await flushPromises()

    const viewError = wrapper
      .findAll('button')
      .find((b) => b.attributes('title') === 'admin.testQuestions.history.viewError')
    await viewError!.trigger('click')
    await flushPromises()

    expect(wrapper.find('.detail-text').text()).toContain('上游 502')
  })

  it('点击查看图片加载结果文件', async () => {
    const wrapper = mountModal()
    await flushPromises()

    const viewImages = wrapper
      .findAll('button')
      .find((b) => b.attributes('title') === 'admin.testQuestions.history.viewImages')
    await viewImages!.trigger('click')
    await flushPromises()

    expect(fetchFile).toHaveBeenCalledWith(2, 10)
    expect(wrapper.find('.detail-images img').exists()).toBe(true)
  })

  it('没有记录时展示空态', async () => {
    listByQuestion.mockResolvedValueOnce({
      items: [],
      total: 0,
      page: 1,
      page_size: 20,
      pages: 1
    })
    const wrapper = mountModal()
    await flushPromises()

    expect(wrapper.find('.empty-stub').exists()).toBe(true)
  })

  it('加载失败时展示错误并可重试', async () => {
    listByQuestion.mockRejectedValueOnce(new Error('boom'))
    const wrapper = mountModal()
    await flushPromises()

    expect(showError).toHaveBeenCalledWith('admin.testQuestions.history.loadFailed')
    expect(wrapper.text()).toContain('admin.testQuestions.history.loadFailed')
  })
})
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd frontend && pnpm vitest run src/components/admin/testquestion/__tests__/TestRecordHistoryModal.spec.ts`
Expected: FAIL —— `Failed to resolve import "../TestRecordHistoryModal.vue"`

- [ ] **Step 3: 写组件**

创建 `frontend/src/components/admin/testquestion/TestRecordHistoryModal.vue`：

```vue
<template>
  <BaseDialog :show="show" :title="dialogTitle" width="extra-wide" @close="emit('close')">
    <div class="space-y-3">
      <div
        v-if="loadError"
        class="flex flex-wrap items-center justify-between gap-3 rounded-lg border border-red-200 bg-red-50 px-4 py-3 text-sm text-red-700 dark:border-red-900/60 dark:bg-red-950/30 dark:text-red-300"
      >
        <span>{{ loadError }}</span>
        <button type="button" class="btn btn-secondary btn-sm" @click="loadRecords">
          {{ t('admin.testQuestions.retry') }}
        </button>
      </div>

      <DataTable
        :columns="columns"
        :data="records"
        :loading="loading"
        :sticky-actions-column="false"
      >
        <template #cell-created_at="{ value }">
          <span class="text-sm text-gray-500 dark:text-dark-400">{{ formatDateTime(value) }}</span>
        </template>

        <template #cell-account_name="{ row }">
          <div class="min-w-0">
            <div class="truncate font-medium text-gray-900 dark:text-white">
              {{ row.account_name || '-' }}
            </div>
            <div class="mt-1 text-xs text-gray-500 dark:text-dark-400">{{ row.platform || '-' }}</div>
          </div>
        </template>

        <template #cell-model="{ value }">
          <span class="font-mono text-xs text-gray-600 dark:text-gray-300">{{ value || '-' }}</span>
        </template>

        <template #cell-reasoning_effort="{ value }">
          <span class="text-sm text-gray-600 dark:text-gray-300">{{ value || '-' }}</span>
        </template>

        <template #cell-latency_ms="{ value }">
          <span class="text-sm text-gray-600 dark:text-gray-300">{{ formatLatency(value) }}</span>
        </template>

        <template #cell-status="{ row }">
          <span
            :class="['badge', row.status === 'success' ? 'badge-success' : 'badge-danger']"
            :title="row.status === 'failed' ? row.error_message : ''"
          >
            {{ row.status === 'success'
              ? t('admin.testQuestions.history.status.success')
              : t('admin.testQuestions.history.status.failed') }}
          </span>
        </template>

        <template #cell-actions="{ row }">
          <div class="flex flex-wrap items-center gap-1">
            <button
              v-if="row.has_text"
              type="button"
              class="btn btn-secondary btn-sm"
              :title="t('admin.testQuestions.history.viewText')"
              @click="openDetail(row, 'text')"
            >
              <Icon name="document" size="sm" class="mr-1" />
              {{ t('admin.testQuestions.history.viewText') }}
            </button>
            <button
              v-if="row.image_count > 0"
              type="button"
              class="btn btn-secondary btn-sm"
              :title="t('admin.testQuestions.history.viewImages')"
              @click="openDetail(row, 'images')"
            >
              <Icon name="eye" size="sm" class="mr-1" />
              {{ t('admin.testQuestions.history.viewImages') }}
            </button>
            <button
              v-if="row.has_text"
              type="button"
              class="btn btn-secondary btn-sm"
              :title="t('admin.testQuestions.history.downloadFile')"
              @click="downloadFromRecord(row)"
            >
              <Icon name="download" size="sm" class="mr-1" />
              {{ t('admin.testQuestions.history.downloadFile') }}
            </button>
            <button
              v-if="row.status === 'failed' && row.error_message"
              type="button"
              class="btn btn-secondary btn-sm"
              :title="t('admin.testQuestions.history.viewError')"
              @click="openDetail(row, 'error')"
            >
              <Icon name="x" size="sm" class="mr-1" />
              {{ t('admin.testQuestions.history.viewError') }}
            </button>
          </div>
        </template>

        <template #empty>
          <EmptyState
            :title="t('admin.testQuestions.history.empty')"
            :description="t('admin.testQuestions.history.emptyHint')"
          />
        </template>
      </DataTable>

      <Pagination
        v-if="total > 0"
        :page="page"
        :total="total"
        :page-size="pageSize"
        @update:page="onPageChange"
        @update:pageSize="onPageSizeChange"
      />
    </div>

    <template #footer>
      <div class="flex justify-end">
        <button type="button" class="btn btn-secondary" @click="emit('close')">
          {{ t('common.close') }}
        </button>
      </div>
    </template>
  </BaseDialog>

  <BaseDialog
    :show="detailVisible"
    :title="detailTitle"
    width="wide"
    :z-index="60"
    @close="closeDetail"
  >
    <div class="space-y-3">
      <p v-if="detailLoading" class="text-sm text-gray-500 dark:text-dark-400">…</p>

      <template v-else-if="detailMode === 'text'">
        <div class="flex flex-wrap items-center justify-between gap-2">
          <p v-if="detailRecord?.response_truncated" class="text-xs text-orange-600 dark:text-orange-400">
            {{ t('admin.testQuestions.history.truncated') }}
          </p>
          <div class="ml-auto flex items-center gap-2">
            <button type="button" class="btn btn-secondary btn-sm copy-text" @click="copyDetailText">
              <Icon name="copy" size="sm" class="mr-1" />
              {{ t('common.copy') }}
            </button>
            <button
              v-if="detailArtifact"
              type="button"
              class="btn btn-secondary btn-sm detail-download"
              @click="downloadDetailArtifact"
            >
              <Icon name="download" size="sm" class="mr-1" />
              {{ t('admin.testQuestions.history.downloadFile') }}
            </button>
          </div>
        </div>
        <img
          v-if="detailArtifact && detailArtifact.isSvg && detailArtifact.objectUrl"
          :src="detailArtifact.objectUrl"
          :alt="detailArtifact.fileName"
          class="max-h-[320px] w-full rounded-lg border border-gray-200 bg-gray-50 object-contain dark:border-dark-500 dark:bg-dark-800"
        />
        <pre
          class="detail-text max-h-[360px] overflow-auto rounded-lg bg-gray-900 p-3 font-mono text-xs text-gray-100"
        >{{ detailRecord?.response_text || '' }}</pre>
      </template>

      <template v-else-if="detailMode === 'error'">
        <pre
          class="detail-text max-h-[360px] overflow-auto rounded-lg bg-gray-900 p-3 font-mono text-xs text-gray-100"
        >{{ detailRecord?.error_message || '' }}</pre>
      </template>

      <template v-else-if="detailMode === 'images'">
        <p v-if="detailImageError" class="text-xs text-orange-600 dark:text-orange-400">
          {{ detailImageError }}
        </p>
        <div class="detail-images flex flex-wrap gap-3">
          <button
            v-for="(image, index) in detailImages"
            :key="image.url"
            type="button"
            class="overflow-hidden rounded-lg border"
            :class="index === activeImageIndex ? 'border-primary-500' : 'border-gray-200 dark:border-dark-500'"
            @click="activeImageIndex = index"
          >
            <img :src="image.url" :alt="image.mimeType" class="h-20 w-20 object-cover" />
          </button>
        </div>
        <img
          v-if="detailImages[activeImageIndex]"
          :src="detailImages[activeImageIndex].url"
          :alt="detailImages[activeImageIndex].mimeType"
          class="max-h-[360px] w-full rounded-lg border border-gray-200 bg-gray-50 object-contain dark:border-dark-500 dark:bg-dark-800"
        />
      </template>
    </div>

    <template #footer>
      <div class="flex justify-end">
        <button type="button" class="btn btn-secondary" @click="closeDetail">
          {{ t('common.close') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { Column } from '@/components/common/types'
import { testRecordsAPI, type TestRecord } from '@/api/admin/testRecords'
import type { TestQuestion } from '@/api/admin/testQuestions'
import { useAppStore } from '@/stores/app'
import { useClipboard } from '@/composables/useClipboard'
import { formatDateTime } from '@/utils/format'
import {
  buildTestArtifact,
  downloadTestArtifact,
  revokeTestArtifact,
  type TestArtifact
} from '@/utils/testArtifact'

import BaseDialog from '@/components/common/BaseDialog.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import EmptyState from '@/components/common/EmptyState.vue'
import Icon from '@/components/icons/Icon.vue'

type DetailMode = 'text' | 'error' | 'images'

interface DetailImage {
  url: string
  mimeType: string
}

const props = defineProps<{
  show: boolean
  question: TestQuestion | null
}>()

const emit = defineEmits<{
  (e: 'close'): void
}>()

const { t } = useI18n()
const appStore = useAppStore()
const { copyToClipboard } = useClipboard()

const records = ref<TestRecord[]>([])
const total = ref(0)
const page = ref(1)
const pageSize = ref(20)
const loading = ref(false)
const loadError = ref('')

const detailVisible = ref(false)
const detailMode = ref<DetailMode>('text')
const detailLoading = ref(false)
const detailRecord = ref<TestRecord | null>(null)
const detailArtifact = ref<TestArtifact | null>(null)
const detailImages = ref<DetailImage[]>([])
const detailImageError = ref('')
const activeImageIndex = ref(0)

const questionID = computed(() => props.question?.id ?? 0)
const dialogTitle = computed(() => {
  const title = props.question?.title
  return title
    ? `${t('admin.testQuestions.history.title')} · ${title}`
    : t('admin.testQuestions.history.title')
})
const detailTitle = computed(() => {
  if (detailMode.value === 'text') return t('admin.testQuestions.history.textResult')
  if (detailMode.value === 'error') return t('admin.testQuestions.history.errorDetail')
  return t('admin.testQuestions.history.imagesResult')
})

const columns = computed<Column[]>(() => [
  { key: 'created_at', label: t('admin.testQuestions.history.columns.createdAt') },
  { key: 'account_name', label: t('admin.testQuestions.history.columns.account') },
  { key: 'model', label: t('admin.testQuestions.history.columns.model') },
  { key: 'reasoning_effort', label: t('admin.testQuestions.history.columns.reasoningEffort') },
  { key: 'latency_ms', label: t('admin.testQuestions.history.columns.latency') },
  { key: 'status', label: t('admin.testQuestions.history.columns.status') },
  { key: 'actions', label: t('admin.testQuestions.history.columns.result') }
])

function formatLatency(latencyMs: number): string {
  const value = Number(latencyMs) || 0
  if (value < 1000) return `${value} ms`
  return `${(value / 1000).toFixed(1)} s`
}

async function loadRecords() {
  if (!questionID.value) return
  loading.value = true
  loadError.value = ''
  try {
    const result = await testRecordsAPI.listByQuestion(questionID.value, page.value, pageSize.value)
    records.value = result.items ?? []
    total.value = result.total ?? 0
  } catch (error) {
    console.error('[测试历史] 加载失败', error)
    records.value = []
    total.value = 0
    loadError.value = t('admin.testQuestions.history.loadFailed')
    appStore.showError(loadError.value)
  } finally {
    loading.value = false
  }
}

function onPageChange(next: number) {
  page.value = next
  void loadRecords()
}

function onPageSizeChange(next: number) {
  pageSize.value = next
  page.value = 1
  void loadRecords()
}

function releaseDetailResources() {
  revokeTestArtifact(detailArtifact.value)
  detailArtifact.value = null
  for (const image of detailImages.value) {
    if (typeof URL.revokeObjectURL === 'function') URL.revokeObjectURL(image.url)
  }
  detailImages.value = []
}

function closeDetail() {
  detailVisible.value = false
  detailRecord.value = null
  detailImageError.value = ''
  releaseDetailResources()
}

async function openDetail(row: TestRecord, mode: DetailMode) {
  closeDetail()
  detailMode.value = mode
  detailVisible.value = true
  detailLoading.value = true
  try {
    const record = await testRecordsAPI.get(row.id)
    detailRecord.value = record
    if (mode === 'text') {
      detailArtifact.value = buildTestArtifact(
        record.response_text ?? '',
        record.question_title || props.question?.title || 'test-result'
      )
    }
    if (mode === 'images') {
      await loadDetailImages(record)
    }
  } catch (error) {
    console.error('[测试历史] 加载详情失败', error)
    appStore.showError(t('admin.testQuestions.history.detailFailed'))
    closeDetail()
  } finally {
    detailLoading.value = false
  }
}

async function loadDetailImages(record: TestRecord) {
  const files = (record.files ?? []).filter((file) => file.kind === 'image')
  if (files.length === 0) return
  const images: DetailImage[] = []
  for (const file of files) {
    try {
      const blob = await testRecordsAPI.fetchFile(record.id, file.id)
      images.push({
        url: URL.createObjectURL(blob),
        mimeType: file.mime_type || 'image/*'
      })
    } catch (error) {
      console.error('[测试历史] 加载结果图片失败', error)
      detailImageError.value = t('admin.testQuestions.history.imageLoadFailed')
    }
  }
  detailImages.value = images
  activeImageIndex.value = 0
}

async function downloadFromRecord(row: TestRecord) {
  try {
    const record = detailRecord.value?.id === row.id
      ? detailRecord.value
      : await testRecordsAPI.get(row.id)
    const artifact = buildTestArtifact(
      record.response_text ?? '',
      record.question_title || props.question?.title || 'test-result'
    )
    if (!artifact) {
      appStore.showError(t('admin.testQuestions.history.noArtifact'))
      return
    }
    downloadTestArtifact(artifact)
    revokeTestArtifact(artifact)
  } catch (error) {
    console.error('[测试历史] 下载失败', error)
    appStore.showError(t('admin.testQuestions.history.downloadFailed'))
  }
}

function copyDetailText() {
  copyToClipboard(detailRecord.value?.response_text ?? '')
}

function downloadDetailArtifact() {
  if (detailArtifact.value) downloadTestArtifact(detailArtifact.value)
}

watch(
  () => [props.show, props.question?.id] as const,
  ([visible]) => {
    if (!visible) {
      closeDetail()
      return
    }
    page.value = 1
    void loadRecords()
  },
  { immediate: true }
)
</script>
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd frontend && pnpm vitest run src/components/admin/testquestion/__tests__/TestRecordHistoryModal.spec.ts -v`
Expected: PASS —— 7 个测试 ok

- [ ] **Step 5: 类型检查与 lint**

Run: `cd frontend && pnpm run typecheck && pnpm run lint:check`
Expected: 无错误

- [ ] **Step 6: 提交**

```bash
git add frontend/src/components/admin/testquestion
git commit -m "feat: 新增测试历史记录弹框组件"
```

---

### Task 11: 账号测试请求带上题目 ID

**Files:**
- Modify: `frontend/src/components/admin/account/AccountTestModal.vue`（请求体构造处，约 1311-1347 行）
- Test: `frontend/src/components/admin/account/__tests__/AccountTestModal.questions.spec.ts`

- [ ] **Step 1: 先确认现状**

Run: `cd frontend && grep -n "reasoning_effort\|test_question_id\|selectedQuestionId" src/components/admin/account/AccountTestModal.vue`
Expected: 能看到请求体里构造 `reasoning_effort` 的位置，以及 `selectedQuestionId` 的 ref 定义（当前不会发送 `test_question_id`）

- [ ] **Step 2: 在请求体里加 `test_question_id`**

在 `startTest` 构造请求体的对象字面量里，紧邻 `reasoning_effort` 之后追加一行：

```ts
      test_question_id: selectedQuestionId.value ? Number(selectedQuestionId.value) : 0,
```

- [ ] **Step 3: 补一条断言测试**

该文件已有用例「默认题目填充提示词 + reasoning_effort」，说明弹窗加载后会自动选中第一道题。沿用该用例的 mock 与 `mountModal` 写法，在 `describe` 块末尾追加：

```ts
  it('请求体带上当前选中测试题的 test_question_id', async () => {
    const wrapper = mountModal()
    await flushPromises()

    await (wrapper.vm as any).startTest()
    await flushPromises()

    const calls = (global.fetch as ReturnType<typeof vi.fn>).mock.calls
    const last = calls[calls.length - 1]
    const body = JSON.parse((last?.[1] as { body: string }).body)
    expect(body.test_question_id).toBe(9)
  })
```

其中 `id: 9` 是该用例 `listQuestions` mock 返回的题目 ID；若现有 mock 用的是别的 ID，把期望值改成同一个数字。断言前先用 `expect((wrapper.vm as any).selectedQuestionId).toBe('9')` 确认自动选中生效。

- [ ] **Step 4: 运行测试**

Run: `cd frontend && pnpm vitest run src/components/admin/account/__tests__/AccountTestModal.questions.spec.ts`
Expected: PASS —— 原有 10 个用例加新用例全绿

- [ ] **Step 5: 类型检查**

Run: `cd frontend && pnpm run typecheck`
Expected: 无错误

- [ ] **Step 6: 提交**

```bash
git add frontend/src/components/admin/account/AccountTestModal.vue frontend/src/components/admin/account/__tests__/AccountTestModal.questions.spec.ts
git commit -m "feat: 账号测试请求携带测试题 ID"
```

---

### Task 12: 题库页面接入「测试历史」按钮

**Files:**
- Modify: `frontend/src/views/admin/TestQuestionsView.vue`
- Test: `frontend/src/views/admin/__tests__/TestQuestionsView.spec.ts`

- [ ] **Step 1: 写失败的测试**

在 `TestQuestionsView.spec.ts` 中追加一个用例，并在 `mountView` 的 stubs 里补上弹框 stub。

先在 `mountView` 的 `global.stubs` 中加入：

```ts
        TestRecordHistoryModal: {
          props: { show: Boolean, question: Object },
          emits: ['close'],
          template: '<div v-if="show" class="history-modal-stub">{{ question ? question.id : "" }}</div>'
        },
```

再追加用例：

```ts
  it('操作列的测试历史按钮打开历史弹框并带上当前题目', async () => {
    const wrapper = mountView()
    await flushPromises()

    const historyButton = wrapper
      .findAll('button')
      .find((button) => button.attributes('title') === 'admin.testQuestions.history.action')
    expect(historyButton).toBeTruthy()
    await historyButton!.trigger('click')
    await flushPromises()

    const modal = wrapper.find('.history-modal-stub')
    expect(modal.exists()).toBe(true)
    expect(modal.text()).toBe('1')
  })
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd frontend && pnpm vitest run src/views/admin/__tests__/TestQuestionsView.spec.ts`
Expected: FAIL —— 找不到 `title="admin.testQuestions.history.action"` 的按钮

- [ ] **Step 3: 加按钮**

在 `TestQuestionsView.vue` 的 `#cell-actions` 模板里，放在「编辑」按钮之前：

```vue
                  <button
                    type="button"
                    class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-700 dark:hover:bg-dark-600 dark:hover:text-gray-300"
                    :title="t('admin.testQuestions.history.action')"
                    @click="openHistoryDialog(row)"
                  >
                    <Icon name="clock" size="sm" />
                  </button>
```

- [ ] **Step 4: 加状态与处理函数**

在 `<script setup>` 中新增 import：

```ts
import TestRecordHistoryModal from '@/components/admin/testquestion/TestRecordHistoryModal.vue'
```

在 `onMounted(loadQuestions)` 之前新增：

```ts
const showHistoryDialog = ref(false)
const historyQuestion = ref<TestQuestion | null>(null)

function openHistoryDialog(question: TestQuestion) {
  historyQuestion.value = question
  showHistoryDialog.value = true
}
```

- [ ] **Step 5: 挂载弹框**

在 `ConfirmDialog` 之后、`</AppLayout>` 之前插入：

```vue
      <TestRecordHistoryModal
        :show="showHistoryDialog"
        :question="historyQuestion"
        @close="showHistoryDialog = false"
      />
```

- [ ] **Step 6: 运行测试确认通过**

Run: `cd frontend && pnpm vitest run src/views/admin/__tests__/TestQuestionsView.spec.ts -v`
Expected: PASS —— 原有 3 个用例加新用例全绿

- [ ] **Step 7: 类型检查与 lint**

Run: `cd frontend && pnpm run typecheck && pnpm run lint:check`
Expected: 无错误

- [ ] **Step 8: 提交**

```bash
git add frontend/src/views/admin/TestQuestionsView.vue frontend/src/views/admin/__tests__/TestQuestionsView.spec.ts
git commit -m "feat: 题库页新增测试历史入口"
```

---

### Task 13: 全量校验

**Files:** 无新增文件，仅验证

- [ ] **Step 1: 后端全量单元测试**

Run: `cd backend && go build ./... && go test -tags=unit ./...`
Expected: 全部通过

- [ ] **Step 2: 后端全量普通测试与 lint**

Run: `cd backend && go test ./... && golangci-lint run ./...`
Expected: 全部通过。若 golangci-lint 未安装，跳过并在最终报告里注明。

- [ ] **Step 3: 前端全量校验**

Run: `cd frontend && pnpm run typecheck && pnpm run lint:check && pnpm run vitest run`
Expected: 全部通过

- [ ] **Step 4: 前端构建**

Run: `cd frontend && pnpm run build`
Expected: 构建成功

- [ ] **Step 5: 数据库迁移校验（人工执行）**

本任务不自动重启服务。部署时重启后执行：

```sql
SELECT filename FROM schema_migrations WHERE filename = '247_account_test_records.sql';
SELECT COUNT(*) FROM account_test_records;
SELECT COUNT(*) FROM account_test_record_files;
```

Expected: 第一条返回一行文件名；后两条返回 0。

- [ ] **Step 6: 端到端人工验证清单**

1. 打开「测试题库」页，某行操作列出现时钟图标按钮，hover 提示「测试历史」。
2. 点击后弹框按时间倒序列出记录；若从未测试过则显示空态。
3. 在「账号管理 → 更多 → 测试」中选择题目并执行，结束后再次打开该题目的测试历史，新记录出现在最上方，账号/模型/思考强度/时长/状态正确。
4. 文本结果点「查看文本」能看到原文并可复制；SVG 结果额外出现「下载文件」按钮，点击可下载 `.svg`。
5. 图片题记录点「查看图片」能加载并预览图片。
6. 失败的测试记录状态为红色「失败」，点「查看错误」能看到错误信息。
7. 连续测试同一题目超过 20 次后，最早的记录及其磁盘文件被自动清理。
8. 删除题目后 `<DATA_DIR>/test-records/` 下该题目的记录目录不再残留。

- [ ] **Step 7: 提交最终改动（如有）**

```bash
git status --short
git add -A
git commit -m "chore: 测试历史功能收尾" || echo "无待提交改动"
```

---

## 完成标准

- 后端：`go build ./...`、`go test -tags=unit ./...`、`go test ./...` 全绿。
- 前端：`typecheck`、`lint:check`、`vitest run`、`build` 全绿。
- 新迁移 `247_account_test_records.sql` 在重启后被 `schema_migrations` 记录，两张表可查询。
- 题库页操作列有「测试历史」按钮，弹框倒序分页展示该题目的记录，六列齐全。
- 结果按钮按记录内容显示：查看文本 / 查看图片 / 下载文件 / 查看错误，行为与账号测试弹框一致。
- 每题最多保留 20 条记录，超出自动裁剪并删除磁盘文件。

