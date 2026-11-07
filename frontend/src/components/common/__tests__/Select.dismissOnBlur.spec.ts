import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { nextTick } from 'vue'

import Select from '../Select.vue'

vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  }
})

const options = [
  { value: 'alpha', label: 'Alpha' },
  { value: 'beta', label: 'Beta' },
]

let wrapper: ReturnType<typeof mount> | undefined
let sandbox: HTMLDivElement | undefined

const mountOpenSelect = async (props: Record<string, unknown> = {}) => {
  wrapper = mount(Select, {
    props: { modelValue: null, options, ...props },
    attachTo: document.body,
  })
  await wrapper.get('button').trigger('click')
  await nextTick()
  return wrapper
}

const dropdownPortal = () => document.body.querySelector<HTMLElement>('.select-dropdown-portal')

// Transition 在 jsdom 中不会触发 transitionend，用触发器的 aria-expanded 判断展开状态。
const isOpen = () => wrapper?.get('button').attributes('aria-expanded') === 'true'

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

describe('Select dismiss on blur', () => {
  it('closes when pressing outside', async () => {
    await mountOpenSelect()
    expect(isOpen()).toBe(true)

    pressOutside(addOutsideElement(document.createElement('div')))
    await nextTick()

    expect(isOpen()).toBe(false)
  })

  it('closes when the outside press is stopped from bubbling by a wrapper', async () => {
    await mountOpenSelect()

    const outside = addOutsideElement(document.createElement('div'))
    outside.addEventListener('mousedown', (event) => event.stopPropagation())
    pressOutside(outside)
    await nextTick()

    expect(isOpen()).toBe(false)
  })

  it('closes when focus moves to another field', async () => {
    await mountOpenSelect()
    expect(isOpen()).toBe(true)

    addOutsideElement(document.createElement('input')).focus()
    await nextTick()

    expect(isOpen()).toBe(false)
  })

  it('closes when the window loses focus', async () => {
    await mountOpenSelect()
    expect(isOpen()).toBe(true)

    window.dispatchEvent(new Event('blur'))
    await nextTick()

    expect(isOpen()).toBe(false)
  })

  it('stays open when focus moves into the search box inside the dropdown', async () => {
    await mountOpenSelect({ searchable: true })
    const search = dropdownPortal()?.querySelector<HTMLInputElement>('.select-search-input')
    expect(search).not.toBeNull()

    search!.focus()
    await nextTick()

    expect(isOpen()).toBe(true)
  })

  it('keeps toggling open and closed when pressing the trigger', async () => {
    const select = await mountOpenSelect()
    const trigger = select.get('button')

    // 先收起，再从收起状态验证“按下触发器不触发外部收起”
    pressOutside(trigger.element)
    await trigger.trigger('click')
    await nextTick()
    expect(isOpen()).toBe(false)

    pressOutside(trigger.element)
    await trigger.trigger('click')
    await nextTick()
    expect(isOpen()).toBe(true)

    pressOutside(trigger.element)
    await trigger.trigger('click')
    await nextTick()
    expect(isOpen()).toBe(false)
  })

  it('selects an option pressed inside the dropdown', async () => {
    const select = await mountOpenSelect()
    const option = dropdownPortal()?.querySelectorAll<HTMLElement>('.select-option')[1]
    expect(option).toBeTruthy()

    option!.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }))
    option!.click()
    await nextTick()

    expect(select.emitted('update:modelValue')).toEqual([['beta']])
    expect(isOpen()).toBe(false)
  })
})
