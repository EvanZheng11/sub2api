# 账号测试题库设计

- 日期:2026-10-20
- 状态:已按当前实现校正(实现完成,待复审)
- 分支:`worktrees/wt_account-test-bank`
- 本文只描述**代码里已经存在的实现**,不描述计划或未实现的能力。

## 1. 背景与目标

账号管理里原有的「更多」菜单「测试」入口**保持不变**(不是本次新增的账号详情功能),本次只改它的弹窗内部:提示词从写死的常量改为可选题目。

本期实现:

1. 新增题库表与管理员 CRUD 接口,题目为全局资源。
2. 测试弹窗可选模型、思考强度档位、题目,发送所选题目**完整的 prompt**。
3. 结果仅在当前弹窗内渲染,不落库、无历史记录。

非目标(YAGNI,均未实现):后端搜索/分页、题目分组或标签、版本历史、结果持久化与统计、题目权限细分。

## 2. 数据模型

表 `test_questions`,与账号/渠道无外键关联。

| 字段 | 类型 | 说明 |
| --- | --- | --- |
| `id` | `BIGSERIAL PRIMARY KEY` | 题目 ID |
| `title` | `VARCHAR(200) NOT NULL` | 标题,列表展示与前端搜索用 |
| `prompt` | `TEXT NOT NULL` | 完整提示词,原样作为测试 prompt |
| `type` | `VARCHAR(16) NOT NULL` | 产物类型:`text` / `image` / `file` |
| `created_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | 创建时间 |
| `updated_at` | `TIMESTAMPTZ NOT NULL DEFAULT NOW()` | 更新时间,更新时由 SQL 显式写入 `NOW()` |

约束:

- `test_questions_type_check`:`CHECK (type IN ('text', 'image', 'file'))`。
- `test_questions_title_not_blank`:`CHECK (btrim(title) <> '')`。
- `test_questions_prompt_not_blank`:`CHECK (btrim(prompt) <> '')`。

索引:只有 `idx_test_questions_updated_at ON test_questions (updated_at DESC, id DESC)`,与列表排序 `ORDER BY updated_at DESC, id DESC` 对齐。**没有** `type` 索引,也没有 trigram/全文索引。

迁移:

- 唯一文件 `backend/migrations/246_test_questions.sql`(建表 + 三个约束 + 索引 + 默认题 seed)。
- 本项目通过 `backend/migrations/migrations.go` 的 `//go:embed *.sql` 把迁移目录下**所有 `.sql` 文件嵌入二进制**,启动时按文件名顺序**单向前进执行**并由 `schema_migrations` 记录校验和。
- 因此:`migrations/` 目录里**没有** `.down.sql`,`246_test_questions.down.sql` 不存在,也**不会**新增。回滚 SQL 一旦落成 `.sql` 文件就会被 embed 并参与启动执行,属于引入故障面。数据库结构变更只靠前向迁移表达,不提供自动回滚脚本。

## 3. 默认题目

迁移中条件插入一条默认题目:

- `title`:`鹈鹕骑自行车 SVG 动画`
- `type`:`file`
- `prompt`(逐字):
  > 不使用技能、不测试，一次性生成一段高质量的 SVG 代码，绘制一只正在骑自行车的鹈鹕的动画，画面美观且结构清晰。

插入语句带 `WHERE NOT EXISTS (SELECT 1 FROM test_questions)`,且迁移本身只执行一次(由 `schema_migrations` 保证),所以**不会**覆盖管理员改过的题目,**不会**复活被删除的默认题目。

## 4. 后端 API

前缀 `/api/v1/admin/test-questions`,挂在既有 admin 路由组(沿用现有 admin 鉴权中间件),注册位置 `backend/internal/server/routes/admin.go` 的 `registerTestQuestionRoutes`。

