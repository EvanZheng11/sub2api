//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"entgo.io/ent/dialect"
	"entgo.io/ent/dialect/sql"
	"github.com/DATA-DOG/go-sqlmock"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestAccountTestUsageCapture(t *testing.T) {
	cases := []struct {
		name, body string
		want       UsageTokens
		seen       bool
	}{
		{"anthropic SSE cumulative", "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":10,\"cache_read_input_tokens\":4,\"cache_creation_input_tokens\":3,\"cache_creation\":{\"ephemeral_5m_input_tokens\":1,\"ephemeral_1h_input_tokens\":2}}}}\n\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":5}}\n\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":8}}\n\ndata: {\"type\":\"error\",\"error\":{\"message\":\"failed\"}}\n\n", UsageTokens{InputTokens: 10, OutputTokens: 8, CacheReadTokens: 4, CacheCreationTokens: 3, CacheCreation5mTokens: 1, CacheCreation1hTokens: 2}, true},
		{"responses SSE duplicate", "data:{\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":20,\"output_tokens\":7,\"input_tokens_details\":{\"cached_tokens\":6}}}}\n\ndata:{\"response\":{\"usage\":{\"input_tokens\":20,\"output_tokens\":7,\"input_tokens_details\":{\"cached_tokens\":6}}}}\n\n", UsageTokens{InputTokens: 14, OutputTokens: 7, CacheReadTokens: 6}, true},
		{"chat JSON", "{\n \"usage\":{\"prompt_tokens\":15,\"completion_tokens\":9,\"prompt_tokens_details\":{\"cached_tokens\":5}}\n}", UsageTokens{InputTokens: 10, OutputTokens: 9, CacheReadTokens: 5}, true},
		{"deepseek JSON", `{"usage":{"prompt_tokens":15,"completion_tokens":9,"prompt_cache_hit_tokens":5}}`, UsageTokens{InputTokens: 10, OutputTokens: 9, CacheReadTokens: 5}, true},
		{"gemini SSE", "data: {\"response\":{\"usageMetadata\":{\"promptTokenCount\":30,\"cachedContentTokenCount\":10,\"candidatesTokenCount\":4,\"thoughtsTokenCount\":6}}}\n\ndata: {\"response\":{\"usageMetadata\":{\"promptTokenCount\":30,\"cachedContentTokenCount\":10,\"candidatesTokenCount\":4,\"thoughtsTokenCount\":6}}}\n\n", UsageTokens{InputTokens: 20, OutputTokens: 10, CacheReadTokens: 10}, true},
		{"bedrock JSON", `{"usage":{"input_tokens":12,"output_tokens":3}}`, UsageTokens{InputTokens: 12, OutputTokens: 3}, true},
		{"no usage", `{"content":"not billable evidence"}`, UsageTokens{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := &accountTestUsageRequest{}
			b := &accountTestUsageBody{ReadCloser: io.NopCloser(strings.NewReader(tc.body)), request: r}
			var got strings.Builder
			buf := make([]byte, 7)
			_, err := io.CopyBuffer(&got, b, buf)
			require.NoError(t, err)
			require.NoError(t, b.Close())
			require.Equal(t, tc.body, got.String())
			require.Equal(t, tc.seen, r.seen)
			require.Equal(t, tc.want, r.tokens)
		})
	}
}

