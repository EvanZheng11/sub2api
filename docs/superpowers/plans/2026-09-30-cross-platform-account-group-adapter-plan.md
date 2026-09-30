# 跨平台账号-分组绑定与 OpenCode OpenAI 适配器实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 让任意平台的账号可绑定任意平台的分组（绑定层放宽），调度按 `account_groups` 绑定关系 + 运行时能力矩阵执行，并把 OpenAI 入站（Chat Completions / Responses）到 `opencode_go` 账号的适配收敛为显式适配器。

**Architecture:** 绑定为静态关系、能力为运行时判定。三层改动：(1) 管理端移除跨平台绑定硬校验、降级为审计日志；(2) 调度候选集改为按 `account_groups` 绑定维度查询（复用既有 `ListSchedulableByGroupID`），在内存按 `SupportsBinding` 能力矩阵过滤，选号失败返回明确"无适配器"错误；(3) OpenCode 适配器把现有散落在 `openai_gateway_forward.go` / `openai_gateway_chat_completions.go` 的 opencode 分流逻辑收敛到 `opencode_openai_adapter.go`，并补齐凭据/base URL 转发前预检。前端 `GroupSelector` 放开平台过滤，允许跨平台勾选分组。

**Tech Stack:** Go 1.x（ent ORM、gin、`log/slog`、`//go:build unit` 单测 + testify）、`backend/internal/pkg/apicompat` 协议桥接、Vue 3 + TypeScript + vitest（前端）、git（merge 上游 tag）。

**Spec:** `docs/superpowers/specs/2026-09-30-cross-platform-account-group-adapter-design.md`（实施时与本文一起阅读，本文按该设计 §3/§4/§5/§7 逐条落实）

## 全局约束

- 工作目录：worktree `/home/andy/app/sub2api-deploy/src/.worktrees/wt_cross-platform-binding`，分支 `worktrees/wt_cross-platform-binding`。所有命令在该目录执行（后端命令在 `backend/` 下执行）。
- 基线：Task 1 先合并上游 v0.2.11；其后所有任务基于合并后的树。行号若因合并漂移，用文中给出的 `grep` 命令重新定位符号。
- 后端单测约定：文件头 `//go:build unit`，包名 `service` / `handler`，运行 `go test -tags=unit ./internal/<pkg>/ -run '<TestName>'`。
- 前端测试：`pnpm --dir frontend exec vitest run <file>`；lint/typecheck：`pnpm --dir frontend run lint:check && pnpm --dir frontend run typecheck`。
- 不修改：`account_groups` 表结构、计费/限额/sticky session/failover 语义、`selectAccountWithMixedScheduling` 的入口条件与 mixed scheduling 开关语义（`IsMixedSchedulingEnabled`）。
- 能力矩阵白名单制：未在矩阵中列出的跨平台组合一律不支持；不存在"平台不等但猜测转发"的静默回退。
- 适配范围：仅 OpenAI 入站（Chat Completions / Responses）× `opencode_go` 账号；anthropic/gemini 入站到 opencode 账号不做适配（矩阵判不支持）。
- composite 分组路由决策在前，`SupportsBinding` 只对最终 TargetPlatform 生效；不改动 composite 路径。
- 日志不得输出 `api_key` 等敏感凭据（沿用 `backend/internal/service/account_credentials_redact.go` 的脱敏约定）；所有新增日志使用中文注释 + 结构化 slog 字段。
- 每个任务一次提交，conventional commits（`feat:`/`test:`/`refactor:`/`chore:`）；提交前 `git status --short` 确认只含本任务文件；提交前更新 codegraph 索引（本机已装 codegraph 1.6.0：`codegraph --help` 查看子命令后执行对应的索引/更新命令；若失败不阻塞提交，记录原因即可）。
- 不读写 `.DS_Store`。
- 前端能力矩阵只读端点（设计 §3.1 "可选"）：本期不实现（YAGNI）；跨平台风险提示沿用既有 `check-mixed-channel` 确认弹窗（只提示不阻断保存）。

## 文件结构

```
backend/internal/service/
  binding_capability.go            [创建] SupportsBinding 能力矩阵 + 绑定能力过滤辅助函数（单一权威来源）
  binding_capability_test.go       [创建] 能力矩阵表驱动单测
  admin_account.go                 [修改] checkMixedChannelRisk 三个保存路径调用点降级为审计日志
  admin_account_binding_test.go    [创建] 绑定降级日志单测
  gateway_scheduling.go            [修改] listSchedulableAccounts 绑定维度查询；isAccountAllowedForPlatform 能力化；选号失败错误文案
  scheduler_snapshot_service.go    [修改] loadAccountsFromDB 分组路径改为绑定维度查询
  gateway_scheduling_binding_test.go [创建] 绑定维度调度单测（含明确错误文案断言）
  opencode_openai_adapter.go       [创建] OpenAI 入站 × opencode_go 账号显式适配器 + 凭据/base URL 预检
  opencode_openai_adapter_test.go  [创建] 适配器请求/凭据单测 + 流式/工具回归
  openai_gateway_forward.go        [修改] /v1/responses 入站 opencode 分支委托适配器
  openai_gateway_chat_completions.go [修改] /v1/chat/completions 入站 opencode 分支委托适配器
backend/internal/handler/
  gateway_handler_chat_completions.go [修改] 转发前终检：gemini 硬过滤 → SupportsBinding
frontend/src/components/common/
  GroupSelector.vue                [修改] 移除分组平台匹配过滤（保留 simple 模式 composite 规则与搜索）
  __tests__/GroupSelector.spec.ts  [修改] 新增跨平台可选断言、删除平台过滤旧断言
frontend/src/components/account/
  CreateAccountModal.vue           [修改] GroupSelector 不再传 :platform / :mixed-scheduling
  EditAccountModal.vue             [修改] 同上
  BulkEditAccountModal.vue         [修改] 同上
```

不改动的关键既有文件（回归保障）：`backend/internal/service/opencode_go.go`（协议规则/模型归一）、`backend/internal/pkg/apicompat/*`（桥接）、`backend/internal/repository/account_repo.go`（`ListSchedulableByGroupID` 已存在，`account_service.go:87` 已在接口上）、`backend/internal/handler/admin/account_handler.go`（check-mixed-channel 只读端点保留）。

---

### Task 1: 获取并合并上游 v0.2.11，解决冲突

**Files:**
- Modify: 整个仓库树（merge 结果）；预期冲突集中在上游高频改动区与本地增强交叠的文件（`backend/internal/service/gateway_scheduling.go`、`admin_account.go`、`openai_gateway_*.go`、`frontend/src/**`）
- 不创建新文件

**Interfaces:**
- Consumes: 远程 `origin` = `https://github.com/Wei-Shaw/sub2api.git`，tag `v0.2.11`
- Produces: 合并提交（两个父提交：本地 `cb3aa2637` 与上游 `v0.2.11`），后续任务在该基线上进行

- [ ] **Step 1: 确认工作区干净并记录基点**

```bash
git status --short   # 预期输出为空
git log --oneline -3 # 预期: cb3aa2637 docs: 跨平台账号-分组绑定与 OpenCode OpenAI 适配器设计稿
git rev-parse HEAD > /tmp/plan-task1-base-sha.txt
```

- [ ] **Step 2: 获取上游 v0.2.11 tag**

```bash
git fetch origin tag v0.2.11
git rev-parse v0.2.11^{commit}   # 预期: 输出上游 v0.2.11 的 commit SHA（非空即成功）
git show v0.2.11:backend/cmd/server/VERSION   # 预期输出: 0.2.11
```

若网络超时（github 访问不稳定），改用 `git fetch https://github.com/Wei-Shaw/sub2api.git tag v0.2.11` 重试一次；仍失败则停下来报告，不要用其它 tag 冒充。

- [ ] **Step 3: 执行合并**

```bash
git merge v0.2.11 --no-edit -m "chore: merge sub2api v0.2.11"
```

- [ ] **Step 4: 解决冲突（若有）**

原则：
1. 纯上游区域（本分支未改过的文件）直接取上游版本：`git checkout --theirs <file>`。
2. 本地增强区域：保留本地增强，同时合入上游新增逻辑。重点人工核对以下文件（本地有增强且上游大概率也改了）：
   - `backend/internal/service/gateway_scheduling.go`（本地调度阈值/profit gate 改动）
   - `backend/internal/service/admin_account.go`（本地绑定校验改动）
   - `backend/internal/service/openai_gateway_forward.go`、`openai_gateway_chat_completions.go`（本地 opencode 分流分支——`if account.IsOpenCodeGo()` 两段必须完整保留）
   - `backend/internal/service/opencode_go.go`（本地新增文件，理论上无冲突）
   - `frontend/src/components/common/GroupSelector.vue`、`frontend/src/components/account/*.vue`
3. `backend/cmd/server/VERSION` 取上游 `0.2.11`。
4. 每解决一个文件：`git add <file>`。

```bash
git diff --name-only --diff-filter=U   # 预期: 空（全部已解决）
```

- [ ] **Step 5: 合并后编译与全量单测**

