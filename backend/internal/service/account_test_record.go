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
	ID                int64            `json:"id"`
	TestQuestionID    int64            `json:"test_question_id"`
	QuestionTitle     string           `json:"question_title"`
	AccountID         *int64           `json:"account_id"`
	AccountName       string           `json:"account_name"`
	Platform          string           `json:"platform"`
	Model             string           `json:"model"`
	ReasoningEffort   string           `json:"reasoning_effort"`
	Status            string           `json:"status"`
	ErrorMessage      string           `json:"error_message"`
	ResponseText      string           `json:"response_text,omitempty"`
	HasText           bool             `json:"has_text"`
	ResponseTruncated bool             `json:"response_truncated"`
	ImageCount        int              `json:"image_count"`
	LatencyMs         int64            `json:"latency_ms"`
	StartedAt         time.Time        `json:"started_at"`
	FinishedAt        time.Time        `json:"finished_at"`
	CreatedAt         time.Time        `json:"created_at"`
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
	// data URL 的百分号编码（RFC 2397）里 + 是字面加号，必须用 PathUnescape
	// 而不是 QueryUnescape（后者会把 + 变成空格）。
	decoded, err := url.PathUnescape(payloadPart)
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

// pathWithinRoot 判断 absPath 是否等于根目录本身或其子路径。
func pathWithinRoot(root, absPath string) bool {
	return absPath == root || strings.HasPrefix(absPath, root+string(filepath.Separator))
}

// safeTestRecordFilePath 把库中存的相对路径拼到根目录，并拒绝越界路径。
// 先做词法检查，再按真实路径（解析符号链接后）复检一次。
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
	if !pathWithinRoot(absRoot, absFull) {
		return "", ErrTestRecordFilePathInvalid
	}

	// 词法检查无法阻止根目录内的符号链接指向外部，这里按真实路径再校验一次。
	realRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		return "", ErrTestRecordFileNotFound
	}
	realFull, err := filepath.EvalSymlinks(absFull)
	if err != nil {
		return "", ErrTestRecordFileNotFound
	}
	if !pathWithinRoot(realRoot, realFull) {
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
