import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { i18n, loadLocaleMessages } from '../index'
import UsageProgressBar from '@/components/account/UsageProgressBar.vue'

// 使用应用自身的懒加载语言包和真实 vue-i18n，不能用返回 key 的 mock 掩盖插值故障。
afterEach(() => {
  vi.unstubAllGlobals()
})

describe('应用翻译插值', () => {
  it.each([
    ['zh', '使用模型：gpt-5', '预计总费用 $12.34'],
    ['en', 'Using model: gpt-5', 'Est. total $12.34']
  ] as const)('%s 替换模型名及费用，并保留美元符号', async (locale, modelText, costText) => {
    await loadLocaleMessages(locale)
    i18n.global.locale.value = locale
    const wrapper = mount(defineComponent({
      setup: () => ({ t: i18n.global.t }),
      template: `<div>{{ t('admin.accounts.usingModel', { model: 'gpt-5' }) }}</div>`
    }), { global: { plugins: [i18n] } })
    expect(wrapper.text()).toBe(modelText)
    wrapper.unmount()

    const progress = mount(UsageProgressBar, {
      props: {
        label: '5h', utilization: 25, color: 'indigo', estimatedTotalCost: 12.34,
        windowStats: { requests: 1, tokens: 100, cost: 1.23 }
      },
      global: { plugins: [i18n] }
    })
    expect(progress.get('[data-test="estimated-total-cost"]').text()).toBe(costText)
    await progress.setProps({ estimatedTotalCost: 0 })
    expect(progress.get('[data-test="estimated-total-cost"]').text()).toContain('$0.00')
    progress.unmount()
  })

  it('严格 CSP 下仍可编译消息，不依赖动态代码执行', async () => {
    vi.stubGlobal('Function', new Proxy(Function, {
      apply() { throw new Error('CSP 禁止动态代码执行') },
      construct() { throw new Error('CSP 禁止动态代码执行') }
    }))
    await loadLocaleMessages('zh')
    i18n.global.locale.value = 'zh'
    expect(i18n.global.t('admin.accounts.mappingExists', { model: 'csp-model' }))
      .toBe('模型 csp-model 的映射已存在')
  })
})
