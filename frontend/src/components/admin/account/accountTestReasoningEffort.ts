import { getPlatformSpec } from '@/constants/platformCatalog'
import type { Account } from '@/types'

/**
 * 管理端账号测试的 reasoning effort 选项。
 *
 * 该矩阵与后端 account_test_service_reasoning.go 保持同源：只有账号、映射后的
 * 模型与测试模式都支持时才返回档位；不支持时返回空数组，调用方应隐藏并清空
 * 已选值，避免把无效 effort 发给上游造成 400。
 */

const OPENAI_DEFAULT_TEST_MODEL = 'gpt-5.4'
const GEMINI_DEFAULT_TEST_MODEL = 'gemini-2.0-flash'
const GROK_DEFAULT_TEST_MODEL = 'grok-4.5'
const CLAUDE_DEFAULT_TEST_MODEL = 'claude-sonnet-4-5-20250929'

const CLAUDE_EFFORT_FAMILIES: Array<[string, string[]]> = [
  ['claude-mythos-preview', ['low', 'medium', 'high', 'max']],
  ['claude-mythos-5', ['low', 'medium', 'high', 'xhigh', 'max']],
  ['claude-fable-5', ['low', 'medium', 'high', 'xhigh', 'max']],
  ['claude-sonnet-4-6', ['low', 'medium', 'high', 'max']],
  ['claude-sonnet-5-5', ['low', 'medium', 'high', 'xhigh', 'max']],
  ['claude-sonnet-5', ['low', 'medium', 'high', 'xhigh', 'max']],
  ['claude-opus-4-8', ['low', 'medium', 'high', 'xhigh', 'max']],
  ['claude-opus-4-7', ['low', 'medium', 'high', 'xhigh', 'max']],
  ['claude-opus-4-6', ['low', 'medium', 'high', 'max']],
  ['claude-opus-4-5', ['low', 'medium', 'high']],
  ['claude-opus-5-5', ['low', 'medium', 'high', 'xhigh', 'max']],
  ['claude-opus-5', ['low', 'medium', 'high', 'xhigh', 'max']],
  ['claude-haiku-5-5', ['low', 'medium', 'high', 'xhigh', 'max']]
]

export function accountTestReasoningEffortOptions(
  account: Account | null | undefined,
  modelId: string,
  mode: string
): string[] {
  if (!account) return []
  const platform = account.platform
  const profile = getPlatformSpec(platform)?.multi_protocol

  // 国产多协议供应商按入站协议分流，各原生端点对 effort 的支持不一致，不能安全透传。
  if (profile && profile.routing !== 'by_model') return []

  switch (platform) {
    case 'openai':
      return openAIEffortOptions(account, modelId, mode)
    case 'gemini':
      return geminiEffortOptions(account, modelId, mode)
    case 'grok':
      return grokEffortOptions(account, modelId, mode)
    case 'antigravity':
      // Antigravity 测试复用网关固定探测请求，prompt/effort 不会原样带入。
      return []
  }

  // 按模型分流的聚合平台可能落到 Anthropic/Responses/Chat 三种协议，无法统一保证。
  if (profile && profile.routing === 'by_model') return []
  // TypeSafe 由网关自行选择协议，测试请求不携带 effort。
  if (platform === 'typesafe') return []
  if (platform === 'anthropic') return claudeEffortOptions(account, modelId)
  return []
}

function openAIEffortOptions(account: Account, modelId: string, mode: string): string[] {
  // compact 是独立的原生压缩探测，不携带推理档位与题目。
  if (normalizeMode(mode) === 'compact') return []
  const requested = modelId.trim() || OPENAI_DEFAULT_TEST_MODEL
  const model = mappedModel(account, requested)
  if (isOpenAIImageModel(model) || !supportsOpenAIEffort(model)) return []

  const options = ['minimal', 'low', 'medium', 'high', 'xhigh']
  if (supportsOpenAIEffortMax(model)) options.push('max')
  // GPT-6 Sol/Luna 额外允许 none（显式关闭推理）。
  if (isGPT6SolOrLuna(model)) options.unshift('none')
  // 镜像后端 openai.ValidateGPT61SolReasoningEffort：gpt-6.1-sol 禁用 minimal，
  // 用同一套别名归一化判断，避免只写死一个拼写而漏掉带后缀/路径的别名。
  return isGPT61Sol(model) ? options.filter((option) => option !== 'minimal') : options
}

function geminiEffortOptions(account: Account, modelId: string, mode: string): string[] {
  if (isMediaMode(mode)) return []
  let model = modelId.trim() || GEMINI_DEFAULT_TEST_MODEL
  // 与 testGeminiAccountConnection 一致：仅静态凭据账号应用精确模型映射。
  if (account.type === 'apikey' || account.type === 'service_account') {
    model = mappedModel(account, model)
  }
  if (isGeminiImageModel(model) || !supportsGeminiThinkingLevel(model)) return []
  return ['minimal', 'low', 'medium', 'high']
}