```bash
cd backend && go build ./... && go vet ./internal/... 
go test -tags=unit ./internal/service/ ./internal/handler/...   # 预期: 全部 PASS（若有失败，逐个判断是上游行为变更还是合并失误；不得为过测试删除本地增强）
cd .. && pnpm --dir frontend install && pnpm --dir frontend run typecheck   # 预期: 无类型错误
```

注意：若上游 v0.2.11 改了 opencode/调度相关行为导致本设计前提变化（例如 `openai_gateway_forward.go` 的 `IsOpenCodeGo` 分支被上游重构），停下来在设计文档上追加变更记录并告知用户，不要自行改设计。

- [ ] **Step 6: 提交合并（merge 已自动生成提交；仅当无冲突产生 merge commit 时确认即可）**

```bash
git log -1 --format='%H %P'   # 预期: 显示两个父提交
git status --short            # 预期: 空
```

---

### Task 2: 能力矩阵 `SupportsBinding`

**Files:**
- Create: `backend/internal/service/binding_capability.go`
- Test: `backend/internal/service/binding_capability_test.go`

**Interfaces:**
- Consumes: `backend/internal/service/domain_constants.go` 的平台常量（`PlatformAnthropic`/`PlatformOpenAI`/`PlatformGemini`/`PlatformAntigravity`/`PlatformOpenCodeGo`/`PlatformKimi` 等，`domain/constants.go:21-34` 的镜像）
- Produces: `func SupportsBinding(inboundPlatform, accountPlatform, apiProtocol string) bool`（后续 Task 4/5/6/9 消费）；`func bindingCapabilityAllows(platform string, acc *Account) bool`（含 antigravity mixed 开关判定，Task 4 消费）

- [ ] **Step 1: 写失败测试**

创建 `backend/internal/service/binding_capability_test.go`：

```go
//go:build unit

package service

import "testing"

// TestSupportsBindingMatrix 能力矩阵全组合表驱动用例（设计 §3.2）。
func TestSupportsBindingMatrix(t *testing.T) {
	cases := []struct {
		name            string
		inboundPlatform string
		accountPlatform string
		apiProtocol     string
		want            bool
	}{
		{"同平台 anthropic", PlatformAnthropic, PlatformAnthropic, "", true},
		{"同平台 openai", PlatformOpenAI, PlatformOpenAI, "", true},
		{"同平台 opencode", PlatformOpenCodeGo, PlatformOpenCodeGo, APIProtocolAdaptive, true},
		{"同平台 kimi", PlatformKimi, PlatformKimi, "", true},
		{"antigravity 到 anthropic（mixed scheduling）", PlatformAnthropic, PlatformAntigravity, "", true},
		{"antigravity 到 gemini（mixed scheduling）", PlatformGemini, PlatformAntigravity, "", true},
		{"openai 到 opencode（本设计新增适配器）", PlatformOpenAI, PlatformOpenCodeGo, APIProtocolAdaptive, true},
		{"anthropic 到 opencode 不支持", PlatformAnthropic, PlatformOpenCodeGo, APIProtocolAdaptive, false},
		{"gemini 到 opencode 不支持", PlatformGemini, PlatformOpenCodeGo, APIProtocolAdaptive, false},
		{"openai 到 anthropic 不支持", PlatformOpenAI, PlatformAnthropic, "", false},
		{"anthropic 到 openai 不支持", PlatformAnthropic, PlatformOpenAI, "", false},
		{"openai 到 kimi 不支持", PlatformOpenAI, PlatformKimi, "", false},
		{"antigravity 到 openai 不支持", PlatformOpenAI, PlatformAntigravity, "", false},
		{"空入站平台", "", PlatformOpenAI, "", false},
		{"空账号平台", PlatformOpenAI, "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := SupportsBinding(tc.inboundPlatform, tc.accountPlatform, tc.apiProtocol)
			if got != tc.want {
				t.Fatalf("SupportsBinding(%q, %q, %q) = %v, want %v",
					tc.inboundPlatform, tc.accountPlatform, tc.apiProtocol, got, tc.want)
			}
		})
	}
}

// TestBindingCapabilityAllowsAntigravitySwitch antigravity 账号承接
// anthropic/gemini 分组时必须打开 IsMixedSchedulingEnabled（设计 §3.4）。
func TestBindingCapabilityAllowsAntigravitySwitch(t *testing.T) {
	mixedOn := &Account{Platform: PlatformAntigravity, mixedSchedulingEnabled: true}
	mixedOff := &Account{Platform: PlatformAntigravity, mixedSchedulingEnabled: false}
	native := &Account{Platform: PlatformAnthropic}

	if !bindingCapabilityAllows(PlatformAnthropic, mixedOn) {
		t.Fatal("mixed_scheduling 开启的 antigravity 账号应可承接 anthropic 分组")
	}
	if bindingCapabilityAllows(PlatformAnthropic, mixedOff) {
		t.Fatal("mixed_scheduling 关闭的 antigravity 账号不应承接 anthropic 分组")
	}
	if !bindingCapabilityAllows(PlatformAntigravity, native) {
		t.Fatal("antigravity 分组应可承接 antigravity 账号")
	}
	if bindingCapabilityAllows(PlatformAnthropic, nil) {
		t.Fatal("nil 账号不可承接任何分组")
	}
}
```

注：`Account.mixedSchedulingEnabled` 的实际字段/构造方式以 `Account.IsMixedSchedulingEnabled()`（`backend/internal/service/account.go`）实现为准；若字段名不同（例如存在 `Extra` map 中），改为通过 `IsMixedSchedulingEnabled()` 的真实数据路径构造账号，断言不变。

- [ ] **Step 2: 运行确认失败**

```bash
cd backend && go test -tags=unit ./internal/service/ -run 'TestSupportsBindingMatrix|TestBindingCapabilityAllowsAntigravitySwitch'
```

预期: 编译失败，`undefined: SupportsBinding` / `undefined: bindingCapabilityAllows`。

- [ ] **Step 3: 最小实现**

创建 `backend/internal/service/binding_capability.go`：

```go
package service

// binding_capability.go 是"分组平台 × 账号平台"绑定能力的单一权威来源（设计 §3.2）。
// 绑定为静态关系，能力为运行时判定；矩阵白名单制——未显式列出的跨平台组合一律不支持。

import "log/slog"

// SupportsBinding 判断入站协议（由分组平台/强制平台决定）能否由账号平台承接。
// 判定顺序：精确平台组合 → 账号 api_protocol 相关组合 → 白名单注册表。
// composite 入站不进入本函数：composite 路由先解析出最终 TargetPlatform
// （设计 §8），本函数只对最终平台生效。
func SupportsBinding(inboundPlatform, accountPlatform, apiProtocol string) bool {
	if inboundPlatform == "" || accountPlatform == "" {
		return false
	}
	if inboundPlatform == accountPlatform {
		// 同平台组合全部支持（apiProtocol 由各平台转发链自行处理）。
		return true
	}
	switch {
	case accountPlatform == PlatformAntigravity &&
		(inboundPlatform == PlatformAnthropic || inboundPlatform == PlatformGemini):
		// mixed scheduling 组合；IsMixedSchedulingEnabled 账号级开关
		// 在 bindingCapabilityAllows 中执行（设计 §3.4）。
		return true
	case inboundPlatform == PlatformOpenAI && accountPlatform == PlatformOpenCodeGo:
		// OpenAI Chat Completions / Responses 入站 × opencode_go 账号，
		// 由 opencode_openai_adapter.go 承接（设计 §4）。
		return true
	default:
		return false
	}
}

// bindingCapabilityAllows 在 SupportsBinding 之上叠加账号级资格：
// antigravity 账号承接 anthropic/gemini 分组时必须启用 mixed_scheduling。
func bindingCapabilityAllows(platform string, acc *Account) bool {
	if acc == nil {
		return false
	}
	if !SupportsBinding(platform, acc.Platform, acc.GetAPIProtocol()) {
		return false
	}
	if acc.Platform == PlatformAntigravity && acc.Platform != platform && !acc.IsMixedSchedulingEnabled() {
		return false
	}
	return true
}

// filterAccountsByBindingCapability 按能力矩阵内存过滤候选账号；
// 被跳过的账号记 binding_capability_skip Debug 日志（设计 §3.2）。
func filterAccountsByBindingCapability(accounts []Account, platform string) []Account {
	kept := make([]Account, 0, len(accounts))
	for i := range accounts {
		if bindingCapabilityAllows(platform, &accounts[i]) {
			kept = append(kept, accounts[i])
			continue
		}
		slog.Debug("binding_capability_skip",
			"group_platform", platform,
			"account_id", accounts[i].ID,
			"account_platform", accounts[i].Platform)
	}
	return kept
}
```

注：`apiProtocol` 参数当前矩阵未消费（opencode 的协议分派由 `ResolveOpenCodeGoUpstreamProtocol` 在转发层完成），按设计 §3.2 签名保留占位，不删除；后续按协议细化矩阵时（设计 §7.1 "opencode api_protocol 变体"）在 `case` 分支内消费。

- [ ] **Step 4: 运行确认通过**

```bash
cd backend && go test -tags=unit ./internal/service/ -run 'TestSupportsBindingMatrix|TestBindingCapabilityAllowsAntigravitySwitch' -v
```

预期: 全部 PASS。

