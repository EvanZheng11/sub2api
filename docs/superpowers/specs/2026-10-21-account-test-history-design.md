# 账号测试历史记录设计

- 日期:2026-10-21
- 状态:设计已确认,待实施
- 分支:`worktrees/wt_test_history`
- 前置功能:测试题库与账号测试弹窗(见 `2026-10-20-account-test-bank-design.md`)

## 1. 背景与目标

测试题库页面(`TestQuestionsView.vue`)的题目操作列目前只有「编辑」「删除」。账号测试的结果**只存在于当前弹窗**,关闭即丢,无法回看同一次生成的文本、图片或文件。

本期实现:

1. 新增测试记录表与媒体文件表,持久化账号测试的结果。
2. 题目操作列新增「测试历史」按钮,弹框用表格**按时间倒序**分页列出该题目的历史测试记录。
3. 表格列:账号、模型、思考强度、测试时长、状态、结果按钮。
4. 结果按钮按记录内容分支:查看文本结果 / 查看图片 / 下载文件;失败记录提供查看错误。

非目标(YAGNI,本期不做):

- 全局/跨题目的历史视图、题目维度以外的筛选、按账号或模型的历史统计。
- 历史记录的「用相同参数重跑」。
- 定时任务(`RunTestBackground`)产生的测试写入历史。
- 结果内容的后台检索、全文搜索、版本对比。
- 音频/视频结果的前端播放(本期只落盘与下载,不做播放器)。

## 2. 已确认的关键决策

| 决策点 | 结论 |
| --- | --- |
| 历史范围 | 只列被点击这道题目的记录(题目维度) |
| 结果存储 | 文本入库;图片/音频/视频等二进制落磁盘,库中只存路径 |
| 保留策略 | 每题保留最近 N 条,超出自动裁剪(含磁盘文件) |
| 结果按钮 | 只读查看,不提供重跑 |

## 3. 结果捕获方案

### 3.1 方案对比

| 方案 | 做法 | 结论 |
| --- | --- | --- |
| A | `gin.Context` 挂 recorder,`sendEvent` 内累积,`TestAccountConnection` 的 `defer` 落库 | **采用** |
| B | handler 层用 `httptest.NewRecorder` 缓冲全部 SSE 再回放 | 破坏流式输出与「取消测试」语义,淘汰 |
| C | 前端 `test_complete` 后回调上报接口 | 可伪造、关页面即漏记、耗时由前端计时不可信,淘汰 |

### 3.2 方案 A 的落点

账号测试的所有平台分支(OpenAI / Gemini / Grok / Antigravity / Claude / Bedrock / 多协议国产厂商 / 模型路由)都在 `TestAccountConnection`(`backend/internal/service/account_test_service.go:408`)内 `return`,且所有 SSE 事件都经由 `sendEvent`(`account_test_service.go:3482`)发出。因此只需三处改动即可覆盖全部平台:

1. **入口**:`TestAccountConnection` 在拿到 account 与 effort 之后创建 recorder 并 `c.Set(accountTestRecorderContextKey, rec)`,记录 `startedAt`、`account_id`、`account_name`、`platform`、`model_id`、`reasoning_effort`、`test_question_id`。当 `test_question_id` 为空时 recorder 置为「不记录」,行为与今天完全一致。
2. **累积**:`sendEvent` 函数体开头追加一次 `accountTestRecorderFromContext(c).capture(event)`,按事件类型累积:

   | 事件 | 累积内容 |
   | --- | --- |
   | `content` | 追加到 `responseText` |
   | `image` / `audio` / `video` | 收集 `ImageURL`/`AudioURL`/`VideoURL`(完整 data URL) |
   | `error` | 记录 `errorMessage`,置 `failed` |
   | `test_complete` | `Success=true` 时置 `success`;`Success=false` 不置成功 |

   recorder 内部用 `sync.Mutex` 保护,兼容多 goroutine 发事件的平台分支。
