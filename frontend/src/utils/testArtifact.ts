/**
 * 账号测试结果的文件提取工具。
 * 从模型输出中识别可下载的代码文件（SVG/HTML/MD/…），供账号测试弹框与测试历史弹框共用。
 */

const artifactMimeTypes: Record<string, string> = {
  svg: 'image/svg+xml',
  html: 'text/html',
  htm: 'text/html',
  md: 'text/markdown',
  markdown: 'text/markdown',
  json: 'application/json',
  csv: 'text/csv',
  xml: 'application/xml',
  css: 'text/css',
  js: 'text/javascript',
  javascript: 'text/javascript',
  ts: 'text/typescript',
  typescript: 'text/typescript',
  yaml: 'application/yaml',
  yml: 'application/yaml',
  py: 'text/x-python',
  python: 'text/x-python',
  go: 'text/x-go',
  sh: 'text/x-shellscript',
  bash: 'text/x-shellscript',
  txt: 'text/plain',
  text: 'text/plain'
}

export interface TestArtifact {
  fileName: string
  mimeType: string
  content: string
  isSvg: boolean
  objectUrl: string
}

const fenceLanguage = (rawLanguage: string): string =>
  rawLanguage.trim().toLowerCase().split(/\s+/)[0] || ''

// 从任意文本中提取完整的 <svg>...</svg>，避免把前后说明文字写进 SVG 导致预览损坏。
const extractSvgRoot = (raw: string): string => {
  const startMatch = /<svg[\s>]/i.exec(raw)
  if (!startMatch) return ''
  const fromStart = raw.slice(startMatch.index)
  const endMatch = /<\/svg\s*>/i.exec(fromStart)
  if (!endMatch) return ''
  return fromStart.slice(0, endMatch.index + endMatch[0].length).trim()
}

// 去掉前导 XML 声明/注释/doctype（及注释间距），用于判断内容真正的根元素。
const stripProlog = (content: string): string => {
  let body = content.trim()
  for (;;) {
    const next = body
      .replace(/^<\?xml[\s\S]*?\?>/i, '')
      .replace(/^<!--[\s\S]*?-->/i, '')
      .replace(/^<!doctype[^>]*>/i, '')
      .trim()
    if (next === body) return body
    body = next
  }
}

// 根元素是 <svg> 才算 SVG 文档：HTML 文档里内嵌 <svg> 不能被截成 SVG。
const isSvgRootDocument = (content: string): boolean => /^<svg[\s>/]/i.test(stripProlog(content))

// 根元素是 <html>（doctype 已剥除）判定为 HTML 文档，需要整体保留。
const isHtmlRootDocument = (content: string): boolean => /^<html[\s>]/i.test(stripProlog(content))

// 只有 svg/xml/空/txt/text 及未知语言才可能是 SVG；明确写了 html/js/json 等已知类型整体保留。
const isSvgCandidateLanguage = (language: string): boolean =>
  language === '' || language === 'svg' || language === 'xml' || language === 'txt' || language === 'text' || !artifactMimeTypes[language]

const normalizeArtifactLanguage = (rawLanguage: string, content: string): string => {
  const language = fenceLanguage(rawLanguage)
  if (language && !isSvgCandidateLanguage(language)) return language
  if (language === 'svg' || isSvgRootDocument(content)) return 'svg'
  if (isHtmlRootDocument(content)) return 'html'
  if (language) return language
  return 'txt'
}

/** 把题目标题转成安全的文件名主干。 */
export function sanitizeTestArtifactBaseName(raw: string): string {
  return (
    raw
      .trim()
      .replace(/[\\/:*?"<>|\s]+/g, '-')
      .slice(0, 80) || 'test-result'
  )
}

/**
 * 从模型输出中提取可下载的文件；无法提取时返回 null。
 * baseName 用于生成文件名，一般传题目标题。
 */
export function buildTestArtifact(rawText: string, baseName: string): TestArtifact | null {
  const text = rawText.trim()
  if (!text) return null

  const fences: Array<{ language: string; content: string }> = []
  const fencePattern = /```([^\n`]*)\r?\n([\s\S]*?)```/g
  let match: RegExpExecArray | null
  while ((match = fencePattern.exec(text)) !== null) {
    fences.push({ language: match[1], content: match[2].trim() })
  }

  let selected: { language: string; content: string } | null = null
  // 多代码块时优先 SVG；但明确写了 html/js/json 等语言、或整份 HTML 文档（即便内嵌 <svg>）都不算 SVG 块。
  const svgFence = fences.find((fence) => {
    const language = fenceLanguage(fence.language)
    if (fence.content.length === 0 || !isSvgCandidateLanguage(language)) return false
    return (
      language === 'svg' ||
      isSvgRootDocument(fence.content) ||
      (extractSvgRoot(fence.content) !== '' && !isHtmlRootDocument(fence.content))
    )
  })
  if (svgFence) {
    selected = {
      language: 'svg',
      content: extractSvgRoot(svgFence.content) || svgFence.content
    }
  } else if (fences.length > 0) {
    selected = fences[0]
  } else {
    const svg = isHtmlRootDocument(text) ? '' : extractSvgRoot(text)
    selected = svg ? { language: 'svg', content: svg } : { language: 'txt', content: text }
  }

  if (!selected.content) return null

  const language = normalizeArtifactLanguage(selected.language, selected.content)
  // 已知代码/文本类型按原扩展名；未知语言安全回退 txt，不引入二进制类型。
  const extension = artifactMimeTypes[language] ? language : 'txt'
  const mimeType = artifactMimeTypes[extension] || 'text/plain'
  const isSvg = extension === 'svg'

  let objectUrl = ''
  if (isSvg && typeof URL.createObjectURL === 'function') {
    objectUrl = URL.createObjectURL(new Blob([selected.content], { type: mimeType }))
  }

  return {
    fileName: `${sanitizeTestArtifactBaseName(baseName)}.${extension}`,
    mimeType,
    content: selected.content,
    isSvg,
    objectUrl
  }
}

/** 释放 buildTestArtifact 为 SVG 创建的对象 URL。 */
export function revokeTestArtifact(artifact: TestArtifact | null): void {
  if (artifact?.objectUrl && typeof URL.revokeObjectURL === 'function') {
    URL.revokeObjectURL(artifact.objectUrl)
  }
}

/** 触发浏览器下载。 */
export function downloadTestArtifact(artifact: TestArtifact): void {
  if (typeof URL.createObjectURL !== 'function') return
  const url = URL.createObjectURL(new Blob([artifact.content], { type: artifact.mimeType }))
  const link = document.createElement('a')
  link.href = url
  link.download = artifact.fileName
  link.rel = 'noopener'
  link.click()
  window.setTimeout(() => URL.revokeObjectURL(url), 0)
}