func TestAccountTestUsageReviewRegressions(t *testing.T) {
	t.Run("Anthropic delta zero preserves start", func(t *testing.T) {
		r := &accountTestUsageRequest{}
		r.observe(gjson.Parse(`{"type":"message_start","message":{"usage":{"input_tokens":10,"cache_read_input_tokens":4,"cache_creation_input_tokens":3}}}`))
		r.observe(gjson.Parse(`{"type":"message_delta","usage":{"input_tokens":0,"cache_read_input_tokens":0,"cache_creation_input_tokens":0,"output_tokens":5}}`))
		require.Equal(t, 10, r.tokens.InputTokens)
		require.Equal(t, 4, r.tokens.CacheReadTokens)
		require.Equal(t, 3, r.tokens.CacheCreationTokens)
		require.Equal(t, 5, r.tokens.OutputTokens)
	})
	t.Run("Images output classification", func(t *testing.T) {
		r := &accountTestUsageRequest{endpoint: "/v1/images/generations"}
		r.observe(gjson.Parse(`{"usage":{"input_tokens":10,"output_tokens":100}}`))
		require.Equal(t, 100, r.tokens.ImageOutputTokens)
		r.observe(gjson.Parse(`{"usage":{"output_tokens":100,"output_tokens_details":{"image_tokens":80}}}`))
		require.Equal(t, 80, r.tokens.ImageOutputTokens)
	})
	t.Run("Gemini image classification", func(t *testing.T) {
		r := &accountTestUsageRequest{}
		r.observe(gjson.Parse(`{"usageMetadata":{"promptTokenCount":10,"candidatesTokenCount":120,"candidatesTokensDetails":[{"modality":"TEXT","tokenCount":20},{"modality":"IMAGE","tokenCount":100}]}}`))
		require.Equal(t, 120, r.tokens.OutputTokens)
		require.Equal(t, 100, r.tokens.ImageOutputTokens)
	})
	t.Run("Grok media retains unpriced probes", func(t *testing.T) {
		session := &accountTestUsageSession{account: &Account{Platform: PlatformGrok}, billing: &accountTestBillingStub{pricing: NewBillingService(nil, nil)}}
		for _, path := range []string{"images/generations", "videos/generations", "tts", "stt"} {
			req, err := http.NewRequestWithContext(context.WithValue(context.Background(), accountTestUsageContextKey{}, session), http.MethodPost, "https://mock.invalid/v1/"+path, strings.NewReader(`{"model":"unpriced-media-model"}`))
			require.NoError(t, err)
			require.NoError(t, validateAccountTestUsageRequest(req))
			require.False(t, accountTestRequestUsesTokenBilling(req, session))
		}
	})
}

func TestAccountTestUsageRequestsAndAdminAttribution(t *testing.T) {
	ledger := &accountTestBillingStub{pricing: NewBillingService(nil, nil)}
	session := &accountTestUsageSession{admin: &User{ID: 900}, key: &APIKey{ID: 901}, account: &Account{ID: 55}, billing: ledger}
	for _, body := range []string{`{"usage":{"input_tokens":100,"output_tokens":5}}`, `{"usage":{"input_tokens":200,"output_tokens":8}}`, `{"error":"no usage"}`} {
		req, _ := http.NewRequestWithContext(context.WithValue(context.Background(), accountTestUsageContextKey{}, session), http.MethodPost, "https://mock.invalid/v1/messages", strings.NewReader(`{"model":"claude-sonnet-4-6"}`))
		resp := observeAccountTestResponse(req, &http.Response{Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))})
		_, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}
	require.NoError(t, session.finish(context.Background()))
	require.Len(t, ledger.logs, 2)
	require.NotEqual(t, ledger.logs[0].RequestID, ledger.logs[1].RequestID)
	for _, log := range ledger.logs {
		require.Equal(t, int64(900), log.UserID)
		require.Equal(t, int64(901), log.APIKeyID)
		require.Nil(t, log.SubscriptionID)
		require.Nil(t, log.GroupID)
		require.Positive(t, log.ActualCost)
	}
}

type testBillingUsers struct {
	UserRepository
	admin  *User
	cost   float64
	userID int64
	err    error
}

func (r *testBillingUsers) GetFirstAdmin(context.Context) (*User, error) { return r.admin, r.err }
func (r *testBillingUsers) DeductBalance(ctx context.Context, id int64, cost float64) error {
	if dbent.TxFromContext(ctx) == nil {
		return errors.New("no transaction")
	}
	r.userID = id
	r.cost += cost
	return r.err
}

type testBillingKeys struct {
	APIKeyRepository
	keys    []APIKey
	created *APIKey
	err     error
}