3. **收尾**:`TestAccountConnection` 用 `defer` 调用 `rec.finalize()`:计算 `latency_ms`、兜底判定状态(既无 `error` 也无 `test_complete` 视为 `failed`)、解码并落盘媒体文件、插入记录与文件行、裁剪超出保留条数的旧记录。

`sendEvent` 的现有签名与全部调用点**不改动**,新增代码只在函数体开头;不引入任何反射式或接口化的拦截层。

### 3.3 落库的上下文与失败处理

- 落库使用 `context.Background()`,不使用请求 context:响应已经开始写出后请求 context 可能被取消。
- 落库失败(数据库或磁盘异常)**只记日志,不改变测试结果**:不向已开始的 SSE 流追加错误事件,不影响前端已经收到的结果。
- 写盘失败但入库成功:记录照常入库,`image_count` 记 0,对应文件行不写入,日志记录原始错误。

## 4. 数据模型

迁移文件:`backend/migrations/247_account_test_records.sql`,沿用 `backend/migrations/migrations.go` 的 `//go:embed *.sql` 单向执行机制。不提供 `.down.sql`(回滚 SQL 会被 embed 参与启动执行)。

### 4.1 `account_test_records`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | `BIGSERIAL PRIMARY KEY` | 记录 ID |
| `test_question_id` | `BIGINT NOT NULL REFERENCES test_questions(id) ON DELETE CASCADE` | 所属题目;题目删除时历史一并删除 |
| `question_title` | `VARCHAR(200) NOT NULL DEFAULT ''` | 题目标题快照 |
| `account_id` | `BIGINT REFERENCES accounts(id) ON DELETE SET NULL` | 账号删除后历史仍可读 |
| `account_name` | `VARCHAR(200) NOT NULL DEFAULT ''` | 账号名快照 |
| `platform` | `VARCHAR(50) NOT NULL DEFAULT ''` | 账号平台 |
| `model` | `VARCHAR(200) NOT NULL DEFAULT ''` | 请求的模型 ID(不做各平台兜底后的实际值回填) |
| `reasoning_effort` | `VARCHAR(20) NOT NULL DEFAULT ''` | 实际生效的思考强度,无则空串 |
| `status` | `VARCHAR(16) NOT NULL DEFAULT 'success'` | `success` / `failed` |
| `error_message` | `TEXT NOT NULL DEFAULT ''` | 失败原因,成功为空串 |
| `response_text` | `TEXT NOT NULL DEFAULT ''` | 文本结果,超上限截断 |
| `response_truncated` | `BOOLEAN NOT NULL DEFAULT FALSE` | `response_text` 是否被截断 |
| `image_count` | `INTEGER NOT NULL DEFAULT 0` | 落盘成功的图片数量 |
| `latency_ms` | `BIGINT NOT NULL DEFAULT 0` | 测试耗时(毫秒) |
| `started_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | 开始时间 |
| `finished_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | 结束时间 |
| `created_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | 入库时间,列表按此倒序 |

约束:

- `account_test_records_status_check`:`CHECK (status IN ('success', 'failed'))`
- `account_test_records_latency_check`:`CHECK (latency_ms >= 0)`

索引:

- `idx_account_test_records_question_created ON account_test_records (test_question_id, created_at DESC, id DESC)`,与列表 `ORDER BY created_at DESC, id DESC` 对齐。

`response_text` 上限 64KB(超出按字节安全截断并置 `response_truncated`);该上限是应用层常量,不在 SQL 中加 CHECK。

### 4.2 `account_test_record_files`

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | `BIGSERIAL PRIMARY KEY` | 文件 ID |
| `record_id` | `BIGINT NOT NULL REFERENCES account_test_records(id) ON DELETE CASCADE` | 所属记录 |
| `kind` | `VARCHAR(16) NOT NULL` | `image` / `audio` / `video` |
| `mime_type` | `VARCHAR(100) NOT NULL DEFAULT ''` | 原始 data URL 的 MIME |
| `path` | `VARCHAR(500) NOT NULL` | 相对 `<DATA_DIR>/test-records` 的路径 |
| `size_bytes` | `BIGINT NOT NULL DEFAULT 0` | 文件大小 |
| `created_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | 创建时间 |