- [ ] **Step 5: 提交**

```bash
cd backend && go vet ./internal/service/ && cd ..
git add backend/internal/service/binding_capability.go backend/internal/service/binding_capability_test.go
git commit -m "feat: 新增跨平台绑定能力矩阵 SupportsBinding"
```

---

### Task 3: 绑定保存放宽——`checkMixedChannelRisk` 降级为审计日志

**Files:**
- Modify: `backend/internal/service/admin_account.go`（三个保存路径调用点，约 :511 / :834 / :1046，用 `grep -n 'checkMixedChannelRisk(ctx' backend/internal/service/admin_account.go` 定位）
- Test: `backend/internal/service/admin_account_binding_test.go`（创建）

**Interfaces:**
- Consumes: `MixedChannelError`（admin_account.go:1607，结构不变）
- Produces: `func (s *adminServiceImpl) logCrossPlatformBindingWarning(err error)`；保存路径不再因跨平台绑定返回错误（`check-mixed-channel` 只读端点行为不变，继续供前端弹窗提示）

- [ ] **Step 1: 写失败测试**

创建 `backend/internal/service/admin_account_binding_test.go`：

```go
//go:build unit

package service

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// TestLogCrossPlatformBindingWarning 跨平台绑定保存不再硬阻断（设计 §3.1），
// 仅记 binding_cross_platform Warn 审计日志。
func TestLogCrossPlatformBindingWarning(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)

	(&adminServiceImpl{}).logCrossPlatformBindingWarning(&MixedChannelError{
		GroupID:         7,
		GroupName:       "混合分组",
		CurrentPlatform: "Anthropic",
		OtherPlatform:   "Antigravity",
	})
	// 非 MixedChannelError 必须静默（无日志、无 panic）。
	(&adminServiceImpl{}).logCrossPlatformBindingWarning(nil)

	out := buf.String()
	if !strings.Contains(out, "binding_cross_platform") {
		t.Fatalf("期望 binding_cross_platform 审计日志, got: %s", out)
	}
	if !strings.Contains(out, "group_id=7") {
		t.Fatalf("审计日志应含 group_id, got: %s", out)
	}
	if strings.Count(out, "binding_cross_platform") != 1 {
		t.Fatalf("nil 错误不应产生日志, got: %s", out)
	}
}
```

- [ ] **Step 2: 运行确认失败**

```bash
cd backend && go test -tags=unit ./internal/service/ -run TestLogCrossPlatformBindingWarning
```

预期: 编译失败，`s.logCrossPlatformBindingWarning undefined`。

- [ ] **Step 3: 实现**

在 `backend/internal/service/admin_account.go` 的 `checkMixedChannelRisk` 函数（:1484）之后新增：

```go
// logCrossPlatformBindingWarning 将跨平台绑定风险降级为审计日志。
// 设计 §3.1：绑定保存始终成功（只要分组存在且有效），不再硬阻断；
// 保留观测点便于回滚与问题定位。日志不输出任何凭据。
func (s *adminServiceImpl) logCrossPlatformBindingWarning(err error) {
	var mixedErr *MixedChannelError
	if !errors.As(err, &mixedErr) {
		return
	}
	slog.Warn("binding_cross_platform",
		"group_id", mixedErr.GroupID,
		"group_name", mixedErr.GroupName,
		"current_platform", mixedErr.CurrentPlatform,
		"other_platform", mixedErr.OtherPlatform)
}
```

然后修改三个保存路径调用点（用 `grep -n 'checkMixedChannelRisk(ctx' backend/internal/service/admin_account.go` 定位；`ValidateAccountGroupBindings`（:1562，composite/simple-mode 校验）保留不动）。以创建路径（:511 附近）为例，把：

```go
	// 检查混合渠道风险（除非用户已确认）
	if len(groupIDs) > 0 && !input.SkipMixedChannelCheck {
		if err := s.checkMixedChannelRisk(ctx, 0, input.Platform, groupIDs); err != nil {
			return nil, err
		}
	}
```

改为：

```go
	// 跨平台绑定不再硬阻断（设计 §3.1）：仅记 binding_cross_platform 审计日志。
	// check-mixed-channel 只读端点与前端确认弹窗保留，仅作提示。
	if len(groupIDs) > 0 && !input.SkipMixedChannelCheck {
		s.logCrossPlatformBindingWarning(s.checkMixedChannelRisk(ctx, 0, input.Platform, groupIDs))
	}
```

其余两处（更新路径 `checkMixedChannelRisk(ctx, account.ID, account.Platform, *input.GroupIDs)`、批量路径 `checkMixedChannelRisk(ctx, accountID, platform, *input.GroupIDs)`）做同样替换，保留各自的 `if len(...) > 0 && !...SkipMixedChannelCheck` 外层条件与原实参。确认文件顶部已 import `log/slog`（若没有则补）。

- [ ] **Step 4: 检查并修正既有断言**

```bash
grep -rn 'MixedChannelError' backend/internal/service/*_test.go
```

若有服务层用例断言"创建/更新账号被 MixedChannelError 阻断"，改为断言"保存成功"。handler 层 `backend/internal/handler/admin/account_handler_mixed_channel_test.go` 使用 `stubAdminService` 注入错误，测的是 handler 对 MixedChannelError 的映射（防御性保留），**不改**，预期继续通过。

- [ ] **Step 5: 运行测试**

```bash
cd backend && go test -tags=unit ./internal/service/ -run 'TestLogCrossPlatformBindingWarning|MixedChannel' -v
go test -tags=unit ./internal/handler/admin/ -run 'MixedChannel' -v
```

预期: 全部 PASS。

- [ ] **Step 6: 提交**

```bash
git add backend/internal/service/admin_account.go backend/internal/service/admin_account_binding_test.go
git commit -m "feat: 跨平台绑定保存放宽为审计日志（移除 checkMixedChannelRisk 硬阻断）"
```

---

### Task 4: 调度候选集按 `account_groups` 绑定维度查询

**Files:**
- Modify: `backend/internal/service/gateway_scheduling.go` 的 `listSchedulableAccounts`（:1020，分组直查路径）
- Modify: `backend/internal/service/scheduler_snapshot_service.go` 的 `loadAccountsFromDB`（:1469，分组路径）
- Test: `backend/internal/service/gateway_scheduling_binding_test.go`（创建）

**Interfaces:**
- Consumes: `AccountRepository.ListSchedulableByGroupID(ctx, groupID)`（`account_service.go:87` 接口已有，`account_repo.go:1949` 已实现，绑定维度、无平台过滤、含 status/schedulable 既有条件）；Task 2 的 `filterAccountsByBindingCapability`
- Produces: 分组 + 非强制场景候选集 = 绑定全量候选 → 能力过滤（快照与直查一致）；`ListSchedulableUngroupedByPlatform` / `ListSchedulableByPlatform`（无分组路径）保持平台过滤不变

- [ ] **Step 1: 写失败测试**

创建 `backend/internal/service/gateway_scheduling_binding_test.go`：

```go
//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

// mockBindingAccountRepo 只实现绑定维度调度路径会触达的方法。
type mockBindingAccountRepo struct {
	AccountRepository
	accounts []Account
}

func (m *mockBindingAccountRepo) ListSchedulableByGroupID(ctx context.Context, groupID int64) ([]Account, error) {
	return m.accounts, nil
}
func (m *mockBindingAccountRepo) GetByID(ctx context.Context, id int64) (*Account, error) {
	for i := range m.accounts {
		if m.accounts[i].ID == id {
			return &m.accounts[i], nil
		}
	}
	return nil, ErrNoAvailableAccounts
}

func newBindingTestService(accounts []Account) *GatewayService {
	return &GatewayService{
		accountRepo: &mockBindingAccountRepo{accounts: accounts},
		cache:       &mockGatewayCacheForPlatform{},
		cfg:         testConfig(),
	}
}

// TestSelectAccountForModelWithPlatform_BindingCandidates 分组平台为 anthropic、
// 绑定候选含 opencode/antigravity 账号：能力过滤跳过无适配器组合（设计 §3.3）。
func TestSelectAccountForModelWithPlatform_BindingCandidates(t *testing.T) {
	svc := newBindingTestService([]Account{
		{ID: 1, Platform: PlatformAnthropic, Priority: 3, Status: StatusActive, Schedulable: true},
		{ID: 2, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Priority: 1, Status: StatusActive, Schedulable: true}, // 无适配器，应被跳过
		{ID: 3, Platform: PlatformAntigravity, Priority: 2, Status: StatusActive, Schedulable: true, mixedSchedulingEnabled: true},
	})

	acc, err := svc.selectAccountForModelWithPlatform(context.Background(), ptr(int64(11)), "", "", nil, PlatformAnthropic)
	require.NoError(t, err)
	require.NotEqual(t, int64(2), acc.ID, "opencode 账号不应被 anthropic 分组选中")
}

// TestSelectAccountForModelWithPlatform_OpenAIxOpenCode openai 分组绑定
// opencode_go 账号：能力矩阵支持，应被选中（设计 §3.2）。
func TestSelectAccountForModelWithPlatform_OpenAIxOpenCode(t *testing.T) {
	svc := newBindingTestService([]Account{
		{ID: 9, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Priority: 1, Status: StatusActive, Schedulable: true},
	})

	acc, err := svc.selectAccountForModelWithPlatform(context.Background(), ptr(int64(12)), "", "", nil, PlatformOpenAI)
	require.NoError(t, err)
	require.Equal(t, int64(9), acc.ID)
}

// TestSelectAccountForModelWithPlatform_NoAdapterError 分组内只有无适配器账号时，
// 选号失败错误必须明确指出"分组平台 × 账号平台 无适配器"（设计 §3.2/§6）。
func TestSelectAccountForModelWithPlatform_NoAdapterError(t *testing.T) {
	svc := newBindingTestService([]Account{
		{ID: 5, Platform: PlatformOpenCodeGo, Type: AccountTypeAPIKey, Priority: 1, Status: StatusActive, Schedulable: true},
	})

	_, err := svc.selectAccountForModelWithPlatform(context.Background(), ptr(int64(13)), "", "glm-5.3", nil, PlatformAnthropic)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
	require.Contains(t, err.Error(), `has no adapter for account platform "opencode_go"`,
		"错误文案需指明无适配器: %v", err)
	require.Contains(t, err.Error(), `group platform "anthropic"`, "错误文案需含分组平台: %v", err)
}
```

