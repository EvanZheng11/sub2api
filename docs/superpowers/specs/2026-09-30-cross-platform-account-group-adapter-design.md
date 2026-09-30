# 跨平台账号-分组绑定与 OpenCode OpenAI 适配器设计

- 日期:2026-09-30
- 状态:设计稿(待评审)
- 基线:上游 sub2api v0.2.11;本 worktree 基点 `main@1f3b5a480`(v0.2.7 合并点,含本地增强)
- 分支:`worktrees/wt_cross-platform-binding`
- 范围:仅设计文档,不含实现

## 1. 背景与现状

### 1.1 平台与绑定现状

平台常量定义于 `backend/internal/domain/constants.go`:
`anthropic`、`openai`、`gemini`、`antigravity`、`grok`、`kimi`、`zhipu`、`deepseek`、`minimax`、`opencode_go`、`composite`。

账号与分组是多对多绑定(`account_groups` 关联表,服务层 `Account.AccountGroups`,
仓库层 `backend/internal/repository/account_repo.go` 的 `queryAccountsByGroup`)。
绑定关系本身不携带平台信息,跨平台约束分散在三处:

1. **创建/更新账号时的渠道风险检查**:`backend/internal/service/admin_account.go` 的
   `checkMixedChannelRisk` 对 anthropic/antigravity 之外的跨平台组合抛出 `MixedChannelError` 硬阻断。
2. **调度时的平台相等过滤**:`backend/internal/service/gateway_scheduling.go` 的
   `listSchedulableAccounts` 按分组平台调用
   `ListSchedulableByGroupIDAndPlatform` / `ListSchedulableByGroupIDAndPlatforms`,
   SQL 层以 `dbaccount.PlatformIn(...)` 与绑定条件同时过滤。
3. **候选资格判断**:`isAccountAllowedForPlatform` 只放行"账号平台 == 分组平台",
   以及"antigravity 账号且 `IsMixedSchedulingEnabled()`"(即现有混合调度)。

### 1.2 混合调度(mixed scheduling)现状

`selectAccountWithMixedScheduling`(gateway_scheduling.go)仅对
anthropic / gemini 分组生效(强制平台模式除外):在原生平台账号之外,
额外纳入启用了 `mixed_scheduling` 的 antigravity 账号。
粘性会话、failover、quota / 窗口费用 / RPM 检查、调度阈值过滤均与单平台路径共用。

### 1.3 OpenCode 平台现状

`opencode_go` 平台(OpenCode Zen 按量 / Go 订阅)已落地:

- 凭据:`credentials["api_key"]`、`account_mode`(zen|go)、`api_protocol`(默认 adaptive)、
  `base_url`、`api_base_urls`(按协议分端点)、`model_mapping`、`protocol_rules`
  (`backend/internal/service/opencode_go.go`)。
- 按模型分流:`DefaultOpenCodeGoProtocolRules` / `DefaultOpenCodeZenProtocolRules`
  将模型 ID 映射到 Responses / Chat Completions / Anthropic Messages 三种原生端点。
- 模型前缀归一:`normalizeOpenCodeGoModelID` 剥离 `opencode/`、`opencode-go/` 等前缀。
- OpenAI 入站(/v1/chat/completions、/v1/responses)已有直达转发
  (`OpenAIGatewayService.Forward`,见 `openai_gateway_opencode_mapping_test.go`)。

### 1.4 协议转换现状

`backend/internal/pkg/apicompat` 已具备成熟的桥接能力:
`chatcompletions_responses_bridge`、`chatcompletions_anthropic_bridge`、
`responses_to_anthropic` 等,覆盖请求、SSE 流式事件、工具调用与 reasoning 字段的双向转换。

## 2. 目标与非目标

### 2.1 目标

1. **全量绑定**:任意账号平台可绑定任意分组平台;绑定保存时不再做平台相等的硬校验。
2. **请求时能力检查**:绑定为"静态关系",能力为"运行时判定"。请求时检查
   入站协议与账号平台(及其 `api_protocol`)是否存在可用适配器;不存在则失败,
   返回明确错误并记录结构化日志。
3. **按绑定关系调度**:调度候选集由账号-分组绑定关系决定,不再按
   `账号平台 == 分组平台` 过滤;能力过滤后移到调度后的内存检查。
4. **新增 OpenAI 请求 × OpenCode 账号适配器**:统一收敛 OpenAI 入站协议
   (Chat Completions / Responses)到 opencode_go 账号的适配逻辑,覆盖:
   请求转换、流式与非流式响应、工具调用、模型映射、凭据与 base URL。
