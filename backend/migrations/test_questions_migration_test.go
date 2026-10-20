package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// 题库迁移必须建独立表、带类型与非空约束，并且只插入一次默认题目。
func TestTestQuestionsMigrationShape(t *testing.T) {
	content, err := FS.ReadFile("246_test_questions.sql")
	require.NoError(t, err)

	sql := strings.Join(strings.Fields(string(content)), " ")

	require.Contains(t, sql, "CREATE TABLE IF NOT EXISTS test_questions")
	require.Contains(t, sql, "type VARCHAR(16) NOT NULL")
	require.Contains(t, sql, "CHECK (type IN ('text', 'image', 'file'))")
	require.Contains(t, sql, "CHECK (btrim(title) <> '')")
	require.Contains(t, sql, "CHECK (btrim(prompt) <> '')")
	require.Contains(t, sql, "CREATE INDEX IF NOT EXISTS idx_test_questions_updated_at")
	require.Contains(t, sql, "鹈鹕骑自行车 SVG 动画")
	require.Contains(t, sql, "不使用技能、不测试，一次性生成一段高质量的 SVG 代码，绘制一只正在骑自行车的鹈鹕的动画，画面美观且结构清晰。")
	// 种子必须是迁移期的条件插入，不能每次启动覆盖已有数据
	require.Contains(t, sql, "WHERE NOT EXISTS")
	require.NotContains(t, sql, "ON CONFLICT")
	require.NotContains(t, strings.ToUpper(sql), "ALTER TABLE ACCOUNTS")
}
