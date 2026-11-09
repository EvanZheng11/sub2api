package service

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/google/uuid"
	"github.com/tidwall/gjson"
)

const accountTestBillingKeyName = "系统账号测试（仅记账）"

type accountTestUsageContextKey struct{}

type accountTestUsageBilling interface {
	prepare(context.Context) (*User, *APIKey, error)
	record(context.Context, *UsageLog) error
	calculate(context.Context, *accountTestUsageRequest) (*CostBreakdown, error)
}

// 测试 Key 仅满足 usage_logs 的归属外键，禁用且不绑定生产分组，不参与认证、套餐或 Key 配额。
type accountTestBilling struct {
	users   UserRepository
	keys    APIKeyRepository
	logs    UsageLogRepository
	client  *dbent.Client
	pricing *BillingService
	cache   interface {
		InvalidateUserBalance(context.Context, int64) error
	}
	mu sync.Mutex
}

func (b *accountTestBilling) prepare(ctx context.Context) (*User, *APIKey, error) {
	if b == nil || b.users == nil || b.keys == nil || b.logs == nil || b.client == nil || b.pricing == nil {
		return nil, nil, errors.New("测试计费服务未配置")
	}
	admin, err := b.users.GetFirstAdmin(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("获取测试费用管理员: %w", err)
	}
	if admin == nil || !admin.IsAdmin() || !admin.IsActive() {
		return nil, nil, errors.New("测试费用管理员不存在或已停用")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for page := 1; ; page++ {
		keys, result, err := b.keys.ListByUserID(ctx, admin.ID, pagination.PaginationParams{Page: page, PageSize: 100}, APIKeyListFilters{})
		if err != nil {
			return nil, nil, fmt.Errorf("读取管理员测试 Key: %w", err)
		}
		for i := range keys {
			key := &keys[i]
			if key.Name != accountTestBillingKeyName {
				continue
			}
			if key.UserID != admin.ID || key.Status != StatusAPIKeyDisabled || key.GroupID != nil || key.Quota != 0 || key.HasRateLimits() || key.ExpiresAt != nil {
				return nil, nil, errors.New("系统测试 Key 配置被修改，请恢复禁用、无分组、无配额、无过期时间状态")
			}
			return admin, key, nil
		}
		if result == nil || page >= result.Pages {
			break
		}
	}
	key := &APIKey{UserID: admin.ID, Name: accountTestBillingKeyName, Key: "sk-test-ledger-" + uuid.NewString(), Status: StatusAPIKeyDisabled}
	if err := b.keys.Create(ctx, key); err != nil {
		return nil, nil, fmt.Errorf("创建管理员测试 Key: %w", err)
	}
	return admin, key, nil
}

func (b *accountTestBilling) calculate(ctx context.Context, r *accountTestUsageRequest) (*CostBreakdown, error) {
	return b.pricing.CalculateCostUnified(CostInput{Ctx: ctx, Model: r.model, Tokens: r.tokens, RateMultiplier: 1, ServiceTier: r.tier, ReasoningEffort: r.effort})
}

// 日志插入与余额后扣共用现有 Ent 事务；唯一键命中时不再次扣费。
func (b *accountTestBilling) record(ctx context.Context, log *UsageLog) error {
	tx, err := b.client.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	txCtx := dbent.NewTxContext(ctx, tx)
	inserted, err := b.logs.Create(txCtx, log)
	if err != nil {
		return err
	}
	if inserted && log.ActualCost > 0 {
		if err := b.users.DeductBalance(txCtx, log.UserID, log.ActualCost); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if inserted && log.ActualCost > 0 && b.cache != nil {
		// 测试无在途预留，提交后直接失效，避免管理员随后读到旧余额。
		if err := b.cache.InvalidateUserBalance(ctx, log.UserID); err != nil {
			return fmt.Errorf("测试已入账但余额缓存失效失败: %w", err)
		}
	}
	return nil
}

type accountTestUsageSession struct {
	admin      *User
	key        *APIKey
	account    *Account
	billing    accountTestUsageBilling
	requests   []*accountTestUsageRequest
	model      string
	completion *TestEvent
	finishing  bool
}

type accountTestUsageRequest struct {
	id, model, endpoint, tier, effort    string
	started                              time.Time
	stream                               bool
	seen                                 bool
	tokens                               UsageTokens
	rawInput, rawCandidates, rawThoughts int
	captureErr                           error
}

func (s *accountTestUsageSession) finish(ctx context.Context) error {
	var errs []error
	for _, r := range s.requests {
		if r.captureErr != nil {
			errs = append(errs, r.captureErr)
		}
		if !r.seen {
			continue
		}
		cost, err := s.billing.calculate(ctx, r)
		if err != nil {
			errs = append(errs, fmt.Errorf("测试模型 %s 无法计价: %w", r.model, err))
			continue
		}
		duration := int(time.Since(r.started).Milliseconds())
		log := &UsageLog{UserID: s.admin.ID, APIKeyID: s.key.ID, AccountID: s.account.ID, RequestID: r.id, Model: r.model,
			InputTokens: r.tokens.InputTokens, OutputTokens: r.tokens.OutputTokens, CacheReadTokens: r.tokens.CacheReadTokens,
			CacheCreationTokens: r.tokens.CacheCreationTokens, CacheCreation5mTokens: r.tokens.CacheCreation5mTokens, CacheCreation1hTokens: r.tokens.CacheCreation1hTokens,
			CacheCreation30mTokens: r.tokens.CacheCreation30mTokens, CacheCreation12hTokens: r.tokens.CacheCreation12hTokens,
			ImageInputTokens: r.tokens.ImageInputTokens, ImageOutputTokens: r.tokens.ImageOutputTokens,
			InputCost: cost.InputCost, OutputCost: cost.OutputCost, CacheReadCost: cost.CacheReadCost, CacheCreationCost: cost.CacheCreationCost,
			ImageInputCost: cost.ImageInputCost, ImageOutputCost: cost.ImageOutputCost, TotalCost: QuantizeUsageBillingAmount(cost.TotalCost), ActualCost: QuantizeUsageBillingAmount(cost.ActualCost),
			RateMultiplier: 1, BillingType: BillingTypeBalance, Stream: r.stream, DurationMs: &duration, UpstreamEndpoint: &r.endpoint}
		if err := s.billing.record(ctx, log); err != nil {
			errs = append(errs, fmt.Errorf("测试使用量入账 %s: %w", r.id, err))
		}
	}
	return errors.Join(errs...)
}

// 每个 HTTP 响应单独收集快照；adaptive 多探测/重试不会相互覆盖。读取器不改变原响应内容。
type accountTestUsageUpstream struct{ HTTPUpstream }

func (u accountTestUsageUpstream) Do(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
	if err := validateAccountTestUsageRequest(req); err != nil {
		return nil, err
	}
	resp, err := u.HTTPUpstream.Do(req, proxy, id, concurrency)
	return observeAccountTestResponse(req, resp), err
}
func (u accountTestUsageUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, profile *tlsfingerprint.Profile) (*http.Response, error) {
	if err := validateAccountTestUsageRequest(req); err != nil {
		return nil, err
	}
	resp, err := u.HTTPUpstream.DoWithTLS(req, proxy, id, concurrency, profile)
	return observeAccountTestResponse(req, resp), err
}
func observeAccountTestResponse(req *http.Request, resp *http.Response) *http.Response {
	session, _ := req.Context().Value(accountTestUsageContextKey{}).(*accountTestUsageSession)
	if session == nil || resp == nil || resp.Body == nil {
		return resp
	}
	if !accountTestRequestUsesTokenBilling(req, session) {
		return resp
	}
	r := accountTestUsageRequestFromHTTP(req)
	// 视频状态轮询返回同一生成任务的累计 usage，不是新的模型消耗。
	if req.Method == http.MethodGet {
		for _, previous := range session.requests {
			if previous.endpoint == req.URL.Path {
				r = previous
				break
			}
		}
	}
	r.stream = strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream")
	found := false
	for _, previous := range session.requests {
		if previous == r {
			found = true
			break
		}
	}
	if !found {
		session.requests = append(session.requests, r)
	}
	resp.Body = &accountTestUsageBody{ReadCloser: resp.Body, request: r}
	return resp
}

func validateAccountTestUsageRequest(req *http.Request) error {
	session, _ := req.Context().Value(accountTestUsageContextKey{}).(*accountTestUsageSession)
	if session == nil || req.Method != http.MethodPost || !accountTestRequestUsesTokenBilling(req, session) {
		return nil
	}
	r := accountTestUsageRequestFromHTTP(req)
	if r.model == "" {
		return errors.New("测试请求缺少可计价模型，已停止调用上游")
	}
	if _, err := session.billing.calculate(req.Context(), r); err != nil {
		return fmt.Errorf("测试模型 %s 无法计价，已停止调用上游: %w", r.model, err)
	}
	return nil
}

// Grok 媒体按时长、字符或张数计费，不拿文本 token 单价拦截原有测试。
func accountTestRequestUsesTokenBilling(req *http.Request, session *accountTestUsageSession) bool {
	if session.account == nil || session.account.Platform != PlatformGrok {
		return true
	}
	path := req.URL.Path
	return !strings.Contains(path, "/images/") && !strings.Contains(path, "/videos/") &&
		!strings.HasSuffix(path, "/tts") && !strings.HasSuffix(path, "/stt")
}

func accountTestUsageRequestFromHTTP(req *http.Request) *accountTestUsageRequest {
	r := &accountTestUsageRequest{id: "account-test:" + uuid.NewString(), endpoint: req.URL.Path, started: time.Now()}
	if req.GetBody != nil {
		body, err := req.GetBody()
		if err == nil {
			data, _ := io.ReadAll(io.LimitReader(body, 2<<20))
			_ = body.Close()
			r.model = gjson.GetBytes(data, "model").String()
			r.tier = gjson.GetBytes(data, "service_tier").String()
			r.effort = gjson.GetBytes(data, "reasoning.effort").String()
			if r.effort == "" {
				r.effort = gjson.GetBytes(data, "reasoning_effort").String()
			}
			if r.effort == "" {
				r.effort = gjson.GetBytes(data, "output_config.effort").String()
			}
		}
	}
	if r.model == "" {
		if at := strings.Index(req.URL.Path, "/models/"); at >= 0 {
			r.model = strings.Split(req.URL.Path[at+8:], ":")[0]
		}
	}
	if r.model == "" {
		if at := strings.Index(req.URL.Path, "/model/"); at >= 0 {
			r.model, _ = url.PathUnescape(strings.Split(req.URL.Path[at+7:], "/")[0])
		}
	}
	if r.model == "" {
		if session, _ := req.Context().Value(accountTestUsageContextKey{}).(*accountTestUsageSession); session != nil {
			r.model = session.model
		}
	}
	return r
}

type accountTestUsageBody struct {
	io.ReadCloser
	request  *accountTestUsageRequest
	pending  []byte
	overflow bool
	closed   bool
	jsonBody []byte
	isSSE    bool
	detected bool
}

func (b *accountTestUsageBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.capture(p[:n])
	if err != nil {
		b.flush()
	}
	return n, err
}
func (b *accountTestUsageBody) Close() error {
	if !b.closed {
		b.closed = true
		b.flush()
	}
	return b.ReadCloser.Close()
}
func (b *accountTestUsageBody) capture(data []byte) {
	if !b.detected {
		if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 {
			b.detected = true
			b.isSSE = trimmed[0] != '{' && trimmed[0] != '['
		}
	}
	if !b.isSSE {
		if len(b.jsonBody)+len(data) <= 16<<20 {
			b.jsonBody = append(b.jsonBody, data...)
		} else {
			b.overflow = true
			b.request.captureErr = errors.New("测试 JSON 响应超过 16 MiB，无法完整采集 usage")
		}
		return
	}
	// 按行观察 SSE，也接受普通 JSON；只保留一行，避免图片/音频响应无界占用内存。
	for len(data) > 0 {
		at := bytes.IndexByte(data, '\n')
		part := data
		if at >= 0 {
			part = data[:at]
		}
		if !b.overflow {
			if len(b.pending)+len(part) > 16<<20 {
				b.pending = nil
				b.overflow = true
				b.request.captureErr = errors.New("测试 SSE 单行超过 16 MiB，无法完整采集 usage")
			} else {
				b.pending = append(b.pending, part...)
			}
		}
		if at < 0 {
			break
		}
		b.flush()
		data = data[at+1:]
	}
}
func (b *accountTestUsageBody) flush() {
	if !b.isSSE {
		if !b.overflow && gjson.ValidBytes(b.jsonBody) {
			b.request.observe(gjson.ParseBytes(b.jsonBody))
		}
		b.jsonBody = nil
		return
	}
	if !b.overflow {
		data := bytes.TrimSpace(b.pending)
		if bytes.HasPrefix(data, []byte("data:")) {
			data = bytes.TrimSpace(data[5:])
			b.request.stream = true
		}
		if gjson.ValidBytes(data) {
			b.request.observe(gjson.ParseBytes(data))
		}
	}
	b.pending = nil
	b.overflow = false
}

func (r *accountTestUsageRequest) observe(root gjson.Result) {
	if root.IsArray() {
		for _, item := range root.Array() {
			r.observe(item)
		}
		return
	}
	// Gemini CLI / Antigravity 与 Responses 的封装响应。
	if nested := root.Get("response"); nested.IsObject() {
		r.observe(nested)
	}
	if nested := root.Get("message"); nested.IsObject() {
		r.observe(nested)
	}
	if model := root.Get("model").String(); r.model == "" && model != "" {
		r.model = model
	}
	u := root.Get("usage")
	gemini := root.Get("usageMetadata")
	if gemini.IsObject() {
		u = gemini
	}
	if !u.IsObject() {
		return
	}
	set := func(dst *int, key string) {
		if v := u.Get(key); v.Exists() && v.Type == gjson.Number && v.Int() >= 0 {
			// 兼容 Anthropic 的 delta 常用 0 表示本帧未更新，不能清掉 start 的累计用量。
			if root.Get("type").String() == "message_delta" && v.Int() == 0 && *dst > 0 {
				return
			}
			*dst = int(v.Int())
			r.seen = true
		}
	}
	if gemini.IsObject() {
		set(&r.rawInput, "promptTokenCount")
		set(&r.tokens.CacheReadTokens, "cachedContentTokenCount")
		set(&r.rawCandidates, "candidatesTokenCount")
		set(&r.rawThoughts, "thoughtsTokenCount")
		r.tokens.OutputTokens = r.rawCandidates + r.rawThoughts
		r.tokens.InputTokens = max(0, r.rawInput-r.tokens.CacheReadTokens)
		if details := u.Get("candidatesTokensDetails"); details.IsArray() {
			r.tokens.ImageOutputTokens = 0
			for i, detail := range details.Array() {
				if detail.Get("modality").String() == "IMAGE" {
					set(&r.tokens.ImageOutputTokens, fmt.Sprintf("candidatesTokensDetails.%d.tokenCount", i))
					break
				}
			}
		}
		return
	}
	// Anthropic message_delta 只携带累计 output，未提供的 input/cache 保留先前快照。
	set(&r.rawInput, "input_tokens")
	set(&r.rawInput, "prompt_tokens")
	r.tokens.InputTokens = r.rawInput
	set(&r.tokens.OutputTokens, "output_tokens")
	set(&r.tokens.OutputTokens, "completion_tokens")
	set(&r.tokens.CacheCreationTokens, "cache_creation_input_tokens")
	set(&r.tokens.CacheCreation5mTokens, "cache_creation.ephemeral_5m_input_tokens")
	set(&r.tokens.CacheCreation1hTokens, "cache_creation.ephemeral_1h_input_tokens")
	set(&r.tokens.CacheReadTokens, "cache_read_input_tokens")
	if u.Get("prompt_tokens").Exists() || u.Get("input_tokens_details").Exists() || u.Get("prompt_cache_hit_tokens").Exists() || u.Get("cache_write_tokens").Exists() || u.Get("cache_write_input_tokens").Exists() {
		// OpenAI 的输入总量包含缓存读写，复用网关的聚合与 TTL 校验，避免重复计费。
		usage, _ := openAIUsageFromGJSON(u)
		r.tokens.CacheReadTokens = usage.CacheReadInputTokens
		r.tokens.CacheCreationTokens = usage.CacheCreationInputTokens
		r.tokens.CacheCreation30mTokens = usage.CacheCreation30mInputTokens
		r.tokens.CacheCreation12hTokens = usage.CacheCreation12hInputTokens
		r.tokens.InputTokens = max(0, r.rawInput-r.tokens.CacheReadTokens-r.tokens.CacheCreationTokens)
	}
	set(&r.tokens.ImageOutputTokens, "output_tokens_details.image_tokens")
	set(&r.tokens.ImageInputTokens, "input_tokens_details.image_tokens")
	if strings.Contains(r.endpoint, "/images/") && !u.Get("output_tokens_details.image_tokens").Exists() {
		// 原生 Images 端点没有文本输出，沿用网关的图片 token 回退口径。
		r.tokens.ImageOutputTokens = r.tokens.OutputTokens
	}
}
