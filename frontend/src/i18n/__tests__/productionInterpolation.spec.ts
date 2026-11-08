// @vitest-environment node
import { describe, expect, it } from 'vitest'
import { build, loadConfigFromFile } from 'vite'
import { resolve } from 'node:path'

// Vitest 的 Node 条件可能载入含编译器的 core-base，必须额外验证浏览器生产构建。
describe('生产构建翻译插值', () => {
  it('真实 Vite 配置打包后仍替换占位符，且不依赖 unsafe-eval', async () => {
    const entry = 'virtual:i18n-production-check'
    const loaded = await loadConfigFromFile({ command: 'build', mode: 'production' }, resolve(process.cwd(), 'vite.config.ts'))
    if (!loaded) throw new Error('缺少生产构建配置')
    // 原始字符串语言包不能搭配 runtime-only；即使新版依赖恰好能编译也保留此约束。
    expect(loaded.config.resolve?.alias).toMatchObject({
      'vue-i18n': 'vue-i18n/dist/vue-i18n.esm-bundler.js'
    })
    expect(loaded.config.define?.__INTLIFY_JIT_COMPILATION__).toBe(true)
    const result = await build({
      configFile: false,
      root: process.cwd(),
      resolve: loaded.config.resolve,
      define: loaded.config.define,
      plugins: [{
        name: 'i18n-production-check',
        resolveId: id => id === entry ? '\0' + entry : undefined,
        load: id => id === '\0' + entry ? `
          import { createI18n } from 'vue-i18n'
          import zh from '${resolve(process.cwd(), 'src/i18n/locales/zh/index.ts')}'
          import en from '${resolve(process.cwd(), 'src/i18n/locales/en/index.ts')}'
          const i18n = createI18n({ legacy: false, locale: 'zh', messages: { zh, en } })
          export function translate(locale, key, params) {
            i18n.global.locale.value = locale
            return i18n.global.t(key, params)
          }
        ` : undefined
      }],
      logLevel: 'silent',
      build: {
        write: false, minify: true,
        rollupOptions: { input: entry, preserveEntrySignatures: 'strict', output: { format: 'iife', name: 'I18nCheck', manualChunks: undefined, inlineDynamicImports: true } }
      }
    })
    const output = Array.isArray(result) ? result[0] : result
    if (!('output' in output)) throw new Error('预期 Vite 返回内存构建产物')
    const chunk = output.output.find(item => item.type === 'chunk')
    if (!chunk) throw new Error('缺少生产构建入口')
    const { runInNewContext } = await import('node:vm')
    // 在禁止动态代码执行的浏览器式沙箱中运行生产产物。
    const sandbox = { console, I18nCheck: undefined as unknown as { translate: (locale: string, key: string, params: Record<string, string>) => string } }
    runInNewContext(chunk.code, sandbox, { contextCodeGeneration: { strings: false, wasm: false } })
    expect(sandbox.I18nCheck.translate('zh', 'admin.accounts.usingModel', { model: 'gpt-5' })).toBe('使用模型：gpt-5')
    expect(sandbox.I18nCheck.translate('zh', 'admin.accounts.usageWindow.estimatedTotalCost', { cost: '12.34' })).toBe('预计总费用 $12.34')
    expect(sandbox.I18nCheck.translate('en', 'admin.accounts.usingModel', { model: 'gpt-5' })).toBe('Using model: gpt-5')
    expect(sandbox.I18nCheck.translate('en', 'admin.accounts.usageWindow.estimatedTotalCost', { cost: '0.00' })).toBe('Est. total $0.00')
  }, 30000)
})
