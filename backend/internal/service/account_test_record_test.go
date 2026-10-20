//go:build unit

package service

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

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

// 远程 https 媒体无法离线落盘，功能上仍跳过（可观测性由 warn 日志承担，这里不断言日志）。
func TestAccountTestRecorderSkipsRemoteMediaURL(t *testing.T) {
	recorder := newTestRecorder()
	recorder.capture(TestEvent{Type: "image", ImageURL: "https://example.com/a.png"})

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

	// data URL 里 + 是字面加号，不能被解成空格
	mime, payload, ok = decodeTestRecordDataURL("data:text/plain,a+b")
	require.True(t, ok)
	require.Equal(t, "text/plain", mime)
	require.Equal(t, []byte("a+b"), payload)

	_, _, ok = decodeTestRecordDataURL("data:text/plain;base64,%%%")
	require.False(t, ok)
}

// 未声明 MIME 时回退为 application/octet-stream。
func TestDecodeTestRecordDataURLDefaultMime(t *testing.T) {
	mime, payload, ok := decodeTestRecordDataURL("data:;base64,aGVsbG8=")
	require.True(t, ok)
	require.Equal(t, "application/octet-stream", mime)
	require.Equal(t, []byte("hello"), payload)
}

func TestSafeTestRecordFilePathRejectsEscape(t *testing.T) {
	root := t.TempDir()
	// 真实路径校验要 EvalSymlinks 成功，目标文件必须存在
	require.NoError(t, os.MkdirAll(filepath.Join(root, "12"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(root, "12", "1.png"), []byte("x"), 0o600))

	full, err := safeTestRecordFilePath(root, "12/1.png")
	require.NoError(t, err)
	require.Equal(t, filepath.ToSlash(filepath.Join(root, "12", "1.png")), filepath.ToSlash(full))

	_, err = safeTestRecordFilePath(root, "../../etc/passwd")
	require.ErrorIs(t, err, ErrTestRecordFilePathInvalid)

	_, err = safeTestRecordFilePath(root, "/etc/passwd")
	require.ErrorIs(t, err, ErrTestRecordFilePathInvalid)

	_, err = safeTestRecordFilePath(root, "  ")
	require.ErrorIs(t, err, ErrTestRecordFilePathInvalid)

	// 文件不存在时返回文件不存在，与 OpenFile 的语义保持一致
	_, err = safeTestRecordFilePath(root, "12/2.png")
	require.ErrorIs(t, err, ErrTestRecordFileNotFound)
}

// 被测规则：根目录内的符号链接不得指向目录外（词法检查通过但真实路径越界）。
func TestSafeTestRecordFilePathRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("secret"), 0o600))
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skipf("当前环境无法创建符号链接，跳过：%v", err)
	}

	_, err := safeTestRecordFilePath(root, "link/secret.txt")
	require.ErrorIs(t, err, ErrTestRecordFilePathInvalid)
}

// 被测规则：DATA_DIR 优先，未设置时回退到 ./data。
func TestResolveTestRecordRootDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("DATA_DIR", dir)
	require.Equal(t, filepath.Join(dir, "test-records"), resolveTestRecordRootDir())

	t.Setenv("DATA_DIR", "")
	require.Equal(t, filepath.Join("data", "test-records"), resolveTestRecordRootDir())
}

// 被测规则：相对路径为 <record_id>/<序号>.<扩展名>，未知 MIME 用 bin。
func TestRecordRelativePath(t *testing.T) {
	require.Equal(t, "12/3.png", filepath.ToSlash(recordRelativePath(12, 3, "image/png")))
	require.Equal(t, "12/3.bin", filepath.ToSlash(recordRelativePath(12, 3, "application/x-unknown")))
}

// 被测规则：耗时为负时归零。
func TestAccountTestRecorderClampsNegativeLatency(t *testing.T) {
	startedAt := time.Date(2026, 10, 21, 10, 0, 10, 0, time.UTC)
	recorder := newAccountTestRecorder(TestRecordInput{TestQuestionID: 7}, startedAt)
	recorder.capture(TestEvent{Type: "test_complete", Success: true})

	input, _ := recorder.finish(time.Date(2026, 10, 21, 10, 0, 5, 0, time.UTC))

	require.Equal(t, int64(0), input.LatencyMs)
}

// 被测规则：按字节截断后仍必须是合法 UTF-8。
func TestAccountTestRecorderTruncatesKeepingValidUTF8(t *testing.T) {
	recorder := newTestRecorder()
	// “鹈”占 3 字节，构造长度略超上限的纯中文文本，使截断点落在字符中间
	text := strings.Repeat("鹈", maxTestRecordResponseBytes/3+10)
	require.Greater(t, len(text), maxTestRecordResponseBytes)
	recorder.capture(TestEvent{Type: "content", Text: text})
	recorder.capture(TestEvent{Type: "test_complete", Success: true})

	input, _ := recorder.finish(time.Now())

	require.True(t, input.ResponseTruncated)
	require.True(t, utf8.ValidString(input.ResponseText))
	require.LessOrEqual(t, len(input.ResponseText), maxTestRecordResponseBytes)
}

// 被测规则：状态优先级 failed 高于 succeeded。
func TestAccountTestRecorderFailedOverridesSuccess(t *testing.T) {
	recorder := newTestRecorder()
	recorder.capture(TestEvent{Type: "error", Error: "上游 502"})
	recorder.capture(TestEvent{Type: "test_complete", Success: true})

	input, _ := recorder.finish(time.Now())

	require.Equal(t, TestRecordStatusFailed, input.Status)
	require.Equal(t, "上游 502", input.ErrorMessage)
}

// 被测规则：audio / video 事件都能成功解码并累计到 media。
func TestAccountTestRecorderAccumulatesAudioAndVideo(t *testing.T) {
	recorder := newTestRecorder()
	recorder.capture(TestEvent{Type: "audio", AudioURL: "data:audio/mpeg;base64,aGVsbG8="})
	recorder.capture(TestEvent{Type: "video", VideoURL: "data:video/mp4;base64,aGVsbG8="})
	recorder.capture(TestEvent{Type: "test_complete", Success: true})

	input, media := recorder.finish(time.Now())

	require.Equal(t, TestRecordStatusSuccess, input.Status)
	require.Len(t, media, 2)
	require.Equal(t, TestRecordFileKindAudio, media[0].Kind)
	require.Equal(t, "audio/mpeg", media[0].MimeType)
	require.Equal(t, []byte("hello"), media[0].Data)
	require.Equal(t, TestRecordFileKindVideo, media[1].Kind)
	require.Equal(t, "video/mp4", media[1].MimeType)
	require.Equal(t, []byte("hello"), media[1].Data)
}

func TestTestRecordMediaExtension(t *testing.T) {
	require.Equal(t, "png", testRecordMediaExtension("image/png"))
	require.Equal(t, "jpg", testRecordMediaExtension("image/jpeg"))
	require.Equal(t, "mp4", testRecordMediaExtension("video/mp4"))
	require.Equal(t, "bin", testRecordMediaExtension("application/x-unknown"))
	require.Equal(t, "bin", testRecordMediaExtension(""))
}

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
	// 保留条数需要钳制到 [0, len(ids)]，否则 ids[keepCount:] 会越界
	keep := keepCount
	if keep < 0 {
		keep = 0
	}
	var pruned []int64
	for _, ids := range byQuestion {
		sort.Slice(ids, func(i, j int) bool { return ids[i] > ids[j] })
		start := keep
		if start > len(ids) {
			start = len(ids)
		}
		for _, id := range ids[start:] {
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
