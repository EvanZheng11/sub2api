import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

import UsageFilters from '../UsageFilters.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

vi.mock('@/api/admin', () => ({
  adminAPI: {
    usage: { searchUsers: vi.fn().mockResolvedValue([{ id: 7, email: 'u@test.com' }]), searchApiKeys: vi.fn() },
    groups: { list: vi.fn().mockResolvedValue({ items: [] }) },
    accounts: { list: vi.fn().mockResolvedValue({ items: [] }) },
  },
}))

const defaultFilters = () => ({
  user_id: undefined,
  api_key_id: undefined,
  account_id: undefined,
  model: null,
  request_type: null,
  native_compaction_v2: null,
  billing_type: null,
  billing_mode: null,
  upstream_model_mismatch: null,
  group_id: null,
  start_date: '',
  end_date: '',
})

let wrapper: ReturnType<typeof mount> | undefined
let sandbox: HTMLDivElement | undefined

const mountFilters = async () => {
  wrapper = mount(UsageFilters, {
    props: {
      modelValue: defaultFilters(),
      exporting: false,
      startDate: '2026-05-01',
      endDate: '2026-05-28',
      showActions: false,
      modelOptions: [],
    },
    attachTo: document.body,
    global: { stubs: { Select: true } },
  })
  // 用户搜索框输入关键字后展示下拉（v-if 需要结果或关键字非空）
  const input = wrapper.get('.usage-filter-dropdown input')
  await input.setValue('u')
  await input.trigger('focus')
  await nextTick()
  return wrapper
}

const userDropdown = () => document.body.querySelector('.usage-filter-dropdown .absolute.z-50')

const addOutsideElement = <T extends HTMLElement>(element: T): T => {
  sandbox = sandbox ?? document.body.appendChild(document.createElement('div'))
  sandbox.appendChild(element)
  return element
}

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  sandbox?.remove()
  sandbox = undefined
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

describe('UsageFilters dismiss on blur', () => {
  it('closes the user dropdown when pressing outside, even if a wrapper stops bubbling', async () => {
    await mountFilters()
    expect(userDropdown()).not.toBeNull()

    const outside = addOutsideElement(document.createElement('div'))
    outside.addEventListener('mousedown', (event) => event.stopPropagation())
    outside.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }))
    await nextTick()

    expect(userDropdown()).toBeNull()
  })

  it('closes the user dropdown when focus moves outside', async () => {
    await mountFilters()
    addOutsideElement(document.createElement('input')).focus()
    await nextTick()
    expect(userDropdown()).toBeNull()
  })

  it('closes the user dropdown when the window loses focus', async () => {
    await mountFilters()
    window.dispatchEvent(new Event('blur'))
    await nextTick()
    expect(userDropdown()).toBeNull()
  })

  it('keeps the dropdown open while typing in its own search input', async () => {
    await mountFilters()
    const input = wrapper!.get('.usage-filter-dropdown input')
    await input.trigger('mousedown')
    await input.trigger('focus')
    await nextTick()
    expect(userDropdown()).not.toBeNull()
  })
})