func (r *testBillingKeys) ListByUserID(_ context.Context, _ int64, _ pagination.PaginationParams, _ APIKeyListFilters) ([]APIKey, *pagination.PaginationResult, error) {
	return r.keys, &pagination.PaginationResult{Pages: 1}, r.err
}
func (r *testBillingKeys) Create(_ context.Context, key *APIKey) error {
	r.created = key
	key.ID = 901
	return r.err
}

type testBillingLogs struct {
	UsageLogRepository
	inserted bool
	err      error
}

func (r *testBillingLogs) Create(ctx context.Context, _ *UsageLog) (bool, error) {
	if dbent.TxFromContext(ctx) == nil {
		return false, errors.New("no transaction")
	}
	return r.inserted, r.err
}

func testBillingSQLClient(t *testing.T) (*dbent.Client, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	client := dbent.NewClient(dbent.Driver(sql.OpenDB(dialect.Postgres, db)))
	t.Cleanup(func() { _ = client.Close() })
	return client, mock
}
func TestAccountTestBillingPrepare(t *testing.T) {
	client, _ := testBillingSQLClient(t)
	users := &testBillingUsers{admin: &User{ID: 900, Role: RoleAdmin, Status: StatusActive}}
	keys := &testBillingKeys{keys: []APIKey{{ID: 99, UserID: 900, Name: "production", Status: StatusActive}}}
	b := &accountTestBilling{users: users, keys: keys, logs: &testBillingLogs{}, client: client, pricing: NewBillingService(nil, nil)}
	admin, key, err := b.prepare(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(900), admin.ID)
	require.Equal(t, keys.created, key)
	require.Equal(t, StatusAPIKeyDisabled, key.Status)
	require.Nil(t, key.GroupID)
	require.Zero(t, key.Quota)
	require.False(t, key.HasRateLimits())
	keys.keys = []APIKey{*key}
	keys.created = nil
	_, found, err := b.prepare(context.Background())
	require.NoError(t, err)
	require.Equal(t, key.ID, found.ID)
	require.Nil(t, keys.created)
	groupID := int64(7)
	keys.keys[0].GroupID = &groupID
	_, _, err = b.prepare(context.Background())
	require.ErrorContains(t, err, "配置被修改")
	users.admin = nil
	_, _, err = b.prepare(context.Background())
	require.ErrorContains(t, err, "管理员不存在")
}

type usageTestBalanceCache struct {
	userID int64
	calls  int
}

func (c *usageTestBalanceCache) InvalidateUserBalance(_ context.Context, id int64) error {
	c.userID = id
	c.calls++
	return nil
}

func TestAccountTestBillingAtomicBoundary(t *testing.T) {
	for _, tc := range []struct {
		name            string
		inserted        bool
		logErr, userErr error
	}{
		{"new charge", true, nil, nil}, {"duplicate no charge", false, nil, nil}, {"log error rolls back", true, errors.New("insert failed"), nil}, {"deduct error rolls back", true, nil, errors.New("deduct failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, mock := testBillingSQLClient(t)
			users := &testBillingUsers{err: tc.userErr}
			logs := &testBillingLogs{inserted: tc.inserted, err: tc.logErr}
			cache := &usageTestBalanceCache{}
			b := &accountTestBilling{client: client, users: users, logs: logs, cache: cache}
			mock.ExpectBegin()
			if tc.logErr != nil || tc.userErr != nil {
				mock.ExpectRollback()
			} else {
				mock.ExpectCommit()
			}
			err := b.record(context.Background(), &UsageLog{UserID: 900, APIKeyID: 901, ActualCost: 0.125})
			if tc.logErr != nil || tc.userErr != nil {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			if tc.inserted && tc.logErr == nil {
				require.Equal(t, int64(900), users.userID)
			} else {
				require.Zero(t, users.cost)
			}
			if tc.inserted && tc.logErr == nil && tc.userErr == nil {
				require.Equal(t, int64(900), cache.userID)
				require.Equal(t, 1, cache.calls)
			} else {
				require.Zero(t, cache.calls)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
func TestAccountTestUsageAdaptiveAndBackground(t *testing.T) {
	account := adaptiveCNAccountTestAccount(1, PlatformDeepseek)
	svc, upstream := adaptiveCNAccountTestService(account,
		&http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n"))},
		&http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":11}}}\n\ndata: {\"type\":\"message_stop\"}\n\n"))},
		&http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":12,\"output_tokens\":3}}}\n\n"))})
	ledger := &accountTestBillingStub{}
	svc.testBilling = ledger
	result, err := svc.RunTestBackground(context.Background(), account.ID, "deepseek-chat")
	require.NoError(t, err)
	require.Equal(t, "success", result.Status)
	require.Len(t, upstream.requests, 3)
	require.Len(t, ledger.logs, 3)
	for i, log := range ledger.logs {
		require.Equal(t, 10+i, log.InputTokens)
		require.Equal(t, int64(900), log.UserID)
	}
}

