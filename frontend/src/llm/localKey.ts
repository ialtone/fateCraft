// 客户端直连模式的本地 API Key：只存浏览器 localStorage，
// 按用户隔离，绝不经过 pinia，不进任何日志或错误上报，服务端永远收不到它。

const KEY_PREFIX = 'fatecraft.llm.key.'

function storageKey(userId: string): string {
  return `${KEY_PREFIX}${userId}`
}

function available(): boolean {
  try {
    return typeof window !== 'undefined' && !!window.localStorage
  } catch {
    return false
  }
}

/** 读取本设备保存的 Key，没有时返回空串。 */
export function getLocalKey(userId: string): string {
  if (!userId || !available()) return ''
  try {
    return window.localStorage.getItem(storageKey(userId)) || ''
  } catch {
    return ''
  }
}

/** 保存本设备 Key；空值等同清除。 */
export function setLocalKey(userId: string, key: string): void {
  if (!userId || !available()) return
  try {
    const value = key.trim()
    if (!value) window.localStorage.removeItem(storageKey(userId))
    else window.localStorage.setItem(storageKey(userId), value)
  } catch {
    // 配额不足或隐私模式：调用方按“未保存”处理。
  }
}

/** 清除本设备 Key。 */
export function clearLocalKey(userId: string): void {
  if (!userId || !available()) return
  try {
    window.localStorage.removeItem(storageKey(userId))
  } catch {
    // 忽略。
  }
}