约束:`account_test_record_files_kind_check`:`CHECK (kind IN ('image', 'audio', 'video'))`。

索引:`idx_account_test_record_files_record ON account_test_record_files (record_id)`。

### 4.3 落盘路径

目录解析沿用 `resolvePluginRootDir`(`backend/internal/service/plugin_package.go:45`)的约定:

- 基准目录:`DATA_DIR` 环境变量,未设置时为 `./data`。
- 记录目录:`<基准>/test-records/<record_id>/`。
- 单条记录内的文件命名:`<序号>.<扩展名>`,扩展名由 MIME 推导(`image/png` → `.png`),推导失败时用 `bin`。
- 目录权限 `0o700`,文件权限 `0o600`。
- `path` 存相对路径,接口读取时与基准目录拼接,并做越界检查(拒绝含 `..` 或绝对路径的记录值)。

## 5. 保留策略

- 保留条数:每题最近 **20** 条,常量 `testRecordKeepPerQuestion`,可在 service 层调整,**不引入配置项**(本期无外部可调需求)。
- 裁剪时机:每次新记录入库后立即执行,与 `scheduled_test_results` 的 `PruneOldResults`(`backend/internal/repository/scheduled_test_repo.go:142`)同一时机。
- 裁剪 SQL:按 `(test_question_id, created_at DESC, id DESC)` 取超出部分:

  ```sql
  DELETE FROM account_test_records
  WHERE id IN (
      SELECT id FROM (
          SELECT id, ROW_NUMBER() OVER (
              PARTITION BY test_question_id ORDER BY created_at DESC, id DESC
          ) AS rn
          FROM account_test_records
      ) ranked
      WHERE rn > $1
  )
  ```

- 裁剪前先按记录 ID 删除磁盘目录;文件删除失败只记日志,不阻断数据库裁剪。
- 题目删除(`TestQuestionService.Delete`)时:先查该题全部记录 ID → 逐个删除磁盘目录 → 再删数据库记录(外键 `ON DELETE CASCADE` 同步删除文件行)。删除过程中任何一步失败都记日志,但**不阻断**题目删除(磁盘残留由后续裁剪或人工清理处理)。

## 6. 后端改动

### 6.1 接口

全部挂在既有 `admin` 路由组(自动继承 admin 鉴权、面板限流、审计与合规守卫)。

| 方法与路径 | 说明 |
| --- | --- |
| `GET /api/v1/admin/test-questions/:id/records?page=1&page_size=20` | 该题目的历史列表,倒序分页 |
| `GET /api/v1/admin/test-records/:id` | 单条记录详情,含 `response_text` 与文件元数据 |
| `GET /api/v1/admin/test-records/:id/files/:file_id` | 读取媒体文件,用于图片预览与下载 |

列表响应体(`response.Paginated`,即 `{items, total, page, page_size, pages}`),`items` 每项字段:

```
id, account_id, account_name, platform, model, reasoning_effort,
status, error_message, latency_ms, image_count,
response_truncated, created_at, has_text
```

`has_text` 表示 `response_text` 非空,前端据此决定是否显示「查看文本」。列表**不返回** `response_text` 与文件列表,避免大 payload;点开结果时再取详情。

详情响应在列表字段基础上追加:

```
response_text, files: [{ id, kind, mime_type, size_bytes }]
```

文件接口:按 `file_id` 查行 → 拼路径并做越界检查 → 读文件。响应头 `Content-Type` 为记录的 MIME;带 `?download=1` 时追加 `Content-Disposition: attachment; filename="<record_id>-<file_id>.<ext>"`。

### 6.2 现有接口的变更

`POST /api/v1/admin/accounts/:id/test` 的请求体(`account_handler.go:1265`)新增可选字段:

```go
TestQuestionID int64 `json:"test_question_id"`
```

