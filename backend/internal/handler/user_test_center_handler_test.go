package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// 仅用于验证可用账号范围过滤：只关心分组 → 账号的映射。
type testCenterGroupRepo struct {
	service.GroupRepository
	groups []service.Group
}

func (r *testCenterGroupRepo) ListActive(context.Context) ([]service.Group, error) {
	return r.groups, nil
}

type testCenterUserRepo struct {
	service.UserRepository
	user *service.User
}

func (r *testCenterUserRepo) GetByID(context.Context, int64) (*service.User, error) {
	return r.user, nil
}

type testCenterSubRepo struct {
	service.UserSubscriptionRepository
}

func (r *testCenterSubRepo) ListActiveByUserID(context.Context, int64) ([]service.UserSubscription, error) {
	return nil, nil
}

type testCenterAccountRepo struct {
	service.AccountRepository
	byGroup map[int64][]service.Account
}

func (r *testCenterAccountRepo) ListByGroup(_ context.Context, groupID int64) ([]service.Account, error) {
	return r.byGroup[groupID], nil
}

func newTestCenterRouter(apiKeyService *service.APIKeyService, accountService *service.AccountService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	h := NewUserTestCenterHandler(apiKeyService, accountService, admin.NewTestQuestionHandler(nil), admin.NewTestRecordHandler(nil), admin.NewAccountHandler(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 7})
		c.Next()
	})
	router.GET("/api/v1/test-center/accounts", h.ListAccounts)
	router.POST("/api/v1/test-center/accounts/:id/test", h.TestAccount)
	return router
}

func TestUserTestCenterListAccountsScopesToAvailableGroups(t *testing.T) {
	groupRepo := &testCenterGroupRepo{groups: []service.Group{{ID: 11, Name: "公开分组", Status: service.StatusActive}}}
	userRepo := &testCenterUserRepo{user: &service.User{ID: 7, Role: service.RoleUser}}
	subRepo := &testCenterSubRepo{}
	apiKeyService := service.NewAPIKeyService(nil, userRepo, groupRepo, subRepo, nil, nil, nil)
	accountService := service.NewAccountService(&testCenterAccountRepo{
		byGroup: map[int64][]service.Account{
			11: {
				{ID: 21, Name: "账号A", Platform: service.PlatformAnthropic, Type: service.AccountTypeOAuth, Status: service.StatusActive},
				{ID: 22, Name: "账号B", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive},
			},
		},
	}, nil)

	router := newTestCenterRouter(apiKeyService, accountService)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/test-center/accounts", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	var resp struct {
		Data []userTestAccountItem `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Len(t, resp.Data, 2)
	require.Equal(t, int64(21), resp.Data[0].ID)
	// 只暴露最小字段：凭据等管理端信息不应出现在响应里
	require.NotContains(t, rec.Body.String(), "credentials")
}

func TestUserTestCenterTestAccountRejectsAccountOutsideUserScope(t *testing.T) {
	groupRepo := &testCenterGroupRepo{groups: []service.Group{{ID: 11, Name: "公开分组", Status: service.StatusActive}}}
	userRepo := &testCenterUserRepo{user: &service.User{ID: 7, Role: service.RoleUser}}
	subRepo := &testCenterSubRepo{}
	apiKeyService := service.NewAPIKeyService(nil, userRepo, groupRepo, subRepo, nil, nil, nil)
	accountService := service.NewAccountService(&testCenterAccountRepo{
		byGroup: map[int64][]service.Account{
			11: {{ID: 21, Name: "账号A", Platform: service.PlatformAnthropic, Status: service.StatusActive}},
		},
	}, nil)

	router := newTestCenterRouter(apiKeyService, accountService)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/test-center/accounts/99/test", nil))

	require.Equal(t, http.StatusForbidden, rec.Code)
}