5. **保留既有 mixed scheduling**:现有 anthropic/gemini 分组纳入
   `mixed_scheduling` antigravity 账号的行为与开关语义完全保留。

### 2.2 非目标

- 不改变账号、分组的 CRUD 数据模型与 `account_groups` 表结构。
- 不改变计费、限额、sticky session、failover 的既有语义。
- 不为所有平台组合实现完整适配器(见 §6 不支持组合的行为)。
- 不实现 anthropic / gemini 入站到 opencode 账号的适配(仅 OpenAI 入站;
  其余组合由能力矩阵显式判定为不支持)。

## 3. 总体设计

### 3.1 绑定层:全平台可绑定

- `account_groups` 不变;绑定时不再校验"账号平台必须属于分组平台的允许集"。
- 移除 `checkMixedChannelRisk` 的硬阻断,降级为 `slog.Warn("binding_cross_platform", ...)`
  审计日志(保留观测点,便于回滚与问题定位;不引入配置开关,遵循简单优先)。
- 复核 `ValidateAccountGroupBindings`(admin_account.go):其中与 composite 分组
  相关的校验保留,平台相等类校验删除。
- 管理端保存绑定始终成功(只要分组存在且有效);能力提示由前端依据 §3.2
  的能力矩阵接口(可选只读端点)展示,不阻断保存。

### 3.2 请求时能力检查

新增服务层能力矩阵(单一权威来源,与 `IsMultiProtocolAPIKeyProvider` 等既有
判定函数同层放置,建议 `backend/internal/service/binding_capability.go`):

```go
// SupportsBinding 判断入站协议(由分组平台/端点决定)能否由账号平台承接。
// 判定顺序:精确平台组合 → 账号 api_protocol → 适配器注册表。
func SupportsBinding(inboundPlatform, accountPlatform, apiProtocol string) bool
```

- 矩阵初版:
  - 同平台组合:全部支持(与现状一致)。
  - `anthropic|gemini` 分组 × `antigravity` 账号:支持,且仍要求
    `IsMixedSchedulingEnabled()`(mixed scheduling 语义不变)。
  - OpenAI 入站(`openai` 分组平台,Chat Completions / Responses)×
    `opencode_go` 账号:支持(§4 适配器)。
  - 其余跨平台组合:不支持。
- 检查时机:
  1. **调度后置过滤**:候选账号在内存中过 `SupportsBinding`,不支持的记
     `slog.Debug("binding_capability_skip", ...)` 并跳过,不进入并发槽与转发。
  2. **转发前终检**(failover 循环内,账号 hydrate 之后):兜底防止
     snapshot 过期;失败时该账号按 failover 处理(计入 `FailedAccountIDs`)。
- 失败语义:当某次选号结果因能力不匹配被拒,或分组内所有候选均被过滤时,
  返回明确的 `ErrNoAvailableAccounts` 派生错误,消息形如
  `no available accounts: group platform "anthropic" has no adapter for account platform "opencode_go" (model: xxx)`,
  HTTP 层沿用现有 `classifySelectionFailureError` → 503;
  同时记录 `slog.Warn("binding_capability_unsupported", "group_id", ..., "group_platform", ...,
  "account_platform", ..., "inbound_protocol", ..., "model", ...)`。

### 3.3 调度按绑定关系

- 仓库层新增/改造查询(backend/internal/repository/account_repo.go):
  `ListSchedulableByGroupID(ctx, groupID)` —— 仅按 `account_groups` 绑定 +
  `status/schedulable/限流窗口` 等既有条件查询,**去掉 `PlatformIn` 过滤**;
  无分组 API key 的路径(`ListSchedulableUngroupedByPlatform`)保持平台过滤不变
  (无分组时没有绑定关系可言,维持原生 anthropic 行为)。
- `listSchedulableAccounts` 改为:按绑定取全量候选 → 内存执行
  `SupportsBinding` 能力过滤 → 交给既有的调度阈值 / quota / 窗口费用 / RPM 过滤链。
- `schedulerSnapshot` 的快照结构保留 platform 字段用于调试日志;
  快照查询同步改为绑定维度,避免快照与直查结果不一致。
- 粘性会话、failover、profit gate、并发槽逻辑不变;粘性命中同样补过
  `SupportsBinding`(与现有 `isAccountInGroup` 同位置的检查链合并)。
- 强制平台模式(`ctxkey.ForcePlatform`)保留:强制平台即入站平台,
  能力矩阵照常生效。

### 3.4 保留既有 mixed scheduling

- `selectAccountWithMixedScheduling` 及其入口条件
  (`platform == anthropic || gemini` 且非强制平台)保持不变。