function grokEffortOptions(account: Account, modelId: string, mode: string): string[] {
  const normalizedMode = normalizeMode(mode)
  if (normalizedMode !== '' && normalizedMode !== 'default' && normalizedMode !== 'text') {
    return []
  }
  let model = modelId.trim() || GROK_DEFAULT_TEST_MODEL
  const mapped = mappedModel(account, model)
  if (mapped) model = mapped
  if (!grokSupportsEffort(model)) return []

  const options = ['none', 'low', 'medium', 'high']
  if (grokSupportsXHighEffort(model)) options.push('xhigh')
  return options
}

function claudeEffortOptions(account: Account, modelId: string): string[] {
  // Bedrock invoke 只对部分模型保留 output_config.effort，无法稳定保证。
  if (account.type === 'bedrock') return []
  let model = modelId.trim() || CLAUDE_DEFAULT_TEST_MODEL
  if (account.type === 'apikey') model = mappedModel(account, model)
  return claudeEffortLevels(model)
}

function claudeEffortLevels(model: string): string[] {
  const id = normalizeClaudeModelId(model)
  const family = CLAUDE_EFFORT_FAMILIES.find(
    ([name]) => id === name || id.startsWith(`${name}-`)
  )
  return family ? [...family[1]] : []
}

function normalizeClaudeModelId(model: string): string {
  let id = model.trim().toLowerCase()
  id = id.replace(/^models\//, '')
  const slash = id.indexOf('/')
  if (slash >= 0) id = id.slice(slash + 1).replace(/^models\//, '')
  for (const prefix of ['us.', 'eu.', 'apac.', 'jp.', 'au.', 'us-gov.', 'global.']) {
    if (id.startsWith(prefix)) {
      id = id.slice(prefix.length)
      break
    }
  }
  id = id.replace(/^anthropic\./, '').replace(/-thinking$/, '')
  if (id === 'claude-opus-5.5') id = 'claude-opus-5-5'
  if (id === 'claude-sonnet-5.5') id = 'claude-sonnet-5-5'
  if (id === 'claude-haiku-5.5') id = 'claude-haiku-5-5'
  if (id === 'claude-sonnet-4-5-20250929') id = 'claude-sonnet-4-5'
  if (id === 'claude-opus-4-5-20251101') id = 'claude-opus-4-5'
  if (id === 'claude-haiku-4-5-20251001') id = 'claude-haiku-4-5'
  // 去掉末尾 -YYYYMMDD 日期后缀。
  id = id.replace(/-\d{8}$/, '')
  return id
}

function supportsOpenAIEffort(model: string): boolean {
  const canonical = canonicalizeOpenAIModel(model)
  if (canonical.startsWith('gpt-')) {
    const major = /^(\d+)/.exec(canonical.slice(4))
    if (major && Number(major[1]) >= 5) return true
  }
  return ['o1', 'o3', 'o4'].some(
    (prefix) => canonical === prefix || canonical.startsWith(`${prefix}-`)
  )
}

function supportsOpenAIEffortMax(model: string): boolean {
  const canonical = canonicalizeOpenAIModel(model)
  if (isGPT6Family(canonical) || canonical === 'gpt-5.6' || canonical.startsWith('gpt-5.6-')) {
    return true
  }
  const normalized = lastModelSegment(model).toLowerCase().replace(/_/g, '-')
  return (
    normalized.startsWith('deepseek-v4') ||
    normalized.startsWith('deepseek-flash') ||
    normalized.startsWith('glm-') ||
    normalized.startsWith('kimi-') ||
    normalized.startsWith('moonshot-') ||
    normalized === 'k3' ||
    normalized.startsWith('k3-')
  )
}

function isGPT6Family(canonical: string): boolean {
  return (
    canonical === 'gpt-6' ||
    canonical.startsWith('gpt-6-astra') ||
    isGPT6SolOrLuna(canonical) ||
    canonical === 'gpt-6.1-sol' ||
    canonical.startsWith('gpt-6.1-sol-')
  )
}

function isGPT6SolOrLuna(model: string): boolean {
  const canonical = canonicalizeOpenAIModel(model)
  for (const base of ['gpt-6-sol', 'gpt-6-luna']) {
    if (canonical === base) return true
    if (!canonical.startsWith(`${base}-`)) continue
    const suffix = canonical.slice(base.length + 1)
    if (['none', 'low', 'medium', 'high', 'xhigh', 'max', 'openai-compact'].includes(suffix)) {
      return true
    }
  }
  return false
}

// 镜像后端 openai.IsGPT61SolModelSpelling：别名归一化后匹配基名或 effort/compact 后缀。
function isGPT61Sol(model: string): boolean {
  const canonical = canonicalizeOpenAIModel(model)
  const base = 'gpt-6.1-sol'
  if (canonical === base) return true
  if (!canonical.startsWith(`${base}-`)) return false
  return ['none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max', 'openai-compact'].includes(
    canonical.slice(base.length + 1)
  )
}

function canonicalizeOpenAIModel(model: string): string {
  let canonical = model.trim()
  const slash = canonical.lastIndexOf('/')
  if (slash >= 0) canonical = canonical.slice(slash + 1).trim()
  canonical = canonical.toLowerCase()
  if (!canonical) return ''
  canonical = canonical.replace(/_/g, '-').replace(/\s+/g, '-')
  while (canonical.includes('--')) canonical = canonical.replace(/--/g, '-')
  if (canonical.startsWith('gpt5')) canonical = `gpt-5${canonical.slice(4)}`
  if (!canonical.startsWith('gpt-') && !canonical.includes('codex')) return ''
  return canonical
    .replace(/gpt-5\.4mini/g, 'gpt-5.4-mini')
    .replace(/gpt-5\.4nano/g, 'gpt-5.4-nano')
    .replace(/gpt-5\.3-codexspark/g, 'gpt-5.3-codex-spark')
    .replace(/gpt-5\.3codexspark/g, 'gpt-5.3-codex-spark')
    .replace(/gpt-5\.3codex/g, 'gpt-5.3-codex')
}

function supportsGeminiThinkingLevel(model: string): boolean {
  let id = model.trim().toLowerCase().replace(/^models\//, '')
  const slash = id.lastIndexOf('/')
  if (slash >= 0) id = id.slice(slash + 1)
  if (!id.startsWith('gemini-')) return false
  const major = /^(\d+)/.exec(id.slice('gemini-'.length))
  return !!major && Number(major[1]) >= 3
}

function grokSupportsEffort(model: string): boolean {
  const id = stripGrokProviderPrefix(model).toLowerCase()
  return [
    'grok-4.5',
    'grok-4.5-latest',
    'grok-4.6',
    'grok-4.6-latest',
    'grok-4.7',
    'grok-4.7-latest',
    'grok-4.3',
    'grok-4.3-latest',
    'grok-3-mini',
    'grok-3-mini-fast',
    'grok-4.20-0309-reasoning',
    'grok-4.20-reasoning',
    'grok-4.20-multi-agent-0309'
  ].includes(id)
}

function grokSupportsXHighEffort(model: string): boolean {
  const id = stripGrokProviderPrefix(model).toLowerCase()
  return ['grok-4.7', 'grok-4.7-latest', 'grok-4.6', 'grok-4.6-latest'].includes(id)
}

function stripGrokProviderPrefix(model: string): string {
  const trimmed = model.trim()
  const lower = trimmed.toLowerCase()
  for (const prefix of ['xai/', 'x-ai/', 'grok/']) {
    if (lower.startsWith(prefix)) return trimmed.slice(prefix.length).trim()
  }
  return trimmed
}

function isOpenAIImageModel(model: string): boolean {
  return model.trim().toLowerCase().startsWith('gpt-image-')
}

function isGeminiImageModel(model: string): boolean {
  const id = model.trim().toLowerCase().replace(/^models\//, '')
  return id.startsWith('gemini-') && id.includes('-image')
}

function isMediaMode(mode: string): boolean {
  return ['image', 'video', 'tts', 'stt', 'realtime'].includes(normalizeMode(mode))
}

function normalizeMode(mode: string): string {
  return (mode || '').trim().toLowerCase()
}

function lastModelSegment(model: string): string {
  const parts = model.trim().split('/')
  return parts[parts.length - 1]?.trim() || ''
}

/** 与后端 Account.GetMappedModel 对齐：精确匹配优先，其次最长通配符匹配。 */
function mappedModel(account: Account, requested: string): string {
  const credentials = account.credentials as Record<string, unknown> | undefined
  const rawMapping = credentials?.model_mapping
  if (!rawMapping || typeof rawMapping !== 'object') return requested
  const mapping = rawMapping as Record<string, unknown>
  const asTarget = (value: unknown) => (typeof value === 'string' ? value.trim() : '')

  if (typeof mapping[requested] === 'string') {
    return asTarget(mapping[requested]) || requested
  }

  let bestKey = ''
  let bestTarget = ''
  for (const [key, value] of Object.entries(mapping)) {
    if (!key.includes('*')) continue
    const pattern = `^${key.split('*').map(escapeRegExp).join('.*')}$`
    if (new RegExp(pattern).test(requested) && key.length > bestKey.length) {
      bestKey = key
      bestTarget = asTarget(value)
    }
  }
  return bestTarget || requested
}

function escapeRegExp(value: string): string {
  return value.replace(/[.+?^${}()|[\]\\]/g, '\\$&')
}
