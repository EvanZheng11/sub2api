import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

import ProxiesView from '../ProxiesView.vue'

const { listProxies } = vi.hoisted(() => ({
  listProxies: vi.fn(),
  getAllWithCount: vi.fn().mockResolvedValue([]),
}))

vi.mock('@/api/admin', () => ({
  adminAPI: { proxies: { list: listProxies, getAllWithCount: vi.fn().mockResolvedValue([]) } },
}))
vi.mock('@/stores/app', () => ({ useAppStore: () => ({ showError: vi.fn() }) }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const DataTableStub = {
  props: ['columns', 'data'],
  template: `<div>
    <div v-for="row in data" :key="row.id">
      <slot name="cell-address" :row="row" />
    </div>
  </div>`,
}

let wrapper: ReturnType<typeof mount> | undefined
let sandbox: HTMLDivElement | undefined

const mountView = async () => {
  wrapper = mount(ProxiesView, {
    attachTo: document.body,
    global: {
      stubs: {
        AppLayout: { template: '<div><slot /></div>' },
        TablePageLayout: { template: '<div><slot name="filters" /><slot name="table" /></div>' },
        DataTable: DataTableStub,
        Pagination: true,
        BaseDialog: true,
        ConfirmDialog: true,
        EmptyState: true,
        ImportDataModal: true,
        Select: true,
        ProxyAdBanner: true,
        Icon: true,
        PlatformTypeBadge: true,
      },
    },
  })
  await flushPromises()
  return wrapper
}

// 右键复制菜单渲染在单元格内（.copy-menu-anchor 容器）
const copyMenu = () => document.body.querySelector('.copy-menu-anchor .absolute.z-50')

const addOutsideElement = <T extends HTMLElement>(element: T): T => {
  sandbox = sandbox ?? document.body.appendChild(document.createElement('div'))
  sandbox.appendChild(element)
  return element
}

beforeEach(() => {
  listProxies.mockReset()
  listProxies.mockResolvedValue({
    items: [{ id: 1, host: '1.2.3.4', port: 8080, protocol: 'http', status: 'active', username: null, password: null }],
    total: 1,
    page: 1,
    page_size: 20,
    pages: 1,
  })
})

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  sandbox?.remove()
  sandbox = undefined
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

describe('ProxiesView copy menu dismiss on blur', () => {
  it('closes on outside press even when a wrapper stops bubbling', async () => {
    const view = await mountView()
    const trigger = view.get('button[title="admin.proxies.copyProxyUrl"]')
    await trigger.trigger('contextmenu')
    await nextTick()
    expect(copyMenu()).toBeTruthy()

    const outside = addOutsideElement(document.createElement('div'))
    outside.addEventListener('mousedown', (event) => event.stopPropagation())
    outside.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }))
    await nextTick()

    expect(copyMenu()).toBeFalsy()
  })

  it('closes when focus moves outside', async () => {
    const view = await mountView()
    await view.get('button[title="admin.proxies.copyProxyUrl"]').trigger('contextmenu')
    await nextTick()
    expect(copyMenu()).toBeTruthy()

    addOutsideElement(document.createElement('input')).focus()
    await nextTick()

    expect(copyMenu()).toBeFalsy()
  })

  it('stays open when pressing inside the menu', async () => {
    const view = await mountView()
    await view.get('button[title="admin.proxies.copyProxyUrl"]').trigger('contextmenu')
    await nextTick()
    const menu = copyMenu()
    expect(menu).toBeTruthy()

    menu!.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }))
    await nextTick()

    expect(copyMenu()).toBeTruthy()
  })
})