注：`Account.mixedSchedulingEnabled` 的构造方式与 Task 2 Step 1 同一约定（以 `IsMixedSchedulingEnabled()` 真实数据路径为准）；`mockGatewayCacheForPlatform` 复用 `gateway_multiplatform_test.go` 既有桩；`ptr` 复用同文件既有泛型辅助。

- [ ] **Step 2: 运行确认失败**

```bash
cd backend && go test -tags=unit ./internal/service/ -run 'TestSelectAccountForModelWithPlatform_BindingCandidates|TestSelectAccountForModelWithPlatform_OpenAIxOpenCode|TestSelectAccountForModelWithPlatform_NoAdapterError'
```

预期: FAIL——候选查询走的是 `ListSchedulableByGroupIDAndPlatform`（平台过滤），opencode 账号根本不在候选里，第一个用例断言"选中 1 或 3"之外的行为与第三个用例的错误文案不满足（错误为笼统 `no available accounts`）。以实际失败输出为准，记录后进入实现。

- [ ] **Step 3: 改 `listSchedulableAccounts` 分组直查路径**

`backend/internal/service/gateway_scheduling.go` `listSchedulableAccounts`（:1020）中，把非 mixed 的分组查询段：

```go
	} else if groupID != nil {
		accounts, err = s.accountRepo.ListSchedulableByGroupIDAndPlatform(ctx, *groupID, platform)
		// 分组内无账号则返回空列表，由上层处理错误，不再回退到全平台查询
	}
```

改为：

```go
	} else if groupID != nil {
		// 绑定维度查询（设计 §3.3）：候选集由 account_groups 决定，不再按
		// 账号平台 == 分组平台 SQL 过滤；能力过滤移到内存执行。
		accounts, err = s.accountRepo.ListSchedulableByGroupID(ctx, *groupID)
		if err == nil {
			accounts = filterAccountsByBindingCapability(accounts, platform)
		}
		// 分组内无账号则返回空列表，由上层处理错误，不再回退到全平台查询
	}
```

混合调度分支（`useMixed` 为 true 的 `ListSchedulableByGroupIDAndPlatforms` 路径）保持原样——Task 5 会把 antigravity 开关统一收敛进能力过滤后评估是否可简化，本期不动（最小改动原则）。

- [ ] **Step 4: 改快照加载路径（快照与直查一致）**

`backend/internal/service/scheduler_snapshot_service.go` `loadAccountsFromDB`（:1469）中，把分组非 mixed 分支：

```go
	if groupID > 0 {
		return s.accountRepo.ListSchedulableByGroupIDAndPlatform(ctx, groupID, bucket.Platform)
	}
```

改为：

```go
	if groupID > 0 {
		// 绑定维度查询（设计 §3.3）：与 listSchedulableAccounts 直查路径一致，
		// 快照桶内做内存能力过滤；bucket 键（GroupID×Platform×Mode）不变。
		accounts, err := s.accountRepo.ListSchedulableByGroupID(ctx, groupID)
		if err != nil {
			return nil, err
		}
		return filterAccountsByBindingCapability(accounts, bucket.Platform), nil
	}
```

`useMixed` 分支保持原样。缓存语义说明：跨平台账号现在会进入其绑定分组的多个平台桶，桶重建仍由既有 outbox 账号/分组事件驱动，无需新增失效逻辑；部署后首个重建周期内旧桶（平台过滤版）会与新直查结果短暂不一致，由 `listSchedulableAccounts` 快照命中后的既有过滤链兜底（Task 5 中 `isAccountAllowedForPlatform` 能力化后即为兜底）。

- [ ] **Step 5: 运行测试（错误文案断言可能仍失败，属 Task 5 范围则先改为宽断言）**

```bash
cd backend && go test -tags=unit ./internal/service/ -run 'TestSelectAccountForModelWithPlatform_BindingCandidates|TestSelectAccountForModelWithPlatform_OpenAIxOpenCode' -v
```

预期: PASS。`TestSelectAccountForModelWithPlatform_NoAdapterError` 的文案断言在 Task 5 实现前会 FAIL——保持该用例为已知失败，Task 5 完成后转绿；若希望每步全绿，可先注释 `require.Contains` 两行并标注 `// Task 5 转绿`，Task 5 恢复。

- [ ] **Step 6: 提交**

```bash
git add backend/internal/service/gateway_scheduling.go backend/internal/service/scheduler_snapshot_service.go backend/internal/service/gateway_scheduling_binding_test.go
git commit -m "feat: 调度候选集按 account_groups 绑定维度查询并做内存能力过滤"
```

---

### Task 5: `isAccountAllowedForPlatform` 能力化 + 选号失败明确错误

**Files:**
- Modify: `backend/internal/service/gateway_scheduling.go`（`isAccountAllowedForPlatform` :1136；`selectionFailureStats` :2423；`summarizeSelectionFailureStats` :2593；两个错误构造点 :2141 / :2408；失败统计收集点 :335 附近）
- Test: `backend/internal/service/gateway_scheduling_binding_test.go`（追加/恢复断言）

**Interfaces:**
- Consumes: Task 2 `bindingCapabilityAllows` / `SupportsBinding`
- Produces: `selectionFailureStats.BindingFiltered int` + `BindingSkippedPlatforms []string`；错误文案 `no available accounts: group platform "X" has no adapter for account platform(s) "Y" (model: Z)`（包裹 `ErrNoAvailableAccounts`，`errors.Is` 兼容既有 `classifySelectionFailureError` → 503 分类）

- [ ] **Step 1: 恢复/确认 Task 4 的失败文案用例处于失败状态**

```bash
cd backend && go test -tags=unit ./internal/service/ -run TestSelectAccountForModelWithPlatform_NoAdapterError
```

预期: FAIL（文案仍是 `platform_filtered=N` 形式或笼统文案）。

- [ ] **Step 2: `isAccountAllowedForPlatform` 切换到能力矩阵**

把 `isAccountAllowedForPlatform`（:1136）：

```go
func (s *GatewayService) isAccountAllowedForPlatform(account *Account, platform string, useMixed bool) bool {
	if account == nil {
		return false
	}
	if useMixed {
		if account.Platform == platform {
			return true
		}
		return account.Platform == PlatformAntigravity && account.IsMixedSchedulingEnabled()
	}
	return account.Platform == platform
}
```

改为：

```go
// isAccountAllowedForPlatform 候选资格判断（设计 §3.2/§3.3）：
// 以能力矩阵为准，useMixed 仅保留参数兼容；antigravity 账号承接
// anthropic/gemini 的开关语义由 bindingCapabilityAllows 统一执行，
// 行为变化：强制平台模式下 mixed_scheduling 开启的 antigravity 账号
// 现在也可入选（能力矩阵显式支持项，设计 §3.2）。
func (s *GatewayService) isAccountAllowedForPlatform(account *Account, platform string, useMixed bool) bool {
	return bindingCapabilityAllows(platform, account)
}
```

`useMixed` 参数保留（4 个调用点 :299/:359/:543/:668 不动）；若 govet/lint 报未使用参数，改名为 `_ useMixed bool` 会破坏接口阅读，保留原参数名即可（Go 允许未使用函数参数）。

- [ ] **Step 3: 失败统计与错误文案**

`selectionFailureStats`（:2423）追加两个字段：

```go
	BindingFiltered    int     // 能力矩阵过滤（无适配器）计数
	BindingSkipped     []int64 // 被能力过滤的账号平台样本（去重前的账号 ID 样本复用 Sample 模式可省略）
	BindingPlatforms   []string // 被能力过滤的账号平台（去重）
```

在候选过滤循环（:299 附近，`if !s.isAccountAllowedForPlatform(account, platform, useMixed) { filteredPlatform++; continue }`）改为：

```go
			if !s.isAccountAllowedForPlatform(account, platform, useMixed) {
				if !SupportsBinding(platform, account.Platform, account.GetAPIProtocol()) {
					stats.BindingFiltered++
					if !containsString(stats.BindingPlatforms, account.Platform) {
						stats.BindingPlatforms = append(stats.BindingPlatforms, account.Platform)
					}
				}
				filteredPlatform++
				continue
			}
```

