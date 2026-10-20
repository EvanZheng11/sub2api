import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import TestQuestionTestModal from '../TestQuestionTestModal.vue'

const { listAccounts, listAccountModels, buildTestUrl } = vi.hoisted(() => ({
  listAccounts: vi.fn(),
  listAccountModels: vi.fn(),
  buildTestUrl: vi.fn()
}))

vi.mock('@/api/testCenter', () => ({
  testCenterAPI: {
    listAccounts,
    listAccountModels,
    buildTestUrl
  }
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

function createStreamResponse(events: Array<Record<string, unknown>>) {
  const encoder = new TextEncoder()
  const chunks = events.map((event) => encoder.encode(`data: ${JSON.stringify(event)}\n`))
  let index = 0
  return {
    ok: true,
    body: {
      getReader: () => ({
        read: vi.fn().mockImplementation(async () => {
          if (index < chunks.length) return { done: false, value: chunks[index++] }
          return { done: true, value: undefined }
        })
      })
    }
  } as unknown as Response
}

function mountModal() {
  return mount(TestQuestionTestModal, {
    props: {
      show: false,
      question: {
        id: 9,
        title: '基础连通性',
        prompt: '请返回 ok',
        type: 'text',
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-02T00:00:00Z'
      }
    },
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        Select: {
          props: ['modelValue', 'options'],
          emits: ['update:modelValue'],
          template: '<select class="select-stub" :value="modelValue"><option v-for="o in options" :key="o.value" :value="o.value">{{ o.label }}</option></select>'
        },
        Icon: true
      }
    }
  })
}

describe('TestQuestionTestModal 题库测试', () => {
  beforeEach(() => {
    listAccounts.mockResolvedValue([
      { id: 21, name: '账号A', platform: 'openai', type: 'oauth', status: 'active' }
    ])
    listAccountModels.mockResolvedValue([{ id: 'gpt-5.4', display_name: 'GPT-5.4' }])
    buildTestUrl.mockImplementation((id: number) => `/api/v1/test-center/accounts/${id}/test`)
    Object.defineProperty(globalThis, 'localStorage', {
      value: {
        getItem: vi.fn((key: string) => (key === 'auth_token' ? 'test-token' : null)),
        setItem: vi.fn(),
        removeItem: vi.fn(),
        clear: vi.fn()
      },
      configurable: true
    })
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('打开时加载可用账号与模型，测试时提交题目与思考强度', async () => {
    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        { type: 'content', text: 'ok' },
        { type: 'test_complete', success: true }
      ])
    ) as any

    const wrapper = mountModal()
    await wrapper.setProps({ show: true })
    await flushPromises()

    expect(listAccounts).toHaveBeenCalledTimes(1)
    expect(listAccountModels).toHaveBeenCalledWith(21)
    expect((wrapper.vm as any).selectedAccountId).toBe('21')
    expect((wrapper.vm as any).selectedModelId).toBe('gpt-5.4')

    ;(wrapper.vm as any).selectedReasoningEffort = 'high'
    await (wrapper.vm as any).startTest()
    await flushPromises()

    const [url, request] = (global.fetch as any).mock.calls[0]
    expect(url).toBe('/api/v1/test-center/accounts/21/test')
    const body = JSON.parse(request.body)
    expect(body.test_question_id).toBe(9)
    expect(body.prompt).toBe('请返回 ok')
    expect(body.model_id).toBe('gpt-5.4')
    expect(body.reasoning_effort).toBe('high')
  })
})