- 为空或 0:不写历史,行为与今天一致(定时任务、旧客户端不受影响)。
- 非 0:先校验题目存在,不存在返回 `400`(`TEST_QUESTION_NOT_FOUND`),避免静默丢记录。

### 6.3 新增与修改的文件

| 层 | 文件 | 动作 |
| --- | --- | --- |
| 迁移 | `backend/migrations/247_account_test_records.sql` | 新增(建两张表 + 约束 + 索引) |
| 迁移测试 | `backend/migrations/account_test_records_migration_test.go` | 新增,照 `test_questions_migration_test.go` 断言 SQL 形状 |
| service | `backend/internal/service/account_test_record.go` | 新增:`TestRecord`、`TestRecordFile`、`TestRecordService`、接口定义、目录解析、裁剪 |
| service | `backend/internal/service/account_test_service.go` | 修改:入口建 recorder、`sendEvent` 加捕获、`defer` 收尾 |
| service | `backend/internal/service/test_question_service.go` | 修改:依赖 `TestRecordStore`,删除题目时清理该题记录文件 |
| service | `backend/internal/service/wire.go` | 修改:注册 `NewTestRecordService` |
| repository | `backend/internal/repository/account_test_record_repo.go` | 新增:raw SQL 仓储 |
| repository | `backend/internal/repository/wire.go` | 修改:注册构造函数 |
| handler | `backend/internal/handler/admin/test_record_handler.go` | 新增:列表、详情、文件三个 handler |
| handler | `backend/internal/handler/wire.go`、`handler.go` | 修改:挂载 handler |
| 路由 | `backend/internal/server/routes/admin.go` | 修改:在 `registerTestQuestionRoutes` 加 `:id/records`,新增 `registerTestRecordRoutes` |
| 依赖注入 | `backend/cmd/server/wire_gen.go` | 重新生成(`go generate ./cmd/server`) |

`AccountTestService` 不直接依赖具体仓储,通过 service 包内定义的 `TestRecordStore` 接口取依赖,与题库的 `TestQuestionRepository` 分层一致。

## 7. 前端改动

### 7.1 题库页面

`src/frontend/src/views/admin/TestQuestionsView.vue`:

- 操作列(`#cell-actions`,约 92-115 行)在「编辑」「删除」之前新增「测试历史」按钮,使用现有按钮样式与 `Icon`。
- 新增状态:`historyVisible`、`historyQuestion`、`historyPage`、`historyPageSize`、`historyRows`、`historyTotal`、`historyLoading`。
- `openHistoryDialog(question)` 打开弹框并加载第一页;翻页只重新请求列表。
- 新组件 `src/frontend/src/components/admin/testquestion/TestRecordHistoryModal.vue`:
  - `BaseDialog`(宽度 `large`)+ `DataTable` + 项目现有分页组件。
  - 列:账号(`account_name`,为空时显示 `-`)、模型(`model`)、思考强度(`reasoning_effort`,空时显示 `-`)、测试时长(`latency_ms` 格式化为 `x.xs` / `xxx ms`)、状态(成功/失败徽标,失败时 `title` 显示错误摘要)、结果按钮。
  - 结果按钮按下列规则渲染,互不排斥、可同时出现:
    - `has_text` 为真 → 「查看文本」按钮,点击打开文本查看层(复用账号测试弹框的终端样式 + 复制按钮 + 已截断提示)。
    - `image_count > 0` → 「查看图片」按钮,点击加载文件列表并以灯箱预览,多张可切换。
    - 文本可提取出代码块(复用 `buildArtifactFromText`)→ 「下载文件」按钮。
    - `status === 'failed'` 且有错误信息 → 「查看错误」按钮,点击展开错误文本。
  - 空列表显示 `EmptyState`;加载失败显示 `loadError` 文案。

### 7.2 共享工具抽取

