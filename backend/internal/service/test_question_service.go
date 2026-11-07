package service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"go.uber.org/zap"
)

// 题库题目类型，必须与 test_questions 表的 CHECK 约束保持一致。
const (
	TestQuestionTypeText  = "text"
	TestQuestionTypeImage = "image"
	TestQuestionTypeFile  = "file"
)

const (
	maxTestQuestionTitleLen = 200
	// 题目提示词按 UTF-8 字符（rune）计上限，128000 与测试输出预算同口径。
	maxTestQuestionPromptLen = 128000
)

var (
	ErrTestQuestionNotFound      = infraerrors.NotFound("TEST_QUESTION_NOT_FOUND", "测试题目不存在")
	ErrTestQuestionInvalidType   = infraerrors.BadRequest("TEST_QUESTION_INVALID_TYPE", "题目类型不合法，仅支持 text、image、file")
	ErrTestQuestionTitleEmpty    = infraerrors.BadRequest("TEST_QUESTION_TITLE_REQUIRED", "题目标题不能为空")
	ErrTestQuestionPromptEmpty   = infraerrors.BadRequest("TEST_QUESTION_PROMPT_REQUIRED", "题目提示词不能为空")
	ErrTestQuestionTitleTooLong  = infraerrors.BadRequest("TEST_QUESTION_TITLE_TOO_LONG", "题目标题过长，最多 200 个字符")
	ErrTestQuestionPromptTooLong = infraerrors.BadRequest("TEST_QUESTION_PROMPT_TOO_LONG", "题目提示词过长，最多 128000 个字符")
)

// TestQuestion 是题库中的一道测试题目。
type TestQuestion struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Prompt    string    `json:"prompt"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// TestQuestionInput 是创建/更新题目的入参（已 trim）。
type TestQuestionInput struct {
	Title  string
	Prompt string
	Type   string
}

// TestQuestionRepository 题库仓储，使用轻量 raw SQL 实现。
type TestQuestionRepository interface {
	List(ctx context.Context) ([]TestQuestion, error)
	GetByID(ctx context.Context, id int64) (*TestQuestion, error)
	Create(ctx context.Context, input TestQuestionInput) (*TestQuestion, error)
	Update(ctx context.Context, id int64, input TestQuestionInput) (*TestQuestion, error)
	Delete(ctx context.Context, id int64) error
}

// TestQuestionService 题库业务逻辑。
type TestQuestionService struct {
	repo    TestQuestionRepository
	records TestRecordStore
}

func NewTestQuestionService(repo TestQuestionRepository, records TestRecordStore) *TestQuestionService {
	return &TestQuestionService{repo: repo, records: records}
}

func (s *TestQuestionService) List(ctx context.Context) ([]TestQuestion, error) {
	return s.repo.List(ctx)
}

func (s *TestQuestionService) GetByID(ctx context.Context, id int64) (*TestQuestion, error) {
	if id <= 0 {
		return nil, ErrTestQuestionNotFound
	}
	return s.repo.GetByID(ctx, id)
}

func (s *TestQuestionService) Create(ctx context.Context, input TestQuestionInput) (*TestQuestion, error) {
	normalized, err := normalizeTestQuestionInput(input)
	if err != nil {
		return nil, err
	}
	return s.repo.Create(ctx, normalized)
}

func (s *TestQuestionService) Update(ctx context.Context, id int64, input TestQuestionInput) (*TestQuestion, error) {
	if id <= 0 {
		return nil, ErrTestQuestionNotFound
	}
	normalized, err := normalizeTestQuestionInput(input)
	if err != nil {
		return nil, err
	}
	return s.repo.Update(ctx, id, normalized)
}

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

// normalizeTestQuestionInput 负责 trim、类型校验与长度校验。
// 注意：错误信息中不包含 prompt 内容，避免提示词进入日志。
func normalizeTestQuestionInput(input TestQuestionInput) (TestQuestionInput, error) {
	title := strings.TrimSpace(input.Title)
	prompt := strings.TrimSpace(input.Prompt)
	questionType := strings.ToLower(strings.TrimSpace(input.Type))

	if title == "" {
		return TestQuestionInput{}, ErrTestQuestionTitleEmpty
	}
	if prompt == "" {
		return TestQuestionInput{}, ErrTestQuestionPromptEmpty
	}
	if !IsValidTestQuestionType(questionType) {
		return TestQuestionInput{}, ErrTestQuestionInvalidType
	}
	if utf8.RuneCountInString(title) > maxTestQuestionTitleLen {
		return TestQuestionInput{}, ErrTestQuestionTitleTooLong
	}
	if utf8.RuneCountInString(prompt) > maxTestQuestionPromptLen {
		return TestQuestionInput{}, ErrTestQuestionPromptTooLong
	}

	return TestQuestionInput{Title: title, Prompt: prompt, Type: questionType}, nil
}

// IsValidTestQuestionType 判断题目类型是否合法。
func IsValidTestQuestionType(questionType string) bool {
	switch questionType {
	case TestQuestionTypeText, TestQuestionTypeImage, TestQuestionTypeFile:
		return true
	default:
		return false
	}
}
