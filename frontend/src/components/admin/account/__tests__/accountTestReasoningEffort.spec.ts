import { describe, expect, it } from 'vitest'
import { accountTestReasoningEffortOptions } from '../accountTestReasoningEffort'

function account(overrides: Record<string, unknown>) {
  return {
    id: 1,
    name: 'test',
    platform: 'openai',
    type: 'apikey',
    status: 'active',
    credentials: {},
    ...overrides
  } as any
}

describe('账号测试 reasoning effort 选项矩阵', () => {
  it('OpenAI 按模型能力返回档位，非推理/图片/compact 隐藏', () => {
    const openai = account({ platform: 'openai' })

    expect(accountTestReasoningEffortOptions(openai, 'gpt-5.4', 'default')).toEqual([
      'minimal',
      'low',
      'medium',
      'high',
      'xhigh'
    ])
    expect(accountTestReasoningEffortOptions(openai, 'gpt-5.6', 'default')).toContain('max')
    expect(accountTestReasoningEffortOptions(openai, 'gpt-6-sol', 'default')[0]).toBe('none')
    expect(accountTestReasoningEffortOptions(openai, 'gpt-6-sol', 'default')).toContain('max')
    expect(accountTestReasoningEffortOptions(openai, 'gpt-4o', 'default')).toEqual([])
    expect(accountTestReasoningEffortOptions(openai, 'gpt-image-2', 'default')).toEqual([])
    expect(accountTestReasoningEffortOptions(openai, 'gpt-5.6', 'compact')).toEqual([])
  })

  it('GPT-6.1 Sol 与后端校验一致：禁用 minimal（含别名/后缀拼写）', () => {
    const openai = account({ platform: 'openai' })
    expect(accountTestReasoningEffortOptions(openai, 'gpt-6.1-sol', 'default')).toEqual([
      'low',
      'medium',
      'high',
      'xhigh',
      'max'
    ])
    for (const alias of ['openai/gpt-6.1-sol_minimal', 'gpt-6.1-sol-low']) {
      expect(accountTestReasoningEffortOptions(openai, alias, 'default')).not.toContain('minimal')
    }
    const mapped = account({
      platform: 'openai',
      credentials: { model_mapping: { 'my-sol': 'gpt-6.1-sol' } }
    })
    expect(accountTestReasoningEffortOptions(mapped, 'my-sol', 'default')).not.toContain('minimal')
  })

  it('OpenAI 使用账号映射后的模型判断能力', () => {
    const openai = account({
      platform: 'openai',
      credentials: { model_mapping: { 'my-codex': 'gpt-6-sol' } }
    })
    expect(accountTestReasoningEffortOptions(openai, 'my-codex', 'default')[0]).toBe('none')
  })

  it('Gemini 仅 3 及以上文本模型开放 thinkingLevel', () => {
    const gemini = account({ platform: 'gemini' })
    expect(accountTestReasoningEffortOptions(gemini, 'gemini-3.1-pro-preview', 'default')).toEqual([
      'minimal',
      'low',
      'medium',
      'high'
    ])
    expect(accountTestReasoningEffortOptions(gemini, 'gemini-2.5-flash', 'default')).toEqual([])
    expect(accountTestReasoningEffortOptions(gemini, 'gemini-3.1-flash-image', 'default')).toEqual([])
  })

  it('Grok 文本模型返回 none/low/medium/high，4.6/4.7 追加 xhigh', () => {
    const grok = account({ platform: 'grok' })
    expect(accountTestReasoningEffortOptions(grok, 'grok-4.5', 'text')).toEqual([
      'none',
      'low',
      'medium',
      'high'
    ])
    expect(accountTestReasoningEffortOptions(grok, 'grok-4.6', 'text')).toContain('xhigh')
    expect(accountTestReasoningEffortOptions(grok, 'grok-4.7-latest', 'default')).toContain('xhigh')
    expect(accountTestReasoningEffortOptions(grok, 'grok-4.6', 'image')).toEqual([])
  })

  it('Claude 按模型目录返回档位，Bedrock 隐藏', () => {
    const claude = account({ platform: 'anthropic', type: 'apikey' })
    expect(accountTestReasoningEffortOptions(claude, 'claude-opus-4-7', 'default')).toEqual([
      'low',
      'medium',
      'high',
      'xhigh',
      'max'
    ])
    expect(accountTestReasoningEffortOptions(claude, 'claude-opus-4-5', 'default')).toEqual([
      'low',
      'medium',
      'high'
    ])
    expect(accountTestReasoningEffortOptions(claude, 'claude-3-5-sonnet', 'default')).toEqual([])
    expect(
      accountTestReasoningEffortOptions(
        account({ platform: 'anthropic', type: 'bedrock' }),
        'claude-opus-4-7',
        'default'
      )
    ).toEqual([])
  })

  it('Antigravity、CN 供应商、按模型分流聚合平台与 TypeSafe 均隐藏', () => {
    expect(
      accountTestReasoningEffortOptions(account({ platform: 'antigravity' }), 'claude-opus-4-7', 'default')
    ).toEqual([])
    expect(
      accountTestReasoningEffortOptions(account({ platform: 'zhipu' }), 'glm-4.7', 'default')
    ).toEqual([])
    expect(
      accountTestReasoningEffortOptions(account({ platform: 'opencode_go' }), 'gpt-5.4', 'default')
    ).toEqual([])
    expect(
      accountTestReasoningEffortOptions(account({ platform: 'typesafe' }), 'claude-opus-4-7', 'default')
    ).toEqual([])
  })
})
