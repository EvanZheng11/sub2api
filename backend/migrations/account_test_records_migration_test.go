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