其中 `containsString` 若无既有辅助则新增：

```go
func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
```

确认该循环把 `stats`（`selectionFailureStats`）传递到 :335 附近的统计收集调用（以 `grep -n 'filteredPlatform' backend/internal/service/gateway_scheduling.go` 定位收集点并同步追加 `BindingFiltered`/`BindingPlatforms` 字段传递）。

`summarizeSelectionFailureStats`（:2593）格式串追加 `binding_unsupported=%d` 并传 `stats.BindingFiltered`。

在两个错误构造点（:2141、:2408，`grep -n 'summarizeSelectionFailureStats(stats)'` 定位），把：

```go
		return nil, fmt.Errorf("%w supporting model: %s (%s)", ErrNoAvailableAccounts, requestedModel, summarizeSelectionFailureStats(stats))
```

改为：

```go
		if stats.BindingFiltered > 0 && stats.Eligible == 0 {
			return nil, fmt.Errorf("%w: group platform %q has no adapter for account platform(s) %s (model: %s)",
				ErrNoAvailableAccounts, platform, strings.Join(stats.BindingPlatforms, ", "), requestedModel)
		}
		return nil, fmt.Errorf("%w supporting model: %s (%s)", ErrNoAvailableAccounts, requestedModel, summarizeSelectionFailureStats(stats))
```

（`platform` 变量若在该作用域不可见，从函数入参透传；两个构造点所在函数签名里已有 platform 参数。）

- [ ] **Step 4: 运行绑定调度用例 + 既有调度回归**

```bash
cd backend && go test -tags=unit ./internal/service/ -run 'TestSelectAccountForModelWithPlatform' -v
```

预期: 全部 PASS（含 Task 4 的 `NoAdapterError` 转绿、既有 `gateway_multiplatform_test.go` / `gemini_multiplatform_test.go` / sticky 用例）。

- [ ] **Step 5: 补 mixed scheduling 回归断言（行为不变性）**

追加到 `gateway_scheduling_binding_test.go`：

```go
// TestSelectAccountForModelWithPlatform_MixedSchedulingUnchanged
// anthropic 分组 × antigravity 账号 mixed 开关语义不变（设计 §3.4）：
// 开启可入选、关闭被跳过；gemini 分组同理。
func TestSelectAccountForModelWithPlatform_MixedSchedulingUnchanged(t *testing.T) {
	svcOn := newBindingTestService([]Account{
		{ID: 21, Platform: PlatformAntigravity, Priority: 1, Status: StatusActive, Schedulable: true, mixedSchedulingEnabled: true},
	})
	acc, err := svcOn.selectAccountForModelWithPlatform(context.Background(), ptr(int64(31)), "", "", nil, PlatformAnthropic)
	require.NoError(t, err)
	require.Equal(t, int64(21), acc.ID)

	svcOff := newBindingTestService([]Account{
		{ID: 22, Platform: PlatformAntigravity, Priority: 1, Status: StatusActive, Schedulable: true, mixedSchedulingEnabled: false},
	})
	_, err = svcOff.selectAccountForModelWithPlatform(context.Background(), ptr(int64(32)), "", "", nil, PlatformAnthropic)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrNoAvailableAccounts)
}
```

```bash
cd backend && go test -tags=unit ./internal/service/ -run 'MixedSchedulingUnchanged' -v
```

预期: PASS。

- [ ] **Step 6: 提交**

```bash
git add backend/internal/service/gateway_scheduling.go backend/internal/service/gateway_scheduling_binding_test.go
git commit -m "feat: 候选资格判断接入能力矩阵并输出明确无适配器错误"
```

---

### Task 6: `/v1/chat/completions` 转发前终检

**Files:**
- Modify: `backend/internal/handler/gateway_handler_chat_completions.go`（:254 的 gemini 硬过滤；用 `grep -n 'groupPlatform == service.PlatformGemini && account.Platform != service.PlatformGemini' backend/internal/handler/gateway_handler_chat_completions.go` 定位）

**Interfaces:**
- Consumes: Task 2 `service.SupportsBinding`；handler 既有 `fs.FailedAccountIDs` failover 机制
- Produces: 转发前终检按能力矩阵执行；不支持组合记 `binding_capability_unsupported` Warn 并按 failover 切换下一账号（设计 §3.2/§6）

- [ ] **Step 1: 定位并替换硬过滤**

把（:254 附近）：

```go
		if groupPlatform == service.PlatformGemini && account.Platform != service.PlatformGemini {
			if accountReleaseFunc != nil {
				accountReleaseFunc()
			}
			fs.FailedAccountIDs[account.ID] = struct{}{}
			continue
		}
```

改为：

```go
		// 转发前终检（设计 §3.2）：兜底防止 snapshot 过期导致的跨平台错配；
		// 不支持组合按 failover 处理并记 Warn，绝不静默猜测转发。
		if !service.SupportsBinding(groupPlatform, account.Platform, account.GetAPIProtocol()) {
			reqLog.Warn("binding_capability_unsupported",
				zap.String("group_platform", groupPlatform),
				zap.String("account_platform", account.Platform),
				zap.Int64("account_id", account.ID))
			if accountReleaseFunc != nil {
				accountReleaseFunc()
			}
			fs.FailedAccountIDs[account.ID] = struct{}{}
			continue
		}
```

antigravity 的 mixed 开关已在选号层过滤，此处终检只做矩阵判定；若 snapshot 过期导致 mixed 关闭的 antigravity 账号漏到这里，由 antigravity 兼容层既有行为处理（与现状一致，不扩大终检范围）。确认 `zap` 已 import（该文件已用 `zap.Int64` 等，无需新增）。

- [ ] **Step 2: 运行 handler 回归**

```bash
cd backend && go test -tags=unit ./internal/handler/ -run 'TestGateway|TestComposite' -v
```

预期: 全部 PASS。若有用例断言"gemini 分组必跳过非 gemini 账号"，按能力矩阵修正断言（gemini×antigravity mixed 开启应放行；gemini×opencode 应跳过）。

- [ ] **Step 3: 提交**

```bash
git add backend/internal/handler/gateway_handler_chat_completions.go
git commit -m "feat: chat completions 转发前终检接入绑定能力矩阵"
```

---

### Task 7: OpenCode OpenAI 适配器收敛（请求 + 凭据/base URL 预检）

**Files:**
- Create: `backend/internal/service/opencode_openai_adapter.go`
- Test: `backend/internal/service/opencode_openai_adapter_test.go`（创建）
- Modify: `backend/internal/service/openai_gateway_forward.go`（:162-172 的 opencode 分支，`grep -n 'if account.IsOpenCodeGo()' backend/internal/service/openai_gateway_forward.go` 定位）
- Modify: `backend/internal/service/openai_gateway_chat_completions.go`（:117-152 的 opencode 分支，`grep -n 'if account.IsOpenCodeGo()' backend/internal/service/openai_gateway_chat_completions.go` 定位）

**Interfaces:**
- Consumes: `resolveOpenCodeGoMappedModel`（openai_gateway_messages_anthropic_native.go:143）、`openCodeGoNativeProtocol`（opencode_go.go:294）、`forwardResponsesViaNativeAnthropic`（openai_gateway_responses_anthropic_native.go:37）、`forwardResponsesViaRawChatCompletions`（openai_gateway_responses_chat_fallback.go:26）、`forwardChatCompletionsViaNativeAnthropic`（openai_gateway_chat_completions_anthropic_native.go:36）、`forwardAsRawChatCompletions`（openai_gateway_chat_completions_raw.go:56）、`apicompat.ResponsesToChatCompletionsRequestWithOptions`、`s.reasoningContentByID`
- Produces: `func (s *OpenAIGatewayService) forwardOpenCodeGoFromResponses(ctx, c, account, body) (*OpenAIForwardResult, error)`、`func (s *OpenAIGatewayService) forwardOpenCodeGoFromChatCompletions(ctx, c, account, body, defaultMappedModel) (*OpenAIForwardResult, error)`（两者都约定：返回 `(nil, nil)` 表示目标协议为原生 Responses，调用方继续走通用转换链）；`func ensureOpenCodeGoForwardCredentials(account *Account) error`

行为约束：本任务为纯收敛重构 + 预检，转换/URL/模型映射行为与现状逐字节一致（由既有 `openai_gateway_opencode_mapping_test.go` 全量守护）；唯一新增行为是凭据缺失时转发前报错（设计 §4.6），不再等到上游 401。

- [ ] **Step 1: 写失败测试**

创建 `backend/internal/service/opencode_openai_adapter_test.go`：

