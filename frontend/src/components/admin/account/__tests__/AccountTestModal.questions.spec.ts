import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'
import AccountTestModal from '../AccountTestModal.vue'
import type { TestQuestion } from '@/api/admin/testQuestions'

const { getAvailableModels, listQuestions, copyToClipboard } = vi.hoisted(() => ({
  getAvailableModels: vi.fn(),
  listQuestions: vi.fn(),
  copyToClipboard: vi.fn()
}))

vi.mock('@/api/admin', () => ({
  adminAPI: {
    accounts: {
      getAvailableModels
    },
    testQuestions: {
      list: listQuestions
    }
  }
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard })
}))

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({
      t: (key: string) => key
    })
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
          if (index < chunks.length) {
            return { done: false, value: chunks[index++] }
          }
          return { done: true, value: undefined }
        })
      })
    }
  } as Response
}

function testQuestion(id: number, type: TestQuestion['type'], prompt: string): TestQuestion {
  return {
    id,
    title: type === 'file' ? 'SVG 题' : '文本题',
    prompt,
    type,
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-02T00:00:00Z'
  }
}

function mountModal(account: Record<string, unknown>) {
  return mount(AccountTestModal, {
    props: {
      show: false,
      account
    } as any,
    global: {
      stubs: {
        BaseDialog: { template: '<div><slot /><slot name="footer" /></div>' },
        Select: { template: '<div class="select-stub"></div>' },
        TextArea: {
          props: ['modelValue'],
          emits: ['update:modelValue'],
          template:
            '<textarea class="textarea-stub" :value="modelValue" @input="$emit(\'update:modelValue\', $event.target.value)" />'
        },
        Icon: true
      }
    }
  })
}

