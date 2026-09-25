import { defineStore } from 'pinia'
import { ref } from 'vue'
import { stateApi } from '@/api'

export const useStateStore = defineStore('state', () => {
  const state = ref<Record<string, any>>({})
  // 请求代际：切换对话、实时事件或 reset 之后，迟到的旧响应不能覆盖当前状态。
  let requestVersion = 0

  async function load(convId: string) {
    const version = ++requestVersion
    const next = await stateApi.get(convId)
    if (version === requestVersion) state.value = next
  }

  function set(newState: Record<string, any>) {
    requestVersion++
    state.value = newState
  }

  function reset() {
    requestVersion++
    state.value = {}
  }

  return { state, load, set, reset }
})
