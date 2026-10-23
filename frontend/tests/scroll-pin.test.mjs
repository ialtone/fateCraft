import assert from 'node:assert/strict'
import fs from 'node:fs'
import test from 'node:test'
import { nextPinned, REPIN_THRESHOLD } from '../src/lib/scrollPin.ts'

test('content growth and programmatic scroll-to-bottom keep the timeline pinned', () => {
  // 节流渲染一次性撑高 500px：scrollTop 不变，距底部远超阈值，仍保持贴底。
  assert.equal(nextPinned(true, 1000, 1000, 500), true)
  // ResizeObserver 滚到底：scrollTop 增大。
  assert.equal(nextPinned(true, 1000, 1500, 0), true)
})

test('user scrolling up unpins and returning near the bottom re-pins', () => {
  assert.equal(nextPinned(true, 1500, 1400, 100), false)
  assert.equal(nextPinned(false, 1400, 1450, 300), false)
  assert.equal(nextPinned(false, 1450, 1700, REPIN_THRESHOLD - 1), true)
})

test('user wheel-up during growth stays unpinned and position stable', () => {
  // 平滑滚动的第一帧只上移几像素、仍在阈值内，也必须解除贴底。
  let pinned = nextPinned(true, 2000, 1990, 10)
  assert.equal(pinned, false)
  // 之后流式内容持续撑高：scrollTop 不动、距底部变大，始终不重新贴底。
  for (const dist of [200, 600, 1200]) pinned = nextPinned(pinned, 1990, 1990, dist)
  assert.equal(pinned, false)
})

test('timeline follows layout via ResizeObserver instead of content events', () => {
  const src = fs.readFileSync(
    new URL('../src/components/MessageTimeline.vue', import.meta.url),
    'utf8',
  )
  assert.match(src, /new ResizeObserver/)
  assert.match(src, /resizeObserver\?\.disconnect\(\)/)
  // 底部哨兵 overflow-anchor 会在用户上翻后把视口继续往下推。
  assert.doesNotMatch(src, /overflow-anchor:/)
  assert.doesNotMatch(src, /requestAnimationFrame|setTimeout|CHAT_CONTENT_CHANGED_EVENT/)
})