| 方法 | 路径 | 行为 |
| --- | --- | --- |
| GET | `/api/v1/admin/test-questions` | 返回**全部**题目数组,`ORDER BY updated_at DESC, id DESC`。**不接受** `search` / `type` / `page` / `page_size` 等查询参数,不做后端过滤与分页 |
| POST | `/api/v1/admin/test-questions` | 创建,请求体 `{title, prompt, type}` |
| PUT | `/api/v1/admin/test-questions/:id` | 全量更新,请求体同上 |
| DELETE | `/api/v1/admin/test-questions/:id` | 删除,成功返回 `{"message": "题目已删除"}` |

响应体统一用既有 `response.Success` / `response.BadRequest` / `response.ErrorFrom` 包装。`data` 中的 `Question`:

```
Question {
  id: number          // int64
  title: string
  prompt: string
  type: "text" | "image" | "file"
  created_at: string  // RFC3339
  updated_at: string  // RFC3339
}
```

校验(`backend/internal/service/test_question_service.go`):

| 规则 | 错误码 | HTTP |
| --- | --- | --- |
| `title` trim 后为空 | `TEST_QUESTION_TITLE_REQUIRED` | 400 |
| `prompt` trim 后为空 | `TEST_QUESTION_PROMPT_REQUIRED` | 400 |
| `type` 小写 trim 后不在白名单 | `TEST_QUESTION_INVALID_TYPE` | 400 |
| `title` 超过 200 个**字符**(rune) | `TEST_QUESTION_TITLE_TOO_LONG` | 400 |
| `prompt` 超过 20000 个**字符**(rune) | `TEST_QUESTION_PROMPT_TOO_LONG` | 400 |
| `:id` 非整数或 `<= 0` | handler 直接 `BadRequest` | 400 |
| 记录不存在 | `TEST_QUESTION_NOT_FOUND` | 404 |
| JSON 绑定失败 | handler 直接 `BadRequest("请求数据格式不正确")` | 400 |

实现位置:`backend/internal/handler/admin/test_question_handler.go`、`backend/internal/service/test_question_service.go`、`backend/internal/repository/test_question_repo.go`(raw SQL,不引入 ent 实体)。

## 5. 账号测试请求扩展

既有接口 `POST /api/v1/admin/accounts/:id/test` 的请求体新增一个**可选**字段:

```
reasoning_effort?: string
```

- **默认行为不变**:前端只在用户选到具体档位时才写这个字段;不传时后端完全不加推理参数,上游走默认行为。
- 后端在写入**任何 SSE 之前**调用 `resolveAccountTestReasoningEffort` 校验(`backend/internal/service/account_test_service_reasoning.go`)。不支持该参数的账号/平台/模型/模式,或档位不在允许集合内时,返回 `*service.AccountTestRequestError`,handler 转成 **HTTP 400** 中文错误,不会静默忽略,也不会伪装成上游失败。
- 各平台实际写入的字段:OpenAI 与 Grok 为 `reasoning.effort`;Gemini 为 `generationConfig.thinkingConfig.thinkingLevel`;Anthropic(非 Bedrock)为 `output_config.effort`。允许档位由各平台函数按模型计算(OpenAI 追加 `max` 视模型而定,GPT-6 Sol/Luna 追加 `none`)。
- 明确**不支持并返回 400** 的情形包括:按入站协议分流的国产供应商账号、Antigravity 的 `reasoning_effort`、按模型路由的聚合平台、`type=safe` 账号、Bedrock、Grok 非文本模式、compact 测试模式、图片生成模型等。Antigravity 测试本身已支持自定义题库 prompt(经 `routeAntigravityTest` 传给上游),仅不接受 `reasoning_effort`;compact 模式下携带自定义题目会在写入 SSE 前直接返回 400。
- **响应里没有「实际生效 effort」字段**:SSE 事件(`test_start` 等)不返回 effort,后端也不会回显。前端不展示所谓实际生效值;能不能用只看当前账号/模型算出的选项列表。
- 前端 `!response.ok` 时抛 `HTTP error! status: 400`,不解析后端错误体,因此界面上的提示是通用 HTTP 状态文本。

前端把所选题目的 `prompt` 原样写入请求 `prompt`,不做拼接或包装;`type` 只影响前端默认**测试模式**的选择(例如 `image` 题自动切到图片测试模式)。