把 `AccountTestModal.vue` 中的文件提取逻辑(`buildArtifactFromText` 及其依赖的 `artifactMimeTypes`、`normalizeArtifactLanguage`、`extractSvgRoot`、`isHtmlRootDocument`、`isSvgCandidateLanguage`、`fenceLanguage`、`clearArtifact`/`downloadArtifact` 中与 Blob 构造相关的部分)抽到 `src/frontend/src/utils/testArtifact.ts`,导出:

```ts
export interface TestArtifact { fileName: string; mimeType: string; content: string; isSvg: boolean }
export function buildTestArtifact(rawText: string, baseName: string): TestArtifact | null
export function downloadTestArtifact(artifact: TestArtifact): void
```

`AccountTestModal.vue` 改为引用该模块,行为保持不变(含 `artifactError` 与 `console.info` 日志)。历史弹框用同一函数,文件名基名取记录对应的题目标题,与账号测试弹框一致。

### 7.3 API 客户端

- `src/frontend/src/api/admin/testQuestions.ts` 新增 `records(questionId, page, pageSize)`。
- 新增 `src/frontend/src/api/admin/testRecords.ts`:`get(id)`、`fileURL(id, fileId, download)`;在 `api/admin/index.ts` 汇总导出。
- 图片预览用 `fetch` + `URL.createObjectURL`,组件卸载时 `revokeObjectURL`,与账号测试弹框的图片处理方式一致。

### 7.4 i18n

`src/frontend/src/i18n/locales/{zh,en}/admin/testQuestions.ts` 下新增 `history.*` 命名空间的 key,包括:按钮文案、弹框标题、六个列标题、状态徽标、四个结果按钮、加载/空/失败/截断/翻页文案。英文文件同步补齐。

## 8. 测试

### 8.1 后端

- `migrations/account_test_records_migration_test.go`:断言两张表、CHECK 约束、两个索引、外键动作存在。
- `account_test_record_test.go`(service 单测,`//go:build unit`):
  - recorder 对 `content` / `image` / `error` / `test_complete` 的累积结果正确。
  - 既无 `error` 也无 `test_complete` 时状态判为 `failed`。
  - `response_text` 超 64KB 时截断并置 `response_truncated`。
  - 写盘失败时不阻断入库、`image_count` 记 0。
  - 裁剪后保留条数正确、磁盘目录被删除。
  - 路径越界(`../`)的记录值被拒绝读取。
  - `test_question_id` 为空时不写库。
- `account_test_service` 侧回归:补一条测试验证 `sendEvent` 在挂了 recorder 时确实累积事件(用现有 `httptest.NewRecorder` + `gin.CreateTestContext` 范式)。
- `handler/admin/test_record_handler_test.go`:照 `test_question_handler_test.go` 的 fake 仓储模式,验证列表分页字段、详情字段、文件接口的 `Content-Disposition` 与 MIME。

### 8.2 前端

- `TestQuestionsView.spec.ts` 扩展:操作列渲染「测试历史」,点击后请求列表并渲染行。
- 新增 `TestRecordHistoryModal.spec.ts`:列渲染、状态徽标、四种结果按钮的出现条件、翻页请求参数、空态与错误态。
- `utils/__tests__/testArtifact.spec.ts`:抽取后的提取逻辑行为与抽取前一致(多代码块优先 SVG、HTML 文档不误判、回退 txt)。
- `api/admin/__tests__/testRecords.spec.ts`:请求路径与参数。

## 9. 校验命令

```bash
# 后端
cd src/backend && go test -tags=unit ./... && go build ./...
# 前端
cd src/frontend && pnpm run typecheck && pnpm run lint:check && pnpm run vitest run
```

迁移检查:重启后 `SELECT * FROM schema_migrations WHERE filename = '247_account_test_records.sql';` 应有一行记录。

## 10. 明确不做的事

- 不做历史记录的删除按钮、不做跨题目/跨账号的历史总览。
- 不把定时任务(`scheduled_testBackground`)的测试写入历史。
- 不做结果内容的检索、统计、成功率报表。
- 不为保留条数引入配置项。
- 不改 `RunTestBackground` 的 `httptest` 录制路径。
