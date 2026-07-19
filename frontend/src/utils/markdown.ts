import { marked } from 'marked'
import DOMPurify from 'dompurify'

marked.setOptions({ breaks: true, gfm: true })

// 把 Markdown 渲染为经过净化的 HTML。
export function renderMarkdown(src: string): string {
  const raw = marked.parse(src || '', { async: false }) as string
  return DOMPurify.sanitize(raw)
}
