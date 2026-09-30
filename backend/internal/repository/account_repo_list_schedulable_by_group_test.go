package repository

import (
	"context"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	_ "github.com/Wei-Shaw/sub2api/ent/runtime"
	"github.com/stretchr/testify/require"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
)

// TestListSchedulableByGroupID_BindingDimensionWithoutPlatformFilter 锁定绑定维度查询：
// 候选账号只按 account_groups 绑定关系 + 既有的 status/schedulable/限流窗口条件筛选，
// 不再按账号平台过滤（设计 §3.3）。平台能力改由调度层在内存按能力矩阵判定。
func TestListSchedulableByGroupID_BindingDimensionWithoutPlatformFilter(t *testing.T) {
	var capturedSQL string
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(captureEntQueryMatcher{actual: &capturedSQL}))
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	driver := entsql.OpenDB(dialect.Postgres, db)
	client := dbent.NewClient(dbent.Driver(driver))
	t.Cleanup(func() { _ = client.Close() })
	repo := newAccountRepositoryWithSQL(client, db, nil)

	mock.ExpectQuery("schedulable accounts by group binding").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))

	accounts, err := repo.ListSchedulableByGroupID(context.Background(), 42)
	require.NoError(t, err)
	require.Empty(t, accounts)
	require.NoError(t, mock.ExpectationsWereMet())

	normalized := normalizeSQLWhitespace(capturedSQL)
	_, whereClause, found := strings.Cut(normalized, " WHERE ")
	require.True(t, found, "expected WHERE clause in query: %s", normalized)
	whereClause, orderClause, hasOrder := strings.Cut(whereClause, " ORDER BY ")
	require.True(t, hasOrder, "expected ORDER BY clause in query: %s", normalized)

	require.Contains(t, whereClause, "group_id", "候选集必须按 account_groups 绑定关系筛选")
	require.NotContains(t, whereClause, "platform", "绑定维度查询不得再按账号平台过滤")
	for _, schedulablePredicate := range []string{
		"status",
		"schedulable",
		"deleted_at",
		"temp_unschedulable_until",
		"expires_at",
		"auto_pause_on_expired",
		"overload_until",
		"rate_limit_reset_at",
	} {
		require.Contains(t, whereClause, schedulablePredicate,
			"绑定维度查询必须保留既有调度资格条件 %q: %s", schedulablePredicate, whereClause)
	}
	require.Contains(t, orderClause, `"priority"`)
}
