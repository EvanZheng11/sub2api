import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

import AccountGroupsCell from '../AccountGroupsCell.vue'
import type { Group } from '@/types'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const groups = Array.from({ length: 6 }, (_, i) => ({
  id: i + 1,
  name: `group-${i + 1}`,
}) as unknown as Group)

let wrapper: ReturnType<typeof mount> | undefined
let sandbox: HTMLDivElement | undefined

const mountCell = async () => {
  wrapper = mount(AccountGroupsCell, {
    props: { groups, maxDisplay: 4 },
    attachTo: document.body,
    global: { stubs: { GroupBadge: true } },
  })
  await wrapper.get('button').trigger('click')
  await nextTick()
  return wrapper
}

// popover 被 Teleport 到 body，用固定定位的遮罩层判断展开状态。
const isOpen = () => !!document.body.querySelector('.fixed.inset-0')

const addOutsideElement = <T extends HTMLElement>(element: T): T => {
  sandbox = sandbox ?? document.body.appendChild(document.createElement('div'))
  sandbox.appendChild(element)
  return element
}

const pressOutside = (element: Element) => {
  element.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }))
}

afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
  sandbox?.remove()
  sandbox = undefined
  document.body.innerHTML = ''
  vi.restoreAllMocks()
})

describe('AccountGroupsCell dismiss on blur', () => {
  it('closes when pressing outside, even if a wrapper stops bubbling', async () => {
    await mountCell()
    expect(isOpen()).toBe(true)

    const outside = addOutsideElement(document.createElement('div'))
    outside.addEventListener('mousedown', (event) => event.stopPropagation())
    pressOutside(outside)
    await nextTick()

    expect(isOpen()).toBe(false)
  })

  it('closes when focus moves outside', async () => {
    await mountCell()
    addOutsideElement(document.createElement('input')).focus()
    await nextTick()
    expect(isOpen()).toBe(false)
  })

  it('closes when the window loses focus', async () => {
    await mountCell()
    window.dispatchEvent(new Event('blur'))
    await nextTick()
    expect(isOpen()).toBe(false)
  })

  it('stays open when pressing inside the popover', async () => {
    await mountCell()
    const inside = document.body.querySelectorAll<HTMLElement>('.fixed.z-50')[0]
    expect(inside).toBeTruthy()

    pressOutside(inside)
    await nextTick()

    expect(isOpen()).toBe(true)
  })
})
