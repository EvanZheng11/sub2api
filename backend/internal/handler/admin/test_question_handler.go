package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// TestQuestionHandler 处理管理员题库的增删改查。
type TestQuestionHandler struct {
	testQuestionService *service.TestQuestionService
}

func NewTestQuestionHandler(testQuestionService *service.TestQuestionService) *TestQuestionHandler {
	return &TestQuestionHandler{testQuestionService: testQuestionService}
}

// TestQuestionRequest 是创建/更新题目的请求体。
type TestQuestionRequest struct {
	Title  string `json:"title"`
	Prompt string `json:"prompt"`
	Type   string `json:"type"`
}

// List 返回全部题目（updated_at desc, id desc）。
// GET /api/v1/admin/test-questions
func (h *TestQuestionHandler) List(c *gin.Context) {
	items, err := h.testQuestionService.List(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if items == nil {
		items = []service.TestQuestion{}
	}
	response.Success(c, items)
}

// Create 新建题目。
// POST /api/v1/admin/test-questions
func (h *TestQuestionHandler) Create(c *gin.Context) {
	var req TestQuestionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求数据格式不正确")
		return
	}

	created, err := h.testQuestionService.Create(c.Request.Context(), service.TestQuestionInput{
		Title:  req.Title,
		Prompt: req.Prompt,
		Type:   req.Type,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, created)
}

// Update 更新题目。
// PUT /api/v1/admin/test-questions/:id
func (h *TestQuestionHandler) Update(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "题目 ID 不合法")
		return
	}

	var req TestQuestionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "请求数据格式不正确")
		return
	}

	updated, err := h.testQuestionService.Update(c.Request.Context(), id, service.TestQuestionInput{
		Title:  req.Title,
		Prompt: req.Prompt,
		Type:   req.Type,
	})
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, updated)
}

// Delete 删除题目。
// DELETE /api/v1/admin/test-questions/:id
func (h *TestQuestionHandler) Delete(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "题目 ID 不合法")
		return
	}

	if err := h.testQuestionService.Delete(c.Request.Context(), id); err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, gin.H{"message": "题目已删除"})
}