- 在能力矩阵下,`antigravity → anthropic/gemini` 是矩阵中的显式支持项,
  `IsMixedSchedulingEnabled()` 继续作为账号级开关在候选过滤中执行。
- 回归要求:现有 mixed scheduling 相关测试
  (`account_pool_mode_test.go`、`gateway_scheduling` 相关用例)全部保持通过。

## 4. OpenAI 请求 × OpenCode 账号适配器

### 4.1 定位

将 OpenAI 入站(分组平台 `openai`,Chat Completions 与 Responses 两种端点)
到 `opencode_go` 账号的转发逻辑收敛为一个显式适配器,替代当前散落在
`OpenAIGatewayService` 内的直通分支,作为后续按能力矩阵逐组合补齐适配器的参考实现。

建议位置:`backend/internal/service/opencode_openai_adapter.go`
(服务层,复用 `apicompat` 桥接与既有 opencode 辅助函数)。

### 4.2 请求适配

- 端点选择:沿用 `api_protocol` + `protocol_rules` 的既有判定
  (adaptive 时按模型 ID 匹配 `DefaultOpenCodeGoProtocolRules` /
  `DefaultOpenCodeZenProtocolRules`;首个命中生效),得到目标原生协议:
  `chat_completions` / `responses` / `anthropic`。
- 协议转换:
  - 入站 Chat Completions → `chat_completions`:零转换直通(仅模型名改写)。
  - 入站 Chat Completions → `responses`:`chatcompletions_responses_bridge`。
  - 入站 Chat Completions → `anthropic`:`chatcompletions_anthropic_bridge`。
  - 入站 Responses → `responses`:零转换直通;→ 其他协议按既有桥接链
    (Responses 先归一到 Chat Completions 再转换,与现网行为一致)。
- 请求头:沿用 OpenCode 账号的既有头策略(`Authorization: Bearer <api_key>`),
  账号 header override(`account_header_override.go`)继续生效。

### 4.3 流式与非流式响应

- 非流式:上游 JSON 响应按入站协议逆转换回 OpenAI 格式;
  usage 统计沿用现有 usage service 记账路径。
- 流式:上游 SSE 逐事件转换;`chatcompletions_responses_bridge` 已覆盖
  流式生命周期(事件序列、finish_reason、reasoning 透传),适配器只负责:
  - 首包前不向客户端写出(沿用 streamStarted 语义,保证 failover 可切换);
  - 上游 5xx/断流时按现有 `UpstreamFailoverError` 语义处理;
  - OpenCode 特有的错误体(非 OpenAI 错误 JSON)归一为网关标准错误格式。

### 4.4 工具调用

- 请求侧:`tools` / `tool_choice` 随桥接层映射
  (Chat Completions function tool → Responses function tool / Anthropic tool_use)。
- 响应侧:`tool_calls` delta(流式)与 `tool_calls` 完整块(非流式)
  逆映射回 OpenAI 格式;`tool_call_id` 配对沿用桥接层的 item-id 辅助
  (`responses_client_tools_item_id_helper`)。
- 并行工具调用、tool 输出回传(下一轮 tool role 消息)由桥接层既有用例覆盖,
  适配器补充 OpenCode 上游的回归用例。

### 4.5 模型映射

解析顺序(与现网一致,收敛进适配器):

1. `normalizeOpenCodeGoModelID`:剥离 `opencode/`、`opencode-go/`、`opencode_go/` 前缀;
2. 账号级 `credentials["model_mapping"]`(精确映射);
3. 分组级调度映射对 `IsMultiProtocolAPIKeyProvider` 平台跳过
   (`opencode_go` 属于该集合,`ResolveMessagesDispatchModel` 返回空),
   即分组侧不改写,避免把 openai 专属默认值发给 OpenCode 上游。

### 4.6 凭据与 base URL

- 凭据来源:仅支持 `AccountTypeAPIKey` 类型;读取 `credentials["api_key"]`。
- base URL 选择:`credentials["api_base_urls"][protocol]` 优先,
  回退 `credentials["base_url"]`;预设按 `account_mode`:
  - `zen`:`https://opencode.ai/zen/v1`(chat_completions / responses)、
    `https://opencode.ai/zen`(anthropic);
  - `go`:`https://opencode.ai/zen/go/v1`。
- 凭据缺失 / base URL 缺失属于配置错误:转发前检查,返回
  明确错误(不静默回退默认域名),并记 `slog.Warn`。
- 敏感信息脱敏沿用 `account_credentials_redact.go`,日志不得输出 api_key。

## 5. 与入站处理器的集成