```go
//go:build unit

package service

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// TestEnsureOpenCodeGoForwardCredentials 凭据预检（设计 §4.6）：
// 非 APIKey 类型 / 缺 api_key 报明确错误，不静默回退。
func TestEnsureOpenCodeGoForwardCredentials(t *testing.T) {
	ok := openCodeMappedTestAccount()
	require.NoError(t, ensureOpenCodeGoForwardCredentials(ok))

	noKey := openCodeMappedTestAccount()
	noKey.Credentials = map[string]any{"account_mode": "zen"}
	err := ensureOpenCodeGoForwardCredentials(noKey)
	require.Error(t, err)
	require.Contains(t, err.Error(), "api_key")

	oauth := openCodeMappedTestAccount()
	oauth.Type = AccountTypeOAuth
	require.Error(t, ensureOpenCodeGoForwardCredentials(oauth))
}

// TestForwardOpenCodeGoFromResponsesDispatch Responses 入站 × opencode 账号
// 三种目标协议分派（收敛后行为与原分支一致）。
func TestForwardOpenCodeGoFromResponsesDispatch(t *testing.T) {
	cases := []struct {
		name      string
		protocol  string
		wantURL   string
		wantModel string
	}{
		{"到 anthropic", APIProtocolAnthropic, "https://opencode.ai/zen/v1/messages", "glm-5.3"},
		{"到 chat_completions", APIProtocolChatCompletions, "https://opencode.ai/zen/v1/chat/completions", "glm-5.3"},
		{"到 responses（回通用链）", APIProtocolResponses, "https://opencode.ai/zen/v1/responses", "glm-5.3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			account := openCodeMappedTestAccount()
			account.Credentials["api_protocol"] = tc.protocol
			body := []byte(`{"model":"opencode/glm-5.3","stream":false,"input":"hi"}`)

			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       nopCloseBody(`{"id":"resp_oc","object":"response","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`),
			}}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			c := adaptiveProtocolTestContext("/v1/responses", body)

			result, err := svc.forwardOpenCodeGoFromResponses(context.Background(), c, account, body)
			require.NoError(t, err)
			if tc.protocol == APIProtocolResponses {
				// (nil, nil) 约定：回通用 Responses 链
				require.Nil(t, result)
				return
			}
			require.NotNil(t, upstream.lastReq)
			require.Equal(t, tc.wantURL, upstream.lastReq.URL.String())
			require.Equal(t, tc.wantModel, gjson.GetBytes(upstream.lastBody, "model").String())
		})
	}
}

// TestForwardOpenCodeGoFromChatCompletionsDispatch CC 入站 × opencode 账号分派。
func TestForwardOpenCodeGoFromChatCompletionsDispatch(t *testing.T) {
	cases := []struct {
		name     string
		protocol string
		wantURL  string
	}{
		{"直通 chat_completions", APIProtocolChatCompletions, "https://opencode.ai/zen/v1/chat/completions"},
		{"到 anthropic", APIProtocolAnthropic, "https://opencode.ai/zen/v1/messages"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			account := openCodeMappedTestAccount()
			account.Credentials["api_protocol"] = tc.protocol
			body := []byte(`{"model":"opencode/glm-5.3","stream":false,"messages":[{"role":"user","content":"hi"}]}`)

			upstream := &httpUpstreamRecorder{resp: &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": []string{"application/json"}},
				Body:       nopCloseBody(`{"id":"chatcmpl_oc","object":"chat.completion","model":"glm-5.3","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`),
			}}
			svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
			c := adaptiveProtocolTestContext("/v1/chat/completions", body)

			result, err := svc.forwardOpenCodeGoFromChatCompletions(context.Background(), c, account, body, "")
			require.NoError(t, err)
			require.NotNil(t, result)
			require.NotNil(t, upstream.lastReq)
			require.Equal(t, tc.wantURL, upstream.lastReq.URL.String())
		})
	}
}
```

流式与工具用例统一使用 `io.NopCloser(strings.NewReader(s))` 构造上游响应体，并在文件 import 中补 `bytes`、`io`、`net/http/httptest`。

- [ ] **Step 2: 运行确认失败**

```bash
cd backend && go test -tags=unit ./internal/service/ -run 'TestEnsureOpenCodeGoForwardCredentials|TestForwardOpenCodeGoFromResponsesDispatch|TestForwardOpenCodeGoFromChatCompletionsDispatch'
```

预期: 编译失败，`undefined: ensureOpenCodeGoForwardCredentials` / `svc.forwardOpenCodeGoFromResponses undefined`。

- [ ] **Step 3: 实现适配器**

创建 `backend/internal/service/opencode_openai_adapter.go`：

```go
package service

// opencode_openai_adapter.go 将 OpenAI 入站（/v1/responses、/v1/chat/completions）
// 到 opencode_go 账号的适配收敛为显式适配器（设计 §4）。
// 职责：目标协议分派、凭据/base URL 转发前预检。
// 模型映射顺序与现网一致（设计 §4.5）：resolveOpenCodeGoMappedModel 内部先做
// 账号级 model_mapping 与前缀归一，分组级映射因 IsMultiProtocolAPIKeyProvider
// 对 opencode_go 返回空而不改写。协议转换复用 apicompat 桥接与既有转发函数，
// 流式/非流式/工具调用的具体转换在桥接层，本文件不做第二套实现。

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
)

// ensureOpenCodeGoForwardCredentials 转发前预检（设计 §4.6）：
// 仅支持 AccountTypeAPIKey 且 credentials["api_key"] 必须存在；
// 缺失属于配置错误，返回明确错误并记 Warn（日志不得输出 api_key 值）。
// base URL 解析沿用既有回退链 api_base_urls[protocol] → base_url →
// 按 account_mode 预设（zen: https://opencode.ai/zen/v1、go: https://opencode.ai/zen/go/v1，
// anthropic 协议见 openCodeDefaultAnthropicBaseURL），该回退为文档化预设而非静默猜测。
func ensureOpenCodeGoForwardCredentials(account *Account) error {
	if account == nil {
		return fmt.Errorf("opencode forward: account is nil")
	}
	if account.Type != AccountTypeAPIKey {
		return fmt.Errorf("opencode account %d: only api_key accounts are supported, got type %q", account.ID, account.Type)
	}
	if key, _ := account.Credentials["api_key"].(string); strings.TrimSpace(key) == "" {
		slog.Warn("opencode_forward_credential_missing", "account_id", account.ID)
		return fmt.Errorf("opencode account %d: credentials.api_key is required", account.ID)
	}
	return nil
}

// forwardOpenCodeGoFromResponses 处理 Responses 入站 × opencode_go 账号（设计 §4.2）。
// 返回 (nil, nil) 表示目标协议为原生 Responses，调用方继续通用 Responses 转发链。
func (s *OpenAIGatewayService) forwardOpenCodeGoFromResponses(
	ctx context.Context, c *gin.Context, account *Account, body []byte,
) (*OpenAIForwardResult, error) {
	if err := ensureOpenCodeGoForwardCredentials(account); err != nil {
		return nil, err
	}
	mapped := resolveOpenCodeGoMappedModel(account, body, "")
	switch openCodeGoNativeProtocol(account, mapped) {
	case APIProtocolAnthropic:
		return s.forwardResponsesViaNativeAnthropic(ctx, c, account, body, "")
	case APIProtocolResponses:
		return nil, nil
	default:
		return s.forwardResponsesViaRawChatCompletions(ctx, c, account, body)
	}
}

// forwardOpenCodeGoFromChatCompletions 处理 Chat Completions 入站 × opencode_go 账号（设计 §4.2）。
// 返回 (nil, nil) 表示目标协议为原生 Responses，调用方继续通用 CC→Responses 转换链
// （Chat Completions 请求先经 apicompat 归一为 Responses，与现网行为一致）。
func (s *OpenAIGatewayService) forwardOpenCodeGoFromChatCompletions(
	ctx context.Context, c *gin.Context, account *Account, body []byte, defaultMappedModel string,
) (*OpenAIForwardResult, error) {
	if err := ensureOpenCodeGoForwardCredentials(account); err != nil {
		return nil, err
	}
	mapped := resolveOpenCodeGoMappedModel(account, body, defaultMappedModel)
	proto := openCodeGoNativeProtocol(account, mapped)
	if proto == APIProtocolResponses {
		return nil, nil
	}
	// Responses 形状的 body 发到 CC URL 是 Cursor 等客户端的兼容场景，先归一。
	isResponsesShape := !gjson.GetBytes(body, "messages").Exists() && gjson.GetBytes(body, "input").Exists()
	if isResponsesShape {
		if proto == APIProtocolAnthropic {
			return s.forwardResponsesViaNativeAnthropic(ctx, c, account, body, "")
		}
		var responsesReq apicompat.ResponsesRequest
		if err := json.Unmarshal(body, &responsesReq); err != nil {
			return nil, fmt.Errorf("parse responses-shaped chat completions request: %w", err)
		}
		chatReq, err := apicompat.ResponsesToChatCompletionsRequestWithOptions(
			&responsesReq,
			&apicompat.ResponsesToChatOptions{ReasoningContentByID: s.reasoningContentByID},
		)
		if err != nil {
			return nil, fmt.Errorf("convert responses-shaped chat completions request: %w", err)
		}
		chatBody, err := json.Marshal(chatReq)
		if err != nil {
			return nil, fmt.Errorf("marshal converted chat completions request: %w", err)
		}
		return s.forwardAsRawChatCompletions(ctx, c, account, chatBody, defaultMappedModel)
	}
	if proto == APIProtocolAnthropic {
		return s.forwardChatCompletionsViaNativeAnthropic(ctx, c, account, body, defaultMappedModel)
	}
	return s.forwardAsRawChatCompletions(ctx, c, account, body, defaultMappedModel)
}
```

- [ ] **Step 4: 两个分派点委托适配器**

`openai_gateway_forward.go`（:162-172）把原 switch 块替换为：

```go
	if account.IsOpenCodeGo() {
		result, err := s.forwardOpenCodeGoFromResponses(ctx, c, account, body)
		if err != nil || result != nil {
			return result, err
		}
		// 目标协议为原生 Responses：继续下方通用 Responses 转发链。
	}
