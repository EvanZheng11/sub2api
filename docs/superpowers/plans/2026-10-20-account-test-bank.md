# 账号测试题库实施计划

> **状态:任务 A / B / C 已按当前实现落地;wire 装配修正已完成;验证与提交待复审(见文末)。**
> 本文已按实际代码校正,只保留与仓库现状一致的内容。

**Goal:** 管理员维护测试题库(CRUD),账号「测试」弹窗可选模型、思考强度档位与题目后发起测试,结果仅存在于当前弹窗(文本复制、文件 artifact 预览与下载、SVG 动画预览)。

**Architecture:** 三块改动相邻但不重叠:A 后端题库(新表 + 前向迁移 + 仓储/服务/handler + 路由与 wire 装配);B 账号测试请求新增可选 `reasoning_effort` 并把所选题目 prompt 透传;C 前端(题库管理页 + 弹窗三选择器 + artifact 预览)。A 的装配面(`handler/wire.go`、`cmd/server/wire_gen.go`、`routes/admin.go`)与 B 的请求字段最终在同一装配链上收口。

**Tech Stack:** Go(gin、`database/sql`、`log/slog`、testify,`//go:build unit` 与 `-tags integration` + testcontainers)、PostgreSQL 迁移脚本、Vue 3 + TypeScript + Pinia + vitest、git(worktree 分支)。

**Spec:** `docs/superpowers/specs/2026-10-20-account-test-bank-design.md`(以该文为准,本文只记录任务与完成状态)。

## 全局约束

- 工作目录:worktree `/home/andy/app/sub2api-deploy/src/.worktrees/wt_account-test-bank`。后端命令在 `backend/` 下执行,前端命令在 `frontend/` 下执行。
- 不改 `account_handler.go` 既有字段语义,只新增可选字段 `reasoning_effort`。
- 页面文案、代码注释、日志均为中文;日志不打印完整 prompt。
- 不读写 `.DS_Store`。
- 迁移目录**只放前向迁移**:`backend/migrations/*.sql` 全部由 `//go:embed` 嵌入并在启动时执行,因此**不创建任何 `.down.sql`**,也不做「删除表/删数据」的回滚实现。

## 文件结构(实际落地)

```
backend/
  migrations/
    246_test_questions.sql                 [新增] 建表 + 约束 + idx_test_questions_updated_at + 默认题 seed
    test_questions_migration_test.go       [新增] 迁移文本形状单测(不连库)
  internal/repository/
    test_question_repo.go                  [新增] List(全量, updated_at desc, id desc) / GetByID / Create / Update / Delete(raw SQL)
    test_question_repo_integration_test.go [新增] testcontainers 集成测试(CRUD + seed 迁移)
    wire.go                                [修改] 增加 NewTestQuestionRepository
  internal/service/
    test_question_service.go               [新增] 校验(trim/长度/类型白名单)+ DTO + 仓储接口
    account_test_service.go                [修改] AccountTestOptions 增加 ReasoningEffort;prompt 真实透传;effort 校验前置
    account_test_service_reasoning.go      [新增] 各平台 effort 支持面与 payload 写入
    account_test_service_reasoning_test.go [新增] payload 与「未知档位在 SSE 前 400」的单测
    wire.go                                [修改] 增加 NewTestQuestionService
  internal/handler/
    admin/test_question_handler.go         [新增] GET/POST/PUT/DELETE /api/v1/admin/test-questions
    admin/test_question_handler_test.go    [新增] CRUD 生命周期、非法入参、排序单测
    admin/account_handler.go               [修改] TestAccountRequest 增加 reasoning_effort,并把 AccountTestRequestError 转 400
    wire.go                                [修改] ProvideAdminHandlers 增加 TestQuestionHandler 参数;ProviderSet 补 admin.NewTestQuestionHandler
  cmd/server/wire_gen.go                   [修改] 由 wire 重新生成
  internal/server/routes/admin.go          [修改] registerTestQuestionRoutes
frontend/
  src/api/admin/testQuestions.ts           [新增] list/create/update/remove
  src/views/admin/TestQuestionsView.vue    [新增] 题库页(表格/搜索/类型筛选/CRUD 弹窗/删除确认)
  src/views/admin/__tests__/TestQuestionsView.spec.ts [新增] 页面单测
  src/components/admin/account/AccountTestModal.vue   [修改] 模型/强度/题目三选择器 + artifact 预览与下载
  src/components/admin/account/__tests__/AccountTestModal.questions.spec.ts [新增] 弹窗题库单测
  src/router/index.ts                      [修改] /admin/test-questions 路由
  src/components/layout/AppSidebar.vue     [修改] 侧边栏入口
  src/i18n/locales/{zh,en}/admin/testQuestions.ts     [新增]
  src/i18n/locales/{zh,en}/admin/accounts.ts          [修改] 弹窗题目/强度文案
  src/i18n/locales/{zh,en}/admin/index.ts             [修改] 汇总 testQuestions
  src/i18n/locales/{zh,en}/common.ts                  [修改] nav.testQuestions
```

注意:不存在 `test_question.go`、`test_question_test.go`、`246_test_questions.down.sql`、`AccountTestDialog.vue`、`TestResultView.vue`;服务层校验由 handler 单测与仓储集成测试覆盖。

## 任务依赖与装配

```
A(后端题库) ─┐
             ├─> 集成(wire 装配收口 + 路由核对 + 联调)
B(账号测试) ─┘
A/C 契约 ──> C(前端)
```

