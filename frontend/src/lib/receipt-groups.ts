import type { ChatMessage } from '@/stores/chat'

/**
 * 时间线分组：把连续的面板动作系统回执（“已执行操作：X”）合并为一组，
 * 供时间线渲染成可展开的“面板操作”条目；其余消息逐条保持原样。
 * 分组只影响展示，不修改消息列表本身。
 */
export interface TimelineGroup {
  key: string
  message?: ChatMessage
  receipts?: ChatMessage[]
}

export function isActionReceipt(message: ChatMessage): boolean {
  return message.type === 'system' && !!message.metadata?.module_action
}

export function groupTimeline(messages: ChatMessage[]): TimelineGroup[] {
  const groups: TimelineGroup[] = []
  let buffer: ChatMessage[] = []
  const flush = () => {
    if (buffer.length) {
      groups.push({ key: `receipts-${buffer[0].id}`, receipts: buffer })
      buffer = []
    }
  }
  for (const message of messages) {
    if (isActionReceipt(message)) {
      buffer.push(message)
      continue
    }
    flush()
    groups.push({ key: message.id, message })
  }
  flush()
  return groups
}
