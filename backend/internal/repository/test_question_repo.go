package repository

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const testQuestionSelectSQL = `
SELECT id, title, prompt, type, created_at, updated_at
FROM test_questions`

type testQuestionRepository struct {
	db *sql.DB
}

// NewTestQuestionRepository 使用轻量 raw SQL 实现题库仓储（不引入新的 ORM 实体）。
func NewTestQuestionRepository(db *sql.DB) service.TestQuestionRepository {
	return &testQuestionRepository{db: db}
}

func (r *testQuestionRepository) List(ctx context.Context) ([]service.TestQuestion, error) {
	rows, err := r.db.QueryContext(ctx, testQuestionSelectSQL+`
 ORDER BY updated_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	items := make([]service.TestQuestion, 0, 16)
	for rows.Next() {
		item, err := scanTestQuestion(rows)
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

func (r *testQuestionRepository) GetByID(ctx context.Context, id int64) (*service.TestQuestion, error) {
	item, err := scanTestQuestion(r.db.QueryRowContext(ctx, testQuestionSelectSQL+` WHERE id = $1`, id))
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrTestQuestionNotFound, nil)
	}
	return item, nil
}

func (r *testQuestionRepository) Create(ctx context.Context, input service.TestQuestionInput) (*service.TestQuestion, error) {
	item, err := scanTestQuestion(r.db.QueryRowContext(ctx, `
INSERT INTO test_questions (title, prompt, type)
VALUES ($1, $2, $3)
RETURNING id, title, prompt, type, created_at, updated_at`,
		input.Title, input.Prompt, input.Type))
	if err != nil {
		return nil, translatePersistenceError(err, nil, nil)
	}
	return item, nil
}

func (r *testQuestionRepository) Update(ctx context.Context, id int64, input service.TestQuestionInput) (*service.TestQuestion, error) {
	item, err := scanTestQuestion(r.db.QueryRowContext(ctx, `
UPDATE test_questions
SET title = $2, prompt = $3, type = $4, updated_at = NOW()
WHERE id = $1
RETURNING id, title, prompt, type, created_at, updated_at`,
		id, input.Title, input.Prompt, input.Type))
	if err != nil {
		return nil, translatePersistenceError(err, service.ErrTestQuestionNotFound, nil)
	}
	return item, nil
}

func (r *testQuestionRepository) Delete(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx, `DELETE FROM test_questions WHERE id = $1`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return service.ErrTestQuestionNotFound
	}
	return nil
}

type testQuestionScanner interface {
	Scan(dest ...any) error
}

func scanTestQuestion(row testQuestionScanner) (*service.TestQuestion, error) {
	var item service.TestQuestion
	if err := row.Scan(&item.ID, &item.Title, &item.Prompt, &item.Type, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return nil, err
	}
	return &item, nil
}