## 6. 前端实现

真实文件(本次改动范围内):

| 文件 | 作用 |
| --- | --- |
| `frontend/src/api/admin/testQuestions.ts` | 题库 API 客户端:`list/create/update/remove`;`list()` **无参数**,返回全量数组 |
| `frontend/src/views/admin/TestQuestionsView.vue` | 题库管理页:表格 + 搜索框 + 类型下拉 + 新建/编辑弹窗 + 删除二次确认 |
| `frontend/src/components/admin/account/AccountTestModal.vue` | 账号测试弹窗(模型/思考强度/题目三个选择器 + 结果渲染 + 文件 artifact 预览) |
| `frontend/src/router/index.ts` | 新增路由 `/admin/test-questions`(`AdminTestQuestions`) |
| `frontend/src/components/layout/AppSidebar.vue` | 侧边栏入口,文案 key `nav.testQuestions` |
| `frontend/src/i18n/locales/zh/admin/testQuestions.ts`、`.../en/admin/testQuestions.ts` | 题库页文案,并在各自 `admin/index.ts` 汇总 |
| `frontend/src/i18n/locales/zh/common.ts`、`.../en/common.ts` | `nav.testQuestions` 等导航文案 |
| `frontend/src/i18n/locales/zh/admin/accounts.ts`、`.../en/admin/accounts.ts` | 弹窗内的题目/思考强度文案 |

题库页过滤**全部在前端完成**:`filteredQuestions` 对 `list()` 返回的全量数组做 `type` 精确匹配 + `title`/`prompt` 小写包含匹配,无分页组件、无服务端查询。

弹窗行为:

1. 打开时并行加载可用模型与题库(`adminAPI.testQuestions.list()`);题库加载失败只在弹窗内提示,不清空其它状态。
2. 思考强度选项由 `accountTestReasoningEffortOptions(account, modelId, mode)`(`frontend/src/components/admin/account/accountTestReasoningEffort.ts`)生成,首项为空值「默认」;平台无档位时该选择器不渲染。
3. 选中题目后按其 `type` 给出提示与模式切换;`image` 题在账号或模型不支持图片测试时提示不适用,`file` 题在图片测试账号上提示模型不匹配。
4. 结果只在当前弹窗渲染:文本可复制;文件类产物由弹窗内 `artifact` 状态承载——`artifactMimeTypes` 按扩展名给 MIME,抽取 ```` ```lang ```` 代码块(优先 SVG),SVG 用 `Blob` + `URL.createObjectURL` 交给 `<img>` 做动画预览,其余按 `<pre>` 文本展示,并提供下载;关闭/重开时 `URL.revokeObjectURL` 释放。
5. **没有**独立的 `TestResultView.vue` 组件,也不新增:预览与下载逻辑就在 `AccountTestModal.vue` 内。

## 7. 安全与边界

- SVG/HTML 一律按文本处理,预览走 `<img>` + Blob URL,不 `innerHTML` 注入。
- 下载文件名由题目标题派生,替换 `\ / : * ? " < > |` 与空白并按 80 字符截断。
- 日志只记录题目数量、id、title、type 等非提示词信息,不打印完整 prompt(服务层错误信息同样不含 prompt 内容)。
- 删除题目不影响历史(无历史数据),也不触发任何账号数据变更。

## 8. 与早期草案的差异(避免误读)

- 早期草案提到的 `246_test_questions.down.sql`、正反向迁移验证:**不存在,也不实施**。理由见 §2:迁移目录所有 `.sql` 都会被 embed 并在启动时执行。
- 早期草案提到的后端 `search`/`type` 过滤与分页接口、`type` 单列索引:**未实现**,`GET` 只返回全量列表。
- 早期草案提到的「`prompt` 非空」之外的长度限制实际为 20000 字符;`title` 为 200 字符。
- 早期草案提到的 `TestResultView.vue`、`AccountTestDialog.vue`、`src/locales/*` 路径:**与实际文件不符**,实际路径见 §6。
- 早期草案提到的「响应如实展示实际生效 effort」:**没有该字段**,见 §5。