type usageTestQuestionLookup struct{}

func (usageTestQuestionLookup) GetByID(context.Context, int64) (*TestQuestion, error) {
	return &TestQuestion{ID: 7, Title: "题目"}, nil
}

type usageTestHistoryWriter struct{ input TestRecordInput }

func (w *usageTestHistoryWriter) Record(_ context.Context, input TestRecordInput, _ []TestRecordMedia) error {
	w.input = input
	return nil
}

func TestAccountTestUsageQuestionHistory(t *testing.T) {
	account := adaptiveCNAccountTestAccount(1, PlatformDeepseek)
	account.Credentials["api_protocol"] = APIProtocolChatCompletions
	resp := &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("data: {\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2}}\n\ndata: [DONE]\n\n"))}
	svc, _ := adaptiveCNAccountTestService(account, resp)
	ledger := &accountTestBillingStub{}
	svc.testBilling = ledger
	writer := &usageTestHistoryWriter{}
	svc.testQuestions = usageTestQuestionLookup{}
	svc.testRecordWriter = writer
	c, _ := newTestContext()
	require.NoError(t, svc.TestAccountConnection(c, 1, "deepseek-chat", "题目正文", "", AccountTestOptions{TestQuestionID: 7}))
	require.Equal(t, int64(7), writer.input.TestQuestionID)
	require.Equal(t, TestRecordStatusSuccess, writer.input.Status)
	require.Len(t, ledger.logs, 1)
	require.Equal(t, int64(900), ledger.logs[0].UserID)
}