- `/v1/chat/completions`(`gateway_handler_chat_completions.go`):
  现有 `groupPlatform == gemini && account.Platform != gemini` 的硬过滤
  由 `SupportsBinding` 取代;转发分派新增 opencode 适配器分支
  (优先级:gemini compat → antigravity compat → opencode adapter → 通用转发)。
- `/v1/responses`、`/v1/messages`、gemini 端点:本设计不改其分派逻辑,
  仅受益于调度层能力过滤(不支持的组合在选号阶段即被排除)。

## 6. 不支持组合的行为

- 管理端:绑定保存成功(§3.1),不做能力阻断。
- 调度:能力矩阵不支持的组合在候选过滤阶段被排除,日志级别 Debug;
  若分组因此无可用账号,选号失败错误信息明确指出
  "分组平台 × 账号平台 无适配器"(而非笼统的 no available accounts)。
- 请求:转发前终检不通过时,按 failover 切换下一账号;
  全部耗尽后返回 503 + 明确原因文案,并记录 `binding_capability_unsupported` Warn 日志。
- 不存在任何"平台不等但猜测转发"的静默回退;每个被拒绝的组合都有日志可查。

## 7. 测试计划

沿用仓库 `//go:build unit` 单测与 httptest 上游桩的既有约定。

### 7.1 单元测试

- 能力矩阵:`SupportsBinding` 全组合表驱动用例(含 antigravity 开关、
  opencode api_protocol 变体)。
- 绑定:跨平台绑定保存成功;`checkMixedChannelRisk` 移除后仅记日志。
- 调度:`ListSchedulableByGroupID` 返回绑定全平台候选;
  能力过滤跳过不支持组合;mixed scheduling 回归用例全绿。
- OpenCode 适配器:
  - 请求:三种目标协议的端点 URL、请求体、模型名改写
    (参照 `openai_gateway_opencode_mapping_test.go` 扩展);
  - 非流式响应:JSON 逆转换、usage 记账;
  - 流式响应:SSE 事件序列、首包前 failover;
  - 工具调用:tool_calls 双向映射、流式 delta 聚合、并行工具调用;
  - 模型映射:前缀剥离 + model_mapping 精确映射优先级;
  - 凭据与 base URL:api_base_urls 按协议选择、缺失时报错与日志。

### 7.2 集成测试

- openai 分组绑定 opencode 账号,Chat Completions 非流式 + 流式端到端(桩上游)。
- anthropic 分组绑定 opencode 账号(不支持组合):选号失败、错误文案、Warn 日志断言。
- anthropic 分组绑定 antigravity 账号(mixed scheduling 开/关)行为不变。
- 无分组 API key 行为与现状一致(仅原生平台未分组账号)。

## 8. 风险

| 风险 | 影响 | 缓解 |
| --- | --- | --- |
| 候选集扩大:按绑定查询不再有 SQL 层平台裁剪,大分组候选数上升 | 调度延迟、内存占用 | 能力过滤放在 snapshot 之后内存执行;`schedulerSnapshot` 沿用;必要时快照内预存能力位 |
| 行为面扩大:原先被平台过滤"隐式屏蔽"的组合现在可达 | 意外启用未适配路径 | 能力矩阵白名单制:未注册组合一律不支持;转发前终检兜底 |
| mixed scheduling 回归 | 现网 anthropic/gemini 分组调度行为变化 | 入口条件与开关语义零改动;专项回归用例 |
| 移除 `checkMixedChannelRisk` 后失去创建期拦截 | 管理员误配置无感知 | 保留跨平台绑定 Warn 日志;管理端可选能力提示 |
| composite 分组与能力矩阵交互(composite 自带路由决策) | 双重过滤冲突 | composite 路径维持现状,`SupportsBinding` 仅对最终 TargetPlatform 生效 |
| 回滚兼容:新版本产生的跨平台绑定,回滚到旧版本后 | 旧代码按平台过滤,不会选中跨平台账号,不产生错误流量;但管理端再次编辑账号会触发旧校验 | 发布说明标注:回滚前无需清理绑定,旧代码行为安全 |
| 上游 v0.2.11 同步冲突 | 调度文件为本地上游高频改动区 | 实施拆小提交;`gateway_scheduling.go` 改动集中且最小化 |

## 9. 实施拆分建议

1. 能力矩阵 + 绑定层放宽(移除硬校验,保留日志)。
2. 调度层改造(绑定维度查询 + 内存能力过滤)。
3. OpenCode OpenAI 适配器(请求/响应/流式/工具/映射/凭据)。
4. 文档与监控:错误码、日志指标(dashboard 计数 `binding_capability_unsupported`)。

每步独立可回滚;步骤 2 与 3 无依赖,可并行。
