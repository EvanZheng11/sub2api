import { describe, expect, it } from 'vitest'
import { buildTestArtifact, sanitizeTestArtifactBaseName } from '../testArtifact'

describe('buildTestArtifact', () => {
  it('从多个代码块中优先提取 SVG', () => {
    const text = ['```json', '{"a":1}', '```', '```svg', '<svg viewBox="0 0 10 10"></svg>', '```'].join('\n')

    const artifact = buildTestArtifact(text, '题目')

    expect(artifact).not.toBeNull()
    expect(artifact?.fileName).toBe('题目.svg')
    expect(artifact?.mimeType).toBe('image/svg+xml')
    expect(artifact?.isSvg).toBe(true)
    expect(artifact?.content).toBe('<svg viewBox="0 0 10 10"></svg>')
  })

  it('识别 xml 围栏里的 SVG 根文档', () => {
    const artifact = buildTestArtifact('```xml\n<?xml version="1.0"?>\n<svg><rect/></svg>\n```', '题目')

    expect(artifact?.fileName).toBe('题目.svg')
  })

  it('HTML 文档内嵌 SVG 时保留整份 HTML', () => {
    const html = '<html><body><p>说明</p><svg viewBox="0 0 4 4"></svg></body></html>'
    const artifact = buildTestArtifact('```html\n' + html + '\n```', '题目')

    expect(artifact?.fileName).toBe('题目.html')
    expect(artifact?.mimeType).toBe('text/html')
    expect(artifact?.content).toBe(html)
  })

  it('JS 字符串里的 <svg> 不误判为 SVG', () => {
    const artifact = buildTestArtifact('```js\nconst s = "<svg/>"\n```', '题目')

    expect(artifact?.fileName).toBe('题目.js')
  })

  it('无代码块时回退为 txt', () => {
    const artifact = buildTestArtifact('这就是一段普通回答', '题目')

    expect(artifact?.fileName).toBe('题目.txt')
    expect(artifact?.mimeType).toBe('text/plain')
    expect(artifact?.isSvg).toBe(false)
  })

  it('空白输入返回 null', () => {
    expect(buildTestArtifact('   ', '题目')).toBeNull()
  })

  it('代码块内容为空时返回 null', () => {
    expect(buildTestArtifact('```js\n   \n```', '题目')).toBeNull()
  })

  it('未知语言安全回退 txt', () => {
    const artifact = buildTestArtifact('```weirdlang\nsome content\n```', '题目')

    expect(artifact?.fileName).toBe('题目.txt')
  })
})

describe('sanitizeTestArtifactBaseName', () => {
  it('替换非法文件名字符并限制长度', () => {
    expect(sanitizeTestArtifactBaseName('a/b:c*d?e"f<g>h i')).toBe('a-b-c-d-e-f-g-h-i')
    expect(sanitizeTestArtifactBaseName('x'.repeat(120))).toBe('x'.repeat(80))
  })

  it('空值回退到 test-result', () => {
    expect(sanitizeTestArtifactBaseName('   ')).toBe('test-result')
  })
})
