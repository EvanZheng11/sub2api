package repository

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// accountTestRecordColumns 返回列表与详情共用的列清单（顺序必须与 scanAccountTestRecord 一致）。
// 正文列由调用方传入：列表用空字符串占位以避免大 payload，详情取真实列。
// has_text 显式取真实列 account_test_records.response_text，所以列表用占位符时它仍是真实值
// （SELECT 列表里的输出别名不可见，这里依然显式限定表名以免误读）。
func accountTestRecordColumns(responseTextExpr string) string {
	return `id, test_question_id, question_title, account_id, account_name, platform, model,
       reasoning_effort, status, error_message, ` + responseTextExpr + `,
       (account_test_records.response_text <> '') AS has_text,
       response_truncated, image_count, latency_ms, started_at, finished_at, created_at`
}

var (
	// 列表查询不取 response_text，只取是否非空的布尔值，避免大 payload。
	accountTestRecordListSQL = `SELECT ` + accountTestRecordColumns(`''::text AS response_text`) + `
FROM account_test_records`

	// 详情查询额外取完整文本。
	accountTestRecordDetailSQL = `SELECT ` + accountTestRecordColumns(`response_text`) + `
FROM account_test_records`

	// 写入后回读的列与详情保持一致，复用同一个扫描函数。
	accountTestRecordReturningSQL = `RETURNING ` + accountTestRecordColumns(`response_text`)
)

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
	item, err := scanAccountTestRecord(r.db.QueryRowContext(ctx, `
INSERT INTO account_test_records (
    test_question_id, question_title, account_id, account_name, platform, model,
    reasoning_effort, status, error_message, response_text, response_truncated,
    latency_ms, started_at, finished_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
`+accountTestRecordReturningSQL,
		input.TestQuestionID, input.QuestionTitle, nullableAccountID(input.AccountID),
		input.AccountName, input.Platform, input.Model, input.ReasoningEffort,
		input.Status, input.ErrorMessage, input.ResponseText, input.ResponseTruncated,
		input.LatencyMs, input.StartedAt, input.FinishedAt))
	if err != nil {
		return nil, translatePersistenceError(err, nil, nil)
	}
	return item, nil
}

// nullableAccountID 把 0 或负数视为未关联账号，写入 NULL 以避免外键失败。
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
		item, err := scanAccountTestRecord(rows)
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
	item, err := scanAccountTestRecord(r.db.QueryRowContext(ctx,
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

func scanAccountTestRecord(row accountTestRecordScanner) (*service.TestRecord, error) {
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

// assignNullableAccountID 把可空的账号 ID 还原成指针，NULL 时保持 nil。
func assignNullableAccountID(item *service.TestRecord, accountID sql.NullInt64) {
	if !accountID.Valid {
		return
	}
	value := accountID.Int64
	item.AccountID = &value
}
