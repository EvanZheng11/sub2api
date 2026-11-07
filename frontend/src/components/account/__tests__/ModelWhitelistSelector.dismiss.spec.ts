import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key })
  }
})

vi.mock('@/stores/app', () => ({
  useAppStore: () => ({
    showError: vi.fn(),
    showSuccess: vi.fn(),
    showInfo: vi.fn(),
    showWarning: vi.fn()
  })
}))

vi.mock('@/api/admin/accounts', () => ({
  accountsAPI: {
    syncUpstreamModels: vi.fn(),
    syncUpstreamModelsPreview: vi.fn()
  }
}))

vi.mock('@/composables/useClipboard', () => ({
  useClipboard: () => ({ copyToClipboard: vi.fn() })
}))

import ModelWhitelistSelector from '../ModelWhitelistSelector.vue'

let wrapper: ReturnType<typeof mount> | undefined

const mountSelector = () => {
  wrapper = mount(ModelWhitelistSelector, {
    props: { modelValue: [], platform: 'openai' },
    attachTo: document.body,
    global: { stubs: { ModelIcon: true } }
  })
  return wrapper
}

const openDropdown = async () => {
  const selector = mountSelector()
  await selector.get('div.cursor-pointer').trigger('click')
  await nextTick()
  return selector
}

const isOpen = () => wrapper?.findAll('[data-testid="model-option"]').length ? true : false

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  document.body.innerHTML = ''
})

describe('ModelWhitelistSelector 失焦收起', () => {
  it('按下下拉以外的区域时收起', async () => {
    await openDropdown()
    expect(isOpen()).toBe(true)

    const outside = document.body.appendChild(document.createElement('div'))
    outside.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }))
    await nextTick()

    expect(isOpen()).toBe(false)
  })

  it('外部容器的 stopPropagation 不影响收起', async () => {
    await openDropdown()

    const outside = document.body.appendChild(document.createElement('div'))
    outside.addEventListener('mousedown', (event) => event.stopPropagation())
    outside.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }))
    await nextTick()

    expect(isOpen()).toBe(false)
  })

  it('焦点移到其他输入框时收起', async () => {
    await openDropdown()
    expect(isOpen()).toBe(true)

    const input = document.body.appendChild(document.createElement('input'))
    input.focus()
    await nextTick()

    expect(isOpen()).toBe(false)
  })

  it('窗口失焦时收起', async () => {
    await openDropdown()
    expect(isOpen()).toBe(true)

    window.dispatchEvent(new Event('blur'))
    await nextTick()

    expect(isOpen()).toBe(false)
  })

  it('在下拉内部搜索时保持展开', async () => {
    const selector = await openDropdown()
    const search = selector.get('input[type="text"]')
    await search.trigger('mousedown')
    ;(search.element as HTMLInputElement).focus()
    await nextTick()

    expect(isOpen()).toBe(true)
  })
})
