package handler

import (
	"log"
	"sort"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/handler/admin"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// userTestAccountItem 是普通用户可见的账号概要：
// 只暴露测试所需的最小字段，不包含凭据、代理、用量等管理端信息。
type userTestAccountItem struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Type     string `json:"type"`
	Status   string `json:"status"`
}

// UserTestCenterHandler 提供普通用户侧的测试题库与题目测试能力。
//
// 权限边界：
//   - 题库只读 + 新建：复用管理端 handler，但不挂载更新/删除路由；
//   - 测试历史：可见该题目的全部记录（与管理员同口径）；
//   - 账号范围：只允许用户可用分组内的活跃账号，避免消耗无关账号额度。
type UserTestCenterHandler struct {
	apiKeyService  *service.APIKeyService
	accountService *service.AccountService
	questions      *admin.TestQuestionHandler
	records        *admin.TestRecordHandler
	accounts       *admin.AccountHandler
}

func NewUserTestCenterHandler(
	apiKeyService *service.APIKeyService,
	accountService *service.AccountService,
	questions *admin.TestQuestionHandler,
	records *admin.TestRecordHandler,
	accounts *admin.AccountHandler,
) *UserTestCenterHandler {
	return &UserTestCenterHandler{
		apiKeyService:  apiKeyService,
		accountService: accountService,
		questions:      questions,
		records:        records,
		accounts:       accounts,
	}
}

// ListQuestions 返回题库全部题目。
// GET /api/v1/test-center/questions
func (h *UserTestCenterHandler) ListQuestions(c *gin.Context) {
	h.questions.List(c)
}

// CreateQuestion 新建题目。
// POST /api/v1/test-center/questions
func (h *UserTestCenterHandler) CreateQuestion(c *gin.Context) {
	h.questions.Create(c)
}

// ListQuestionRecords 返回某题目的测试历史。
// GET /api/v1/test-center/questions/:id/records
func (h *UserTestCenterHandler) ListQuestionRecords(c *gin.Context) {
	h.records.ListByQuestion(c)
}

// GetRecord 返回单条测试记录详情。
// GET /api/v1/test-center/records/:id
func (h *UserTestCenterHandler) GetRecord(c *gin.Context) {
	h.records.Get(c)
}

// GetRecordFile 返回测试结果文件内容。
// GET /api/v1/test-center/records/:id/files/:file_id
func (h *UserTestCenterHandler) GetRecordFile(c *gin.Context) {
	h.records.File(c)
}

// ListAccounts 返回当前用户可用分组内的活跃账号（脱敏）。
// GET /api/v1/test-center/accounts
func (h *UserTestCenterHandler) ListAccounts(c *gin.Context) {
	accounts, err := h.allowedAccounts(c)
	if err != nil {
		log.Printf("[测试题库] 查询可用账号失败: %v", err)
		response.InternalError(c, "查询可用账号失败")
		return
	}
	items := make([]userTestAccountItem, 0, len(accounts))
	for i := range accounts {
		items = append(items, userTestAccountItem{
			ID:       accounts[i].ID,
			Name:     accounts[i].Name,
			Platform: accounts[i].Platform,
			Type:     accounts[i].Type,
			Status:   accounts[i].Status,
		})
	}
	response.Success(c, items)
}

// GetAccountModels 返回指定账号可测试的模型。
// GET /api/v1/test-center/accounts/:id/models
func (h *UserTestCenterHandler) GetAccountModels(c *gin.Context) {
	if !h.requireAllowedAccount(c) {
		return
	}
	h.accounts.GetAvailableModels(c)
}

// TestAccount 对指定账号发起题目测试（SSE）。
// POST /api/v1/test-center/accounts/:id/test
func (h *UserTestCenterHandler) TestAccount(c *gin.Context) {
	if !h.requireAllowedAccount(c) {
		return
	}
	h.accounts.Test(c)
}

// allowedAccounts 汇总用户可用分组下的活跃账号，按 ID 排序去重。
func (h *UserTestCenterHandler) allowedAccounts(c *gin.Context) ([]service.Account, error) {
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok {
		return nil, infraerrors.Unauthorized("TEST_CENTER_UNAUTHENTICATED", "用户未认证")
	}
	if h.apiKeyService == nil || h.accountService == nil {
		return nil, infraerrors.InternalServer("TEST_CENTER_UNAVAILABLE", "测试题库服务不可用")
	}

	// 管理员可选全部活跃账号（账号管理本就是其职权范围）；
	// 普通用户仅限自身可用分组内的账号。
	if role, ok := middleware.GetUserRoleFromContext(c); ok && role == service.RoleAdmin {
		accounts, err := h.accountService.ListActive(c.Request.Context())
		if err != nil {
			return nil, err
		}
		sort.Slice(accounts, func(i, j int) bool { return accounts[i].ID < accounts[j].ID })
		return accounts, nil
	}

	groups, err := h.apiKeyService.GetAvailableGroups(c.Request.Context(), subject.UserID)
	if err != nil {
		return nil, err
	}

	seen := make(map[int64]struct{})
	accounts := make([]service.Account, 0, 16)
	for _, group := range groups {
		groupAccounts, err := h.accountService.ListByGroup(c.Request.Context(), group.ID)
		if err != nil {
			return nil, err
		}
		for i := range groupAccounts {
			account := groupAccounts[i]
			if _, exists := seen[account.ID]; exists {
				continue
			}
			seen[account.ID] = struct{}{}
			accounts = append(accounts, account)
		}
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].ID < accounts[j].ID })
	return accounts, nil
}

// requireAllowedAccount 校验路径中的账号属于用户可用范围；不通过时直接写出 403。
func (h *UserTestCenterHandler) requireAllowedAccount(c *gin.Context) bool {
	accountID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || accountID <= 0 {
		response.BadRequest(c, "账号 ID 不合法")
		return false
	}

	accounts, err := h.allowedAccounts(c)
	if err != nil {
		log.Printf("[测试题库] 校验可用账号失败: %v", err)
		response.InternalError(c, "查询可用账号失败")
		return false
	}
	for i := range accounts {
		if accounts[i].ID == accountID {
			return true
		}
	}
	log.Printf("[测试题库] 用户尝试测试未授权账号 account=%d", accountID)
	response.Forbidden(c, "该账号不在你的可用范围内")
	return false
}