- A 与 B 改动文件不重叠,**但都在同一套 wire 装配上收口**:A 的仓储/服务/handler 需要出现在 `internal/repository/wire.go`、`internal/service/wire.go`、`internal/handler/wire.go` 的 ProviderSet 中;B 只在 `account_handler.go` / `account_test_service*.go` 内改请求字段。
- 本次复审发现的手写装配缺口:A 的 `provideAdminHandlers` 新增了 `*admin.TestQuestionHandler` 参数,但 `backend/internal/handler/wire.go` 的 `ProviderSet` **漏了** `admin.NewTestQuestionHandler`,导致手写 `wire_gen.go` 能编译、`wire` 重新生成却失败。修正方式:在 ProviderSet 补该 provider,并用本地缓存的 wire 模块重新生成 `cmd/server/wire_gen.go`(只影响生成文件,不改 `go.mod` / `go.sum`)。

## 任务 A:后端题库(已实现)

- [x] A1 `migrations/246_test_questions.sql`:建 `test_questions`(`id`/`title`/`prompt`/`type`/`created_at`/`updated_at`)、`CHECK (type IN ('text','image','file'))`、title/prompt 非空约束、`idx_test_questions_updated_at (updated_at DESC, id DESC)`、默认题条件 seed(`WHERE NOT EXISTS`)。**未创建 `.down.sql`**。
- [x] A2 仓储 `internal/repository/test_question_repo.go`:`List` 返回**全量**并按 `updated_at DESC, id DESC` 排序;`GetByID`/`Create`/`Update`/`Delete` 全部参数化 SQL,无字符串拼接。
- [x] A3 服务 `internal/service/test_question_service.go`:title 非空且 ≤200 字符、prompt 非空且 ≤20000 字符、type 白名单;错误信息中文且不含 prompt 内容;输出 `TestQuestion` DTO。
- [x] A4 handler `admin/test_question_handler.go`:四条路由;id 非法 400、不存在 404;`List` 空结果返回 `[]` 而不是 `null`。
- [x] A5 路由与依赖装配:`registerTestQuestionRoutes` + repository/service/handler 三个 ProviderSet + `ProvideAdminHandlers` 参数与 `AdminHandlers.TestQuestion` 字段 + `wire_gen.go` 重新生成。
- [x] A6 测试:`test_question_handler_test.go`(unit)、`test_questions_migration_test.go`(迁移文本形状)、`test_question_repo_integration_test.go`(integration,含 seed 迁移)。

## 任务 B:后端账号测试扩展(已实现)

- [x] B1 `TestAccountRequest` 新增可选 `reasoning_effort`,其余字段语义不变;`AccountTestRequestError` 转 HTTP 400。
- [x] B2 `account_test_service.go` + `account_test_service_reasoning.go`:prompt 原样进入各平台测试 payload;effort 在写入任何 SSE 前校验,不支持/未知档位 400;不支持的平台不写任何推理字段。
- [x] B3 测试:`account_test_service_reasoning_test.go` 覆盖 Claude/OpenAI Responses/OpenAI Chat/Gemini/Grok payload 与「未知 effort 在 SSE 前被拒」等路径。

## 任务 C:前端(已实现)

- [x] C1 `api/admin/testQuestions.ts`:`list()` 无参返回全量,另有 `create/update/remove`;类型与后端 `Question` 对齐。
- [x] C2 `views/admin/TestQuestionsView.vue`:表格 + 搜索框 + 类型下拉 + 新建/编辑弹窗 + 删除二次确认;搜索与类型筛选**纯前端**对全量数组过滤,无分页。
- [x] C3 `components/admin/account/AccountTestModal.vue`:模型、思考强度(平台无档位则不渲染)、题目三个选择器;选中题目发送完整 prompt;无历史记录,关闭即清空。
- [x] C4 同一文件内的 artifact 预览:代码块抽取(优先 SVG)、扩展名到 MIME 映射、`Blob` + `objectUrl` 交 `<img>` 做 SVG 动画预览、`<pre>` 文本展示、下载与 `revokeObjectURL` 释放。未新增组件。
- [x] C5 i18n 与单测:中英文案补齐,新增 `TestQuestionsView.spec.ts` 与 `AccountTestModal.questions.spec.ts`。

## 待复审验证(尚未在本轮执行/未提交)

以下为**待执行**的验收项,本文不声称已通过:

```
# backend/
go test ./internal/handler/admin -run TestTestQuestion
go test ./migrations -run TestTestQuestions
TESTCONTAINERS_RYUK_DISABLED=true go test -tags integration ./internal/repository -run TestTestQuestion
go test -tags unit ./internal/service -run 'TestAccountTest|TestResolveAccountTestReasoningEffort|TestCreate.*ReasoningEffort'
go build ./...

# frontend/
pnpm run typecheck
pnpm run check:i18n
pnpm run test:run
pnpm run build
```

本轮已完成的装配层验证(事实记录):

- `backend/`: `go build ./cmd/server ./...` 通过(wire_gen.go 为 wire 重新生成版本)。
- `wire` 生成:用本机模块缓存里的 `github.com/google/wire v0.7.0` 本地驱动 `./cmd/server` 重新生成成功;补 provider 前该生成会报「缺少 `*admin.TestQuestionHandler` provider」。生成过程未修改 `go.mod` / `go.sum`(网络不可用,故此方式代替 `go run github.com/google/wire/cmd/wire`)。
- 回归与提交(codegraph 索引更新、conventional commit、合并集成分支)仍未进行,需人工复审后执行。
