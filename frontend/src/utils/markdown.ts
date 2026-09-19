import { marked } from 'marked'
import DOMPurify from 'dompurify'

marked.setOptions({ breaks: true, gfm: true })

// 把 Markdown 渲染为经过净化的 HTML。
export function renderMarkdown(src: string): string {
  const raw = marked.parse(src || '', { async: false }) as string
  return DOMPurify.sanitize(raw)
}

// 公开分享页不加载远程图片，避免第三方资源获知访问者 IP 或 Referer。
export function renderSharedMarkdown(src: string): string {
  const raw = marked.parse(src || '', { async: false }) as string
  const clean = DOMPurify.sanitize(raw, { FORBID_TAGS: ['img', 'iframe', 'style'] })
  const document = new DOMParser().parseFromString(clean, 'text/html')
  document.querySelectorAll('a').forEach((anchor) => {
    anchor.setAttribute('rel', 'noopener noreferrer')
    anchor.setAttribute('target', '_blank')
  })
  return document.body.innerHTML
}
