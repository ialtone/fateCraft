// A conversation ID alone cannot distinguish leaving and reopening the same
// conversation while an earlier mutation is still in flight.
export function createActionScope() {
  let epoch = 0
  let conversation = ''
  let alive = true
  return {
    enter(id: string) {
      conversation = id
      epoch++
    },
    capture() {
      return { conversation, epoch }
    },
    isCurrent(token: { conversation: string; epoch: number }) {
      return alive && token.conversation === conversation && token.epoch === epoch
    },
    dispose() {
      alive = false
      epoch++
    },
  }
}

export function clearMatchingAction(
  storage: Pick<Storage, 'getItem' | 'removeItem'>,
  conversation: string,
  requestID: string,
) {
  const key = `fatecraft:action:${conversation}`
  try {
    const pending = JSON.parse(storage.getItem(key) || 'null')
    if (pending?.request_id === requestID) storage.removeItem(key)
  } catch {
    // Unavailable browser storage does not invalidate the in-memory result.
  }
}