```

`openai_gateway_chat_completions.go`（:117-152）把原 `if account.IsOpenCodeGo() { ... }` 整块替换为：

```go
	// OpenCode Go：按模型原生协议分流（与 inbound 协议正交），逻辑收敛于
	// opencode_openai_adapter.go（设计 §4）。规则未命中一律兜底 Chat Completions。
	if account.IsOpenCodeGo() {
		result, err := s.forwardOpenCodeGoFromChatCompletions(ctx, c, account, body, defaultMappedModel)
		if err != nil || result != nil {
			return result, err
		}
		// 目标协议为原生 Responses：落到下方通用 CC→Responses 转换链。
	}
```

被移动代码遗留的未使用 import（如 `apicompat` 若仅此处使用）按编译器提示清理；`isResponsesShape` 变量在 `openai_gateway_chat_completions.go` 中若仍被后续分支使用则保留原计算。

- [ ] **Step 5: 运行适配器与既有映射回归**

```bash
cd backend && go test -tags=unit ./internal/service/ -run 'OpenCode' -v
go test -tags=unit ./internal/service/ -run 'TestOpenCodeGatewayAppliesMappedModelOnAllIngresses' -v
```

预期: 全部 PASS（既有映射用例零改动通过 = 行为等价证明）。

- [ ] **Step 6: 提交**

```bash
cd backend && go vet ./internal/service/ && cd ..
git add backend/internal/service/opencode_openai_adapter.go backend/internal/service/opencode_openai_adapter_test.go backend/internal/service/openai_gateway_forward.go backend/internal/service/openai_gateway_chat_completions.go
git commit -m "refactor: OpenAI 入站 × opencode_go 适配收敛为显式适配器并加凭据预检"
```

---

### Task 8: 适配器流式 / 工具调用 / 模型映射回归

**Files:**
- Test: `backend/internal/service/opencode_openai_adapter_test.go`（追加用例，不新增文件）

**Interfaces:**
- Consumes: Task 7 适配器入口；`adaptiveProtocolTestContext`、`httpUpstreamRecorder`、`rawChatCompletionsTestConfig`、`openCodeMappedTestAccount` 既有测试设施；`apicompat` 桥接的流式生命周期
- Produces: 设计 §7.1 要求的流式 SSE 事件序列、首包前 failover、工具调用双向、模型映射优先级回归用例

- [ ] **Step 1: 追加流式 + 工具调用失败测试**

追加到 `opencode_openai_adapter_test.go`：

```go
// TestOpenCodeCCIngressStreamViaResponses CC 入站（stream）× 目标协议 responses：
// 上游 Responses SSE 转换回 chat.completion.chunk 事件序列（设计 §4.3）。
func TestOpenCodeCCIngressStreamViaResponses(t *testing.T) {
	account := openCodeMappedTestAccount() // api_protocol: adaptive
	// 用映射到 responses 协议的模型：协议规则表里 GPT 系命中 responses。
	account.Credentials["model_mapping"] = map[string]any{"gpt-5.2": "gpt-5.2"}
	body := []byte(`{"model":"gpt-5.2","stream":true,"messages":[{"role":"user","content":"hi"}]}`)

	sse := "data: {\"type\":\"response.output_text.delta\",\"delta\":\"he\"}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"llo\"}\n\n" +
		"data: {\"type\":\"response.completed\",\"response\":{\"usage\":{\"input_tokens\":2,\"output_tokens\":2,\"total_tokens\":4}}}\n\n" +
		"data: [DONE]\n\n"
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		Body:       io.NopCloser(strings.NewReader(sse)),
	}}
	// 客户端响应取值：自带 recorder 的 context（adaptiveProtocolTestContext 不返回 recorder）。
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}

	_, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
	require.NoError(t, err)

	// 客户端应收到 OpenAI chunk 流；上游不得收到未转换的 CC body。
	require.NotNil(t, upstream.lastReq)
	require.Equal(t, "https://opencode.ai/zen/v1/responses", upstream.lastReq.URL.String())
	require.NotContains(t, string(upstream.lastBody), `"messages"`, "CC 请求体应已转换为 Responses 形状")
	require.Contains(t, recorder.Body.String(), `"delta"`, "客户端应收到流式增量")
}
```

若 `"delta"` 断言因流式 Flush 聚合方式不满足，改断言 `recorder.Body.Len() > 0`，并把事件序列断言保留在 apicompat 桥接既有用例（`chatcompletions_responses_stream_lifecycle_test.go` 已覆盖），在用例注释中说明。文件需补 import：`bytes`、`net/http/httptest`。

```go
// TestOpenCodeCCIngressToolCallsNonStream CC 入站（非流式）带 tools × 目标协议 responses：
// 请求侧 function tool 映射，响应侧 function_call 逆映射回 tool_calls（设计 §4.4）。
func TestOpenCodeCCIngressToolCallsNonStream(t *testing.T) {
	account := openCodeMappedTestAccount()
	account.Credentials["model_mapping"] = map[string]any{"gpt-5.2": "gpt-5.2"}
	body := []byte(`{"model":"gpt-5.2","stream":false,"messages":[{"role":"user","content":"天气如何"}],` +
		`"tools":[{"type":"function","function":{"name":"get_weather","description":"查天气","parameters":{"type":"object","properties":{"city":{"type":"string"}}}}}]}`)

	upstreamJSON := `{"id":"resp_tool","object":"response","status":"completed","output":[` +
		`{"type":"function_call","call_id":"call_1","name":"get_weather","arguments":"{\"city\":\"北京\"}"}],` +
		`"usage":{"input_tokens":5,"output_tokens":3,"total_tokens":8}}`
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(upstreamJSON)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	c := adaptiveProtocolTestContext("/v1/chat/completions", body)

	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")

	_, err := svc.ForwardAsChatCompletions(context.Background(), c, account, body, "", "")
	require.NoError(t, err)

	require.Contains(t, string(upstream.lastBody), `"get_weather"`, "请求侧 tools 应已映射")
	require.Contains(t, recorder.Body.String(), `"tool_calls"`, "客户端应收到 tool_calls")
	require.Contains(t, recorder.Body.String(), `"get_weather"`, "tool_calls 应含函数名")
}
```

（两个用例的 context 构造可抽一个测试辅助 `streamTestContext(t, path, body) (*gin.Context, *httptest.ResponseRecorder)`，放在本测试文件顶部。）

```go
// TestOpenCodeModelMappingPriority 模型映射优先级（设计 §4.5）：
// 前缀归一（opencode/、opencode-go/、opencode_go/）+ 账号级 model_mapping 精确映射。
func TestOpenCodeModelMappingPriority(t *testing.T) {
	account := openCodeMappedTestAccount() // model_mapping: opencode/claude-sonnet-4 -> claude-sonnet-4
	body := []byte(`{"model":"opencode/claude-sonnet-4","stream":false,"input":"hi"}`)
	upstream := &httpUpstreamRecorder{resp: &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(`{"id":"r","object":"response","status":"completed","output":[],"usage":{"input_tokens":1,"output_tokens":1,"total_tokens":2}}`)),
	}}
	svc := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig(), httpUpstream: upstream}
	c := adaptiveProtocolTestContext("/v1/responses", body)

	_, err := svc.forwardOpenCodeGoFromResponses(context.Background(), c, account, body)
	require.NoError(t, err)
	require.Equal(t, "claude-sonnet-4", gjson.GetBytes(upstream.lastBody, "model").String())
	require.NotContains(t, string(upstream.lastBody), "opencode/")
}
```

- [ ] **Step 2: 运行并按注释说明调整取值细节**

```bash
cd backend && go test -tags=unit ./internal/service/ -run 'TestOpenCode' -v
```

预期: 新增用例 PASS。若流式/响应取值细节与注释不符，按注释给出的降级路径调整（桥接层已有事件级用例，不让本任务重复实现桥接测试）。

- [ ] **Step 3: 全量 service 包回归（含 mixed scheduling 既有用例）**

```bash
cd backend && go test -tags=unit ./internal/service/ 
```

预期: 全部 PASS；特别确认 `account_pool_mode_test.go`、`openai_gateway_opencode_mapping_test.go`、`scheduler_snapshot_*_test.go` 未回归。

- [ ] **Step 4: 提交**

```bash
git add backend/internal/service/opencode_openai_adapter_test.go
git commit -m "test: 补齐 OpenCode 适配器流式/工具/模型映射回归用例"
```

---

### Task 9: 前端跨平台绑定 CRUD（GroupSelector 放开平台过滤）

**Files:**
- Modify: `frontend/src/components/common/GroupSelector.vue`（`filteredGroups` 计算属性与 Props）
- Modify: `frontend/src/components/account/CreateAccountModal.vue`、`EditAccountModal.vue`、`BulkEditAccountModal.vue`（GroupSelector 调用处）
- Test: `frontend/src/components/common/__tests__/GroupSelector.spec.ts`（修改）

**Interfaces:**
- Consumes: 后端 Task 3 已保证任意绑定保存成功；`Group` 类型的 `platform` 字段仅用于徽章展示
- Produces: `GroupSelector` 不再按账号平台过滤分组（保留 simple 模式排除 composite 与搜索过滤）；Props 中 `platform`/`mixedScheduling` 移除

- [ ] **Step 1: 修改测试（先红）**

`frontend/src/components/common/__tests__/GroupSelector.spec.ts`：
1. 删除/改写断言"只能选择同 platform 分组"的用例（若有）。
2. 新增用例（沿用文件顶部既有 `vi.mock('@/stores')` / `vi.mock('vue-i18n')` 与 `GroupBadge`/`Icon` stub，仅新增数据与用例）：

```typescript
const crossPlatformGroups = [
  { id: 11, name: 'AnthropicGroup', platform: 'anthropic', status: 'active' },
  { id: 12, name: 'OpenAIGroup', platform: 'openai', status: 'active' },
  { id: 13, name: 'OpenCodeGroup', platform: 'opencode_go', status: 'active' }
] as any

