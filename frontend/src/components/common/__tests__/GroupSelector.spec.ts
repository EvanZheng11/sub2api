import { mount } from '@vue/test-utils'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import GroupSelector from '../GroupSelector.vue'

const authState = { isSimpleMode: false }

vi.mock('@/stores', () => ({ useAuthStore: () => authState }))
vi.mock('vue-i18n', async () => {
  const actual = await vi.importActual<typeof import('vue-i18n')>('vue-i18n')
  return { ...actual, useI18n: () => ({ t: (key: string) => key }) }
})

const groups = [
  { id: 1, name: 'Basic', platform: 'anthropic', status: 'active' },
  { id: 2, name: 'Composite', platform: 'composite', status: 'active' }
] as any

const crossPlatformGroups = [
  { id: 11, name: 'AnthropicGroup', platform: 'anthropic', status: 'active' },
  { id: 12, name: 'OpenAIGroup', platform: 'openai', status: 'active' },
  { id: 13, name: 'OpenCodeGroup', platform: 'opencode_go', status: 'active' }
] as any

const mountSelector = (modelValue: number[] = []) => mount(GroupSelector, {
  props: { modelValue, groups },
  global: {
    stubs: {
      GroupBadge: { props: ['name', 'platform'], template: '<span>{{ name }} ({{ platform }})</span>' },
      Icon: true
    }
  }
})

const mountCrossPlatformSelector = (modelValue: number[] = []) => mount(GroupSelector, {
  props: { modelValue, groups: crossPlatformGroups, platform: 'openai' } as any,
  global: {
    stubs: {
      GroupBadge: { props: ['name', 'platform'], template: '<span>{{ name }} ({{ platform }})</span>' },
      Icon: true
    }
  }
})

describe('GroupSelector simple-mode binding policy', () => {
  beforeEach(() => { authState.isSimpleMode = false })

  it('hides composite groups in simple mode and preserves basic groups', () => {
    authState.isSimpleMode = true
    const wrapper = mountSelector()
    expect(wrapper.text()).toContain('Basic')
    expect(wrapper.text()).not.toContain('Composite')
  })

  it('keeps composite groups available in advanced mode', () => {
    const wrapper = mountSelector()
    expect(wrapper.text()).toContain('Composite')
  })

  it('任意平台账号可选任意平台分组并显示分组实际平台', () => {
    const wrapper = mountCrossPlatformSelector()
    const checkboxes = wrapper.findAll('input[type="checkbox"]')

    expect(checkboxes).toHaveLength(3)
    expect(wrapper.text()).toContain('AnthropicGroup (anthropic)')
    expect(wrapper.text()).toContain('OpenAIGroup (openai)')
    expect(wrapper.text()).toContain('OpenCodeGroup (opencode_go)')
  })

  it('cleans hidden historical composite IDs while preserving visible selections', () => {
    authState.isSimpleMode = true
    const wrapper = mountSelector([1, 2])
    expect(wrapper.emitted('update:modelValue')).toEqual([[[1]]])
  })
})
