import { defineStore } from 'pinia'
import { ref } from 'vue'
import { stateApi } from '@/api'

export const useStateStore = defineStore('state', () => {
  const state = ref<Record<string, any>>({})

  async function load(convId: string) {
    state.value = await stateApi.get(convId)
  }

  function set(newState: Record<string, any>) {
    state.value = newState
  }

  function reset() {
    state.value = {}
  }

  return { state, load, set, reset }
})