const mountCrossPlatform = (modelValue: number[] = []) => mount(GroupSelector, {
  props: { modelValue, groups: crossPlatformGroups },
  global: { stubs: { GroupBadge: { props: ['name'], template: '<span>{{ name }}</span>' }, Icon: true } }
})

// 放入既有 describe 块内（beforeEach 已重置 isSimpleMode）：
it('任意平台账号可选任意平台分组（跨平台绑定放开）', () => {
  const wrapper = mountCrossPlatform()
  const checkboxes = wrapper.findAll('input[type=checkbox]')
  expect(checkboxes.length).toBe(3)
  expect(wrapper.text()).toContain('OpenAIGroup')
  expect(wrapper.text()).toContain('OpenCodeGroup')
})
```

```bash
pnpm --dir frontend exec vitest run src/components/common/__tests__/GroupSelector.spec.ts
```

预期: 新用例 FAIL（当前实现按 platform 过滤只渲染 1 个）。

- [ ] **Step 2: 实现**

`GroupSelector.vue` 的 `filteredGroups`：

```typescript
// Filter groups by platform if specified
const filteredGroups = computed(() => {
  let result = authStore.isSimpleMode
    ? props.groups.filter((g) => g.platform !== 'composite')
    : props.groups
  // 跨平台绑定放开（设计 §3.1）：不再按账号平台过滤分组；
  // 平台徽章仍展示，能力提示交给确认弹窗与后端审计日志。
  if (isSearchable.value && searchText.value) {
    const q = searchText.value.toLowerCase()
    result = result.filter(
      (g) => g.name.toLowerCase().includes(q) || g.description?.toLowerCase().includes(q)
    )
  }
  return result
})
```

同时删除 `interface Props` 中的 `platform` 与 `mixedScheduling` 字段及其 `withDefaults` 相关项，并删除 watch 之外对它们的引用。

三个 Modal（Create/Edit/BulkEdit）把 `<GroupSelector ... :platform="account?.platform" :mixed-scheduling="mixedScheduling" ...>` 中的 `:platform` 与 `:mixed-scheduling` 两个绑定删除（`EditAccountModal.vue` 约 :2960-:2965；Create/BulkEdit 用 `grep -n 'GroupSelector' -A5 frontend/src/components/account/CreateAccountModal.vue` 定位）。注意：`mixedScheduling` ref 本身（账号的 mixed_scheduling 开关）继续保留并提交，只删 GroupSelector 上的绑定。

- [ ] **Step 3: 运行前端测试与类型检查**

```bash
pnpm --dir frontend exec vitest run src/components/common/__tests__/GroupSelector.spec.ts src/components/account/__tests__/CreateAccountModal.spec.ts src/components/account/__tests__/EditAccountModal.spec.ts src/components/account/__tests__/BulkEditAccountModal.spec.ts
pnpm --dir frontend run typecheck && pnpm --dir frontend run lint:check
```

预期: 全部 PASS / 无错误。Modal 既有用例若断言了 platform 过滤，按放开后的行为修正断言。

- [ ] **Step 4: 提交**

```bash
git add frontend/src/components/common/GroupSelector.vue frontend/src/components/common/__tests__/GroupSelector.spec.ts frontend/src/components/account/CreateAccountModal.vue frontend/src/components/account/EditAccountModal.vue frontend/src/components/account/BulkEditAccountModal.vue
git commit -m "feat: 前端分组选择放开跨平台绑定（GroupSelector 移除平台过滤）"
```

---

### Task 10: 全量回归

**Files:**
- 无新增；只运行与修复

**Interfaces:**
- Consumes: Task 1-9 全部产物
- Produces: 全绿基线

- [ ] **Step 1: 后端全量单测**

```bash
cd backend && go test -tags=unit ./...
```

预期: 全部 PASS。重点核对设计 §7.1 清单：能力矩阵、绑定、调度、mixed scheduling（`account_pool_mode_test.go` 等）、OpenCode 适配器全部用例。

- [ ] **Step 2: 后端全量测试（含集成 tag）**

```bash
cd backend && go test ./... 2>&1 | tail -20
```

预期: 与合并基线（Task 1 完成时）相同的通过/跳过集合；新增失败逐个归因，不允许带病提交。

- [ ] **Step 3: 前端全量**

```bash
pnpm --dir frontend run lint:check && pnpm --dir frontend run typecheck
make test-frontend-critical
```

预期: 全部 PASS。

- [ ] **Step 4: 人工核对设计回归清单（设计 §7.2 集成项的可单测子集）**

逐项确认存在对应用例或既有覆盖：
1. openai 分组绑定 opencode 账号，CC 非流式 + 流式（Task 7/8 用例）。
2. anthropic 分组绑定 opencode 账号（不支持组合）：选号失败文案 + `binding_capability_skip` Debug（Task 4/5 用例）。
3. anthropic 分组绑定 antigravity 账号 mixed 开/关行为不变（Task 5 用例）。
4. 无分组 API key 行为不变（`ListSchedulableUngroupedByPlatform` 未改动，既有用例守护）。

若有缺失，回到对应任务补用例（不允许在本任务里顺手实现功能）。

---

### Task 11: 最终构建、自审、推送

**Files:**
- Modify: `docs/superpowers/plans/2026-09-30-cross-platform-account-group-adapter-plan.md`（勾选完成项，可选）

**Interfaces:**
- Consumes: Task 1-10 全绿基线
- Produces: 推送到 `http://git.frp.andyzheng.top:26998/admin/sub2api` 的分支 `worktrees/wt_cross-platform-binding`

- [ ] **Step 1: 最终构建**

```bash
make build   # 预期: backend/bin/server 生成，前端 dist 生成，无错误
```

- [ ] **Step 2: 自审（对照设计文档逐条）**

```bash
git diff 1f3b5a480..HEAD --stat | tail -30
```

逐条核对：
- 设计 §3.1 绑定层放宽 → Task 3/9；`slog.Warn("binding_cross_platform", ...)` 存在：`grep -rn 'binding_cross_platform' backend/internal/`
- 设计 §3.2 能力矩阵/日志/错误 → Task 2/5/6；`grep -rn 'binding_capability_skip\|binding_capability_unsupported' backend/internal/`
- 设计 §3.3 绑定维度调度 → Task 4；`grep -n 'ListSchedulableByGroupID(ctx' backend/internal/service/gateway_scheduling.go backend/internal/service/scheduler_snapshot_service.go`
- 设计 §3.4 mixed scheduling 保留 → Task 5 用例 + 既有测试全绿
- 设计 §4 适配器 → Task 7/8；`grep -n 'forwardOpenCodeGoFrom' backend/internal/service/*.go | grep -v _test`
- 设计 §5 cc handler 终检 → Task 6
- 设计 §6 无静默回退 → `grep -rn 'SupportsBinding' backend/internal/ | grep -v _test` 覆盖选号+终检两处
- 错误码/日志指标：确认 `binding_capability_unsupported` 可被 dashboard 检索（结构化 slog 字段齐全），不新增 metrics 代码（设计 §9 第 4 步的监控接入不在本计划范围，仅保证日志字段稳定）

- [ ] **Step 3: 勾选计划文档完成项并提交（如有勾选变更）**

```bash
git add docs/superpowers/plans/2026-09-30-cross-platform-account-group-adapter-plan.md
git commit -m "docs: 勾选跨平台绑定实施计划完成项" || true
```

- [ ] **Step 4: 推送**

```bash
git remote -v | grep -q 'andy' || git remote add andy http://git.frp.andyzheng.top:26998/admin/sub2api.git
git push andy worktrees/wt_cross-platform-binding
```

预期: 推送成功（若远端要求认证，使用既有凭据助手；失败则报告错误原文）。

- [ ] **Step 5: 汇报**

在最终回复中给出：合并的 v0.2.11 SHA、各任务提交 SHA 列表、测试结果摘要（后端/前端）、推送结果。
