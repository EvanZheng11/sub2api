package admin

import (
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/gin-gonic/gin"
)

// TestRecordHandler 提供测试历史的列表、详情与结果文件读取。
type TestRecordHandler struct {
	testRecordService *service.TestRecordService
}

func NewTestRecordHandler(testRecordService *service.TestRecordService) *TestRecordHandler {
	return &TestRecordHandler{testRecordService: testRecordService}
}

// ListByQuestion 返回某题目的测试历史，按时间倒序分页。
// GET /api/v1/admin/test-questions/:id/records
func (h *TestRecordHandler) ListByQuestion(c *gin.Context) {
	questionID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || questionID <= 0 {
		response.BadRequest(c, "题目 ID 不合法")
		return
	}

	page, pageSize := response.ParsePagination(c)
	items, total, err := h.testRecordService.ListByQuestion(c.Request.Context(), questionID, page, pageSize)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	// 列表接口不返回完整文本；仓储通常不查该列，这里兜底清空。
	for i := range items {
		items[i].ResponseText = ""
	}
	response.Paginated(c, items, total, page, pageSize)
}

// Get 返回单条测试记录详情，含完整文本与文件元数据。
// GET /api/v1/admin/test-records/:id
func (h *TestRecordHandler) Get(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "记录 ID 不合法")
		return
	}

	record, err := h.testRecordService.GetByID(c.Request.Context(), id)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, record)
}

// File 返回测试结果文件的二进制内容，?download=1 时作为附件下载。
// GET /api/v1/admin/test-records/:id/files/:file_id
func (h *TestRecordHandler) File(c *gin.Context) {
	recordID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || recordID <= 0 {
		response.BadRequest(c, "记录 ID 不合法")
		return
	}
	fileID, err := strconv.ParseInt(c.Param("file_id"), 10, 64)
	if err != nil || fileID <= 0 {
		response.BadRequest(c, "文件 ID 不合法")
		return
	}

	content, err := h.testRecordService.OpenFile(c.Request.Context(), recordID, fileID)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if c.Query("download") == "1" {
		c.Header("Content-Disposition", `attachment; filename="`+content.FileName+`"`)
	}
	c.Data(http.StatusOK, content.MimeType, content.Data)
}
