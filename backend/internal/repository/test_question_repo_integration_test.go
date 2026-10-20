//go:build integration

package repository

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

// 题库 raw SQL 仓储的 CRUD、排序与数据库约束校验（使用 testcontainers 的临时 Postgres）。
func TestTestQuestionRepositoryCRUDIntegration(t *testing.T) {
	ctx := context.Background()
	repo := NewTestQuestionRepository(integrationDB)

	titlePrefix := "集成测试题目-" + time.Now().Format("150405.000000000")
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM test_questions WHERE title LIKE $1`, titlePrefix+"%")
	})

	created, err := repo.Create(ctx, service.TestQuestionInput{Title: titlePrefix + "-A", Prompt: "提示词 A", Type: service.TestQuestionTypeText})
	require.NoError(t, err)
	require.NotZero(t, created.ID)
	require.Equal(t, service.TestQuestionTypeText, created.Type)
	require.False(t, created.CreatedAt.IsZero())
	require.False(t, created.UpdatedAt.IsZero())

	fetched, err := repo.GetByID(ctx, created.ID)
	require.NoError(t, err)
	require.Equal(t, "提示词 A", fetched.Prompt)

	second, err := repo.Create(ctx, service.TestQuestionInput{Title: titlePrefix + "-B", Prompt: "提示词 B", Type: service.TestQuestionTypeImage})
	require.NoError(t, err)

	// 稍等重塑 updated_at，避免同微秒导致顺序不确定
	time.Sleep(5 * time.Millisecond)
	updated, err := repo.Update(ctx, created.ID, service.TestQuestionInput{Title: titlePrefix + "-A2", Prompt: "提示词 A2", Type: service.TestQuestionTypeFile})
	require.NoError(t, err)
	require.Equal(t, titlePrefix+"-A2", updated.Title)
	require.Equal(t, service.TestQuestionTypeFile, updated.Type)

	items, err := repo.List(ctx)
	require.NoError(t, err)
	var mine []service.TestQuestion
	for _, item := range items {
		if strings.HasPrefix(item.Title, titlePrefix) {
			mine = append(mine, item)
		}
	}
	require.Len(t, mine, 2)
	require.Equal(t, created.ID, mine[0].ID, "最近更新的题目应排在最前")
	require.Equal(t, second.ID, mine[1].ID)

	// 数据库层约束：非法类型、空标题必须被拒绝
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO test_questions (title, prompt, type) VALUES ($1, $2, 'video')`, titlePrefix+"-bad", "p")
	require.Error(t, err)
	_, err = integrationDB.ExecContext(ctx, `INSERT INTO test_questions (title, prompt, type) VALUES ('   ', $1, 'text')`, "p")
	require.Error(t, err)

	require.NoError(t, repo.Delete(ctx, created.ID))
	_, err = repo.GetByID(ctx, created.ID)
	require.ErrorIs(t, err, service.ErrTestQuestionNotFound)
	require.ErrorIs(t, repo.Delete(ctx, created.ID), service.ErrTestQuestionNotFound)
	require.ErrorIs(t, repo.Delete(ctx, -1), service.ErrTestQuestionNotFound)

	// 更新不存在的题目返回 404 语义
	_, err = repo.Update(ctx, second.ID+999999, service.TestQuestionInput{Title: "x", Prompt: "y", Type: service.TestQuestionTypeText})
	require.ErrorIs(t, err, service.ErrTestQuestionNotFound)
}

// 默认题目只在迁移时插入一次，并且内容与合同一致。
func TestTestQuestionSeedMigrationIntegration(t *testing.T) {
	ctx := context.Background()

	var (
		id     int64
		prompt string
		qType  string
	)
	err := integrationDB.QueryRowContext(ctx, `
SELECT id, prompt, type FROM test_questions WHERE title = $1`, "鹈鹕骑自行车 SVG 动画").Scan(&id, &prompt, &qType)
	require.NoError(t, err, "迁移应插入默认题目")
	require.Equal(t, service.TestQuestionTypeFile, qType)
	require.Equal(t, "不使用技能、不测试，一次性生成一段高质量的 SVG 代码，绘制一只正在骑自行车的鹈鹕的动画，画面美观且结构清晰。", prompt)

	var count int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM test_questions WHERE title = $1`, "鹈鹕骑自行车 SVG 动画").Scan(&count))
	require.Equal(t, 1, count)

	// 迁移不会重复执行：再次运行迁移不应复活被删除的题目
	_, err = integrationDB.ExecContext(ctx, `DELETE FROM test_questions WHERE id = $1`, id)
	require.NoError(t, err)
	require.NoError(t, ApplyMigrations(ctx, integrationDB))

	var after int
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT COUNT(*) FROM test_questions WHERE title = $1`, "鹈鹕骑自行车 SVG 动画").Scan(&after))
	require.Equal(t, 0, after, "已删除的默认题目不应被迁移复活")

	// 恢复默认题目，避免影响其他集成测试断言
	_, err = integrationDB.ExecContext(ctx, `
INSERT INTO test_questions (title, prompt, type)
VALUES ($1, $2, $3)`, "鹈鹕骑自行车 SVG 动画",
		"不使用技能、不测试，一次性生成一段高质量的 SVG 代码，绘制一只正在骑自行车的鹈鹕的动画，画面美观且结构清晰。",
		service.TestQuestionTypeFile)
	require.NoError(t, err)

	// 确认数据库列类型存在（id/created_at/updated_at）
	var nullTime sql.NullTime
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT updated_at FROM test_questions ORDER BY id LIMIT 1`).Scan(&nullTime))
	require.True(t, nullTime.Valid)
}