func TestAccountTestUsagePollingSnapshotsAreNotChargedTwice(t *testing.T) {
	ledger := &accountTestBillingStub{}
	session := &accountTestUsageSession{admin: &User{ID: 900}, key: &APIKey{ID: 901}, account: &Account{ID: 1}, billing: ledger, model: "grok-video"}
	for i := 0; i < 3; i++ {
		req, _ := http.NewRequestWithContext(context.WithValue(context.Background(), accountTestUsageContextKey{}, session), http.MethodGet, "https://mock.invalid/v1/videos/task1", nil)
		resp := observeAccountTestResponse(req, &http.Response{Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"usage":{"input_tokens":10,"output_tokens":2}}`))})
		_, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}
	require.NoError(t, session.finish(context.Background()))
	require.Len(t, ledger.logs, 1)
	require.Equal(t, 10, ledger.logs[0].InputTokens)
}

func TestAccountTestUsageSyntheticDoesNotRequireLedger(t *testing.T) {
	account := adaptiveCNAccountTestAccount(1, PlatformDeepseek)
	account.Extra = map[string]any{"synthetic_ui_test": true}
	svc, upstream := adaptiveCNAccountTestService(account)
	svc.testBilling = nil
	c, _ := newTestContext()
	require.NoError(t, svc.TestAccountConnection(c, 1, "deepseek-chat", "", ""))
	require.Empty(t, upstream.requests)
}

func TestAccountTestUsageCompletionWaitsForLedger(t *testing.T) {
	account := adaptiveCNAccountTestAccount(1, PlatformDeepseek)
	account.Credentials["api_protocol"] = APIProtocolChatCompletions
	resp := func() *http.Response {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("data: {\"usage\":{\"prompt_tokens\":3}}\n\ndata: [DONE]\n\n"))}
	}
	svc, _ := adaptiveCNAccountTestService(account, resp())
	svc.testBilling = &accountTestBillingStub{recordErr: errors.New("ledger unavailable")}
	c, rec := newTestContext()
	require.ErrorContains(t, svc.TestAccountConnection(c, 1, "deepseek-chat", "", ""), "ledger unavailable")
	require.NotContains(t, rec.Body.String(), `"success":true`)

	ledger := &accountTestBillingStub{pricing: NewBillingService(nil, nil)}
	svc, upstream := adaptiveCNAccountTestService(account, resp())
	svc.testBilling = ledger
	c, _ = newTestContext()
	require.ErrorContains(t, svc.TestAccountConnection(c, 1, "definitely-unpriced-test-model", "", ""), "已停止调用上游")
	require.Empty(t, upstream.requests)
}

func TestAccountTestUsageRefusesUnavailableBilling(t *testing.T) {
	account := adaptiveCNAccountTestAccount(1, PlatformDeepseek)
	svc, upstream := adaptiveCNAccountTestService(account)
	svc.testBilling = nil
	c, _ := newTestContext()
	require.ErrorContains(t, svc.TestAccountConnection(c, 1, "deepseek-chat", "", ""), "计费服务未配置")
	require.Empty(t, upstream.requests)
	svc.testBilling = &accountTestBillingStub{prepareErr: errors.New("missing admin")}
	c, _ = newTestContext()
	require.ErrorContains(t, svc.TestAccountConnection(c, 1, "deepseek-chat", "", ""), "missing admin")
	require.Empty(t, upstream.requests)
}
func TestAccountTestUsageSSEErrorStillRecords(t *testing.T) {
	account := adaptiveCNAccountTestAccount(1, PlatformDeepseek)
	account.Credentials["api_protocol"] = APIProtocolAnthropic
	svc, _ := adaptiveCNAccountTestService(account, &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":17}}}\n\ndata: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":3}}\n\ndata: {\"type\":\"error\",\"error\":{\"message\":\"interrupted\"}}\n\n"))})
	ledger := &accountTestBillingStub{}
	svc.testBilling = ledger
	c, rec := newTestContext()
	require.ErrorContains(t, svc.TestAccountConnection(c, 1, "deepseek-chat", "", ""), "interrupted")
	require.Len(t, ledger.logs, 1)
	require.Equal(t, 17, ledger.logs[0].InputTokens)
	require.Equal(t, 3, ledger.logs[0].OutputTokens)
	require.NotContains(t, rec.Body.String(), `"success":true`)
}

func TestAccountTestUsageFailureStillRecordsAndReportsLedgerError(t *testing.T) {
	account := adaptiveCNAccountTestAccount(1, PlatformDeepseek)
	account.Credentials["api_protocol"] = APIProtocolAnthropic
	svc, _ := adaptiveCNAccountTestService(account, &http.Response{StatusCode: 500, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"usage":{"input_tokens":17,"output_tokens":2},"error":"failed"}`))})
	ledger := &accountTestBillingStub{}
	svc.testBilling = ledger
	c, _ := newTestContext()
	require.Error(t, svc.TestAccountConnection(c, 1, "deepseek-chat", "", ""))
	require.Len(t, ledger.logs, 1)
	require.Equal(t, 17, ledger.logs[0].InputTokens)
	ledger.recordErr = errors.New("ledger unavailable")
	session := &accountTestUsageSession{admin: &User{ID: 900}, key: &APIKey{ID: 901}, account: account, billing: ledger, requests: []*accountTestUsageRequest{{id: "stable", seen: true, started: time.Now()}}}
	require.ErrorContains(t, session.finish(context.Background()), "ledger unavailable")
}
