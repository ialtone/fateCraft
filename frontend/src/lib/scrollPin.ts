// 用户滚回距底部小于该值时重新贴底。
export const REPIN_THRESHOLD = 80

// 滚动事件后的贴底状态：scrollTop 变小（用户上翻，包括平滑滚动的每一帧）
// 一律解除贴底，即使仍在底部附近；内容变高或程序滚到底不会解除；
// 只有向下滚（或静止）回到底部附近才重新贴底。
export function nextPinned(
  pinned: boolean,
  prevTop: number,
  top: number,
  distanceToBottom: number,
) {
  if (top < prevTop) return false
  return distanceToBottom < REPIN_THRESHOLD ? true : pinned
}