describe('AccountTestModal 题库与文件结果', () => {
  beforeEach(() => {
    getAvailableModels.mockResolvedValue([{ id: 'gpt-5.4', display_name: 'GPT-5.4' }])
    listQuestions.mockResolvedValue([
      testQuestion(1, 'text', '请用中文写出三条测试建议。'),
      testQuestion(2, 'file', '请返回一个带动画的 SVG 文件。')
    ])
    copyToClipboard.mockReset()
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

  it('默认题目会填充可编辑提示词，并发送 reasoning_effort', async () => {
    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        { type: 'content', text: 'ok' },
        { type: 'test_complete', success: true }
      ])
    ) as any

    const wrapper = mountModal({
      id: 42,
      name: 'OpenAI OAuth',
      platform: 'openai',
      type: 'oauth',
      status: 'active'
    })
    await wrapper.setProps({ show: true })
    await flushPromises()

    expect((wrapper.vm as any).selectedQuestionId).toBe('1')
    expect((wrapper.vm as any).testPrompt).toBe('请用中文写出三条测试建议。')
    ;(wrapper.vm as any).testPrompt = '请给出部署检查清单'
    ;(wrapper.vm as any).selectedReasoningEffort = 'high'

    await (wrapper.vm as any).startTest()
    await flushPromises()

    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body)).toMatchObject({
      model_id: 'gpt-5.4',
      prompt: '请给出部署检查清单',
      mode: 'default',
      reasoning_effort: 'high'
    })
  })

  it('文件题会提取多代码块中的 SVG，仅用图片预览并可下载，不执行脚本', async () => {
    listQuestions.mockResolvedValue([testQuestion(2, 'file', '请返回一个带动画的 SVG 文件。')])
    const svg = '<svg xmlns="http://www.w3.org/2000/svg"><script>window.__executed = true</script></svg>'
    const reply = `说明文字\n\`\`\`json\n{"ok":true}\n\`\`\`\n\`\`\`svg\n${svg}\n\`\`\``

    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        { type: 'content', text: reply },
        { type: 'test_complete', success: true }
      ])
    ) as any

    const createObjectURL = vi
      .fn()
      .mockReturnValueOnce('blob:svg-preview')
      .mockReturnValueOnce('blob:svg-download')
    Object.defineProperty(URL, 'createObjectURL', {
      configurable: true,
      value: createObjectURL
    })
    const revokeObjectURL = vi.fn()
    Object.defineProperty(URL, 'revokeObjectURL', {
      configurable: true,
      value: revokeObjectURL
    })
    const anchorClick = vi.spyOn(HTMLAnchorElement.prototype, 'click').mockImplementation(() => {})

    const wrapper = mountModal({
      id: 7,
      name: 'OpenAI OAuth',
      platform: 'openai',
      type: 'oauth',
      status: 'active'
    })
    await wrapper.setProps({ show: true })
    await flushPromises()
    await (wrapper.vm as any).startTest()
    await flushPromises()

    const artifact = (wrapper.vm as any).artifact
    expect(artifact).toMatchObject({
      fileName: 'SVG-题.svg',
      mimeType: 'image/svg+xml',
      isSvg: true,
      objectUrl: 'blob:svg-preview'
    })
    expect(artifact.content).toContain('<svg')
    expect(wrapper.find('img[alt="SVG-题.svg"]').attributes('src')).toBe('blob:svg-preview')
    expect(wrapper.html()).not.toContain('<script>')
    expect((window as any).__executed).toBeUndefined()

    ;(wrapper.vm as any).downloadArtifact()
    expect(anchorClick).toHaveBeenCalledTimes(1)
    expect(createObjectURL).toHaveBeenLastCalledWith(expect.any(Blob))
    const downloadBlob = createObjectURL.mock.calls.at(-1)?.[0] as Blob
    expect(downloadBlob.type).toBe('image/svg+xml')

    const pendingFetch = vi.fn((_url: string, init: RequestInit) => {
      return new Promise((_resolve, reject) => {
        init.signal?.addEventListener('abort', () =>
          reject(new DOMException('Aborted', 'AbortError'))
        )
      })
    })
    global.fetch = pendingFetch as any
    const secondRun = (wrapper.vm as any).startTest()
    await nextTick()
    expect((wrapper.vm as any).artifact).toBeNull()
    ;(wrapper.vm as any).cancelTest()
    await secondRun
    expect((wrapper.vm as any).status).toBe('idle')

    ;(wrapper.vm as any).clearArtifact()
    expect(revokeObjectURL).toHaveBeenCalledWith('blob:svg-preview')
  })

  it('图片题在没有生图能力的平台会被明确阻止', async () => {
    getAvailableModels.mockResolvedValue([{ id: 'kimi-k2', display_name: 'Kimi K2' }])
    listQuestions.mockResolvedValue([testQuestion(3, 'image', '生成一张测试图片')])
    global.fetch = vi.fn() as any

    const wrapper = mountModal({
      id: 11,
      name: 'Kimi',
      platform: 'kimi',
      type: 'apikey',
      status: 'active'
    })
    await wrapper.setProps({ show: true })
    await flushPromises()

    expect((wrapper.vm as any).imageQuestionUnsupported).toBe(true)
    expect((wrapper.vm as any).canStartTest).toBe(false)
    expect(wrapper.text()).toContain('admin.accounts.testQuestions.imageUnsupported')
    expect(global.fetch).not.toHaveBeenCalled()
  })

  it('XML 围栏内的 SVG 按根元素识别，提取完整 SVG 并同时提供预览与源码', async () => {
    listQuestions.mockResolvedValue([testQuestion(2, 'file', '请返回 SVG')])
    const svg = '<svg xmlns="http://www.w3.org/2000/svg"><rect width="1" height="1" /></svg>'
    const reply = `下面是文件：\n\`\`\`xml\n<?xml version="1.0"?>\n${svg}\n\`\`\`\n以上。`

    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        { type: 'content', text: reply },
        { type: 'test_complete', success: true }
      ])
    ) as any
    const createObjectURL = vi.fn().mockReturnValue('blob:xml-svg')
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: createObjectURL })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: vi.fn() })

    const wrapper = mountModal({
      id: 21,
      name: 'OpenAI OAuth',
      platform: 'openai',
      type: 'oauth',
      status: 'active'
    })
    await wrapper.setProps({ show: true })
    await flushPromises()
    await (wrapper.vm as any).startTest()
    await flushPromises()

    const artifact = (wrapper.vm as any).artifact
    expect(artifact.isSvg).toBe(true)
    expect(artifact.fileName).toBe('SVG-题.svg')
    expect(artifact.content).toBe(svg)
    expect(artifact.content).not.toContain('下面是文件')
    expect(artifact.content).not.toContain('<?xml')
    expect(wrapper.find('img[alt="SVG-题.svg"]').attributes('src')).toBe('blob:xml-svg')
    expect(wrapper.find('details').exists()).toBe(true)
    expect(wrapper.text()).toContain('admin.accounts.fileArtifact.showSource')
    expect(wrapper.find('details pre').text()).toContain('<svg')
  })

  it('HTML 围栏内嵌 SVG 时整体保留 HTML，不因含 svg 被截成 SVG', async () => {
    listQuestions.mockResolvedValue([testQuestion(2, 'file', '请返回一个 HTML 页面')])
    const svg = '<svg xmlns="http://www.w3.org/2000/svg"><circle r="2" /></svg>'
    const html = `<!DOCTYPE html>\n<html><head><title>测试页</title></head><body><h1>标题</h1><button>按钮</button>${svg}</body></html>`
    const reply = `下面是文件：\n\`\`\`html\n${html}\n\`\`\`\n以上。`

    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        { type: 'content', text: reply },
        { type: 'test_complete', success: true }
      ])
    ) as any
    const createObjectURL = vi.fn().mockReturnValue('blob:html-doc')
    Object.defineProperty(URL, 'createObjectURL', { configurable: true, value: createObjectURL })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: vi.fn() })

    const wrapper = mountModal({
      id: 23,
      name: 'OpenAI OAuth',
      platform: 'openai',
      type: 'oauth',
      status: 'active'
    })
    await wrapper.setProps({ show: true })
    await flushPromises()
    await (wrapper.vm as any).startTest()
    await flushPromises()

    const artifact = (wrapper.vm as any).artifact
    expect(artifact.isSvg).toBe(false)
    expect(artifact.fileName).toBe('SVG-题.html')
    expect(artifact.mimeType).toBe('text/html')
    expect(artifact.content).toBe(html)
    expect(artifact.content).toContain('<h1>标题</h1>')
    expect(createObjectURL).not.toHaveBeenCalled()
  })

  it('HTML 围栏内的 HTML 片段（非 html 根文档）也整体保留，不截成 SVG', async () => {
    listQuestions.mockResolvedValue([testQuestion(2, 'file', '请返回一个 HTML 片段')])
    const svg = '<svg xmlns="http://www.w3.org/2000/svg"><circle r="2" /></svg>'
    const fragment = `<div><button>下载</button>${svg}</div>`
    const reply = `下面是文件：\n\`\`\`html\n${fragment}\n\`\`\`\n以上。`

    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        { type: 'content', text: reply },
        { type: 'test_complete', success: true }
      ])
    ) as any
    Object.defineProperty(URL, 'createObjectURL', {
      configurable: true,
      value: vi.fn().mockReturnValue('blob:html-fragment')
    })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: vi.fn() })

    const wrapper = mountModal({
      id: 24,
      name: 'OpenAI OAuth',
      platform: 'openai',
      type: 'oauth',
      status: 'active'
    })
    await wrapper.setProps({ show: true })
    await flushPromises()
    await (wrapper.vm as any).startTest()
    await flushPromises()

    const artifact = (wrapper.vm as any).artifact
    expect(artifact.isSvg).toBe(false)
    expect(artifact.fileName).toBe('SVG-题.html')
    expect(artifact.mimeType).toBe('text/html')
    expect(artifact.content).toBe(fragment)
    expect(artifact.content).toContain('<button>下载</button>')
  })

  it('JS 围栏内字符串含 <svg> 时保留完整 JS 代码与 .js 扩展名', async () => {
    listQuestions.mockResolvedValue([testQuestion(2, 'file', '请返回一个 JS 文件')])
    const svg = '<svg xmlns="http://www.w3.org/2000/svg"><circle r="2" /></svg>'
    const code = `const icon = '${svg}'\nexport default icon`
    const reply = `下面是文件：\n\`\`\`js\n${code}\n\`\`\`\n以上。`

    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        { type: 'content', text: reply },
        { type: 'test_complete', success: true }
      ])
    ) as any
    Object.defineProperty(URL, 'createObjectURL', {
      configurable: true,
      value: vi.fn().mockReturnValue('blob:js-file')
    })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: vi.fn() })

    const wrapper = mountModal({
      id: 25,
      name: 'OpenAI OAuth',
      platform: 'openai',
      type: 'oauth',
      status: 'active'
    })
    await wrapper.setProps({ show: true })
    await flushPromises()
    await (wrapper.vm as any).startTest()
    await flushPromises()

    const artifact = (wrapper.vm as any).artifact
    expect(artifact.isSvg).toBe(false)
    expect(artifact.fileName).toBe('SVG-题.js')
    expect(artifact.mimeType).toBe('text/javascript')
    expect(artifact.content).toBe(code)
    expect(artifact.content).toContain('export default icon')
  })

  it('原始回复夹带说明文字时只提取完整 SVG', async () => {
    listQuestions.mockResolvedValue([testQuestion(2, 'file', '请返回 SVG')])
    const svg = '<svg xmlns="http://www.w3.org/2000/svg"><circle r="2" /></svg>'
    const reply = `这是你要的图：\n${svg}\n请查收。`

    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        { type: 'content', text: reply },
        { type: 'test_complete', success: true }
      ])
    ) as any
    Object.defineProperty(URL, 'createObjectURL', {
      configurable: true,
      value: vi.fn().mockReturnValue('blob:raw-svg')
    })
    Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: vi.fn() })

    const wrapper = mountModal({
      id: 22,
      name: 'OpenAI OAuth',
      platform: 'openai',
      type: 'oauth',
      status: 'active'
    })
    await wrapper.setProps({ show: true })
    await flushPromises()
    await (wrapper.vm as any).startTest()
    await flushPromises()

    expect((wrapper.vm as any).artifact.content).toBe(svg)
  })

  it('选择题库题目时 OpenAI 从 compact 复位到 default，避免题目被截走', async () => {
    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        { type: 'content', text: 'ok' },
        { type: 'test_complete', success: true }
      ])
    ) as any

    const wrapper = mountModal({
      id: 31,
      name: 'OpenAI OAuth',
      platform: 'openai',
      type: 'oauth',
      status: 'active'
    })
    await wrapper.setProps({ show: true })
    await flushPromises()

    ;(wrapper.vm as any).testMode = 'compact'
    ;(wrapper.vm as any).selectedQuestionId = '2'
    await nextTick()

    expect((wrapper.vm as any).testMode).toBe('default')
    await (wrapper.vm as any).startTest()
    await flushPromises()

    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body).mode).toBe('default')
  })

  it('模型切换会清空失效档位，发送前二次校验也会丢弃无效 effort', async () => {
    getAvailableModels.mockResolvedValue([
      { id: 'gpt-5.6', display_name: 'GPT-5.6' },
      { id: 'gpt-4o', display_name: 'GPT-4o' }
    ])
    global.fetch = vi.fn().mockResolvedValue(
      createStreamResponse([
        { type: 'content', text: 'ok' },
        { type: 'test_complete', success: true }
      ])
    ) as any

    const wrapper = mountModal({
      id: 32,
      name: 'OpenAI OAuth',
      platform: 'openai',
      type: 'oauth',
      status: 'active'
    })
    await wrapper.setProps({ show: true })
    await flushPromises()

    ;(wrapper.vm as any).selectedModelId = 'gpt-5.6'
    ;(wrapper.vm as any).selectedReasoningEffort = 'max'
    await nextTick()
    expect((wrapper.vm as any).reasoningEffortOptions.map((o: any) => o.value)).toContain('max')

    ;(wrapper.vm as any).selectedModelId = 'gpt-4o'
    await nextTick()
    expect((wrapper.vm as any).showReasoningEffort).toBe(false)
    expect((wrapper.vm as any).selectedReasoningEffort).toBe('')

    // 绕过 watch 直接注入无效档位，验证发送前兜底。
    ;(wrapper.vm as any).selectedModelId = 'gpt-5.4'
    await nextTick()
    ;(wrapper.vm as any).selectedReasoningEffort = 'max'
    await (wrapper.vm as any).startTest()
    await flushPromises()

    const [, request] = (global.fetch as any).mock.calls[0]
    expect(JSON.parse(request.body)).not.toHaveProperty('reasoning_effort')
  })

  it('重开另一账号时丢弃旧账号的模型异步响应', async () => {
    function deferred<T>() {
      let resolve!: (value: T) => void
      const promise = new Promise<T>((r) => {
        resolve = r
      })
      return { promise, resolve }
    }
    const first = deferred<Array<{ id: string; display_name: string }>>()
    const second = deferred<Array<{ id: string; display_name: string }>>()
    getAvailableModels.mockImplementation((id: number) =>
      id === 101 ? first.promise : second.promise
    )
    listQuestions.mockResolvedValue([testQuestion(1, 'text', '文本题')])

    const wrapper = mountModal({
      id: 101,
      name: '账号 A',
      platform: 'openai',
      type: 'oauth',
      status: 'active'
    })
    await wrapper.setProps({ show: true })
    await nextTick()
    await wrapper.setProps({
      show: false,
      account: { id: 202, name: '账号 B', platform: 'openai', type: 'oauth', status: 'active' }
    } as any)
    await wrapper.setProps({ show: true })
    await nextTick()

    second.resolve([{ id: 'new-model', display_name: 'New' }])
    await flushPromises()
    first.resolve([{ id: 'old-model', display_name: 'Old' }])
    await flushPromises()

    expect((wrapper.vm as any).availableModels.map((m: any) => m.id)).toEqual(['new-model'])
    expect((wrapper.vm as any).selectedModelId).toBe('new-model')
  })
})
