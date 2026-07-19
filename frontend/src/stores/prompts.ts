import { defineStore } from 'pinia'
import { ref } from 'vue'
import { promptsApi } from '@/api'
import type { PromptPreset } from '@/types'

export const usePromptStore = defineStore('prompt', () => {
  const presets = ref<PromptPreset[]>([])
  const current = ref<PromptPreset | null>(null)

  async function loadList() {
    presets.value = await promptsApi.list()
  }

  async function load(id: string) {
    current.value = await promptsApi.get(id)
    return current.value
  }

  async function create(name: string) {
    const p = await promptsApi.create({ name })
    presets.value.unshift(p)
    return p
  }

  async function remove(id: string) {
    await promptsApi.remove(id)
    presets.value = presets.value.filter((p) => p.id !== id)
    if (current.value?.id === id) current.value = null
  }

  async function addSegment(presetId: string, body: Record<string, any>) {
    await promptsApi.addSegment(presetId, body)
    await load(presetId)
  }

  async function updateSegment(presetId: string, segId: string, body: Record<string, any>) {
    await promptsApi.updateSegment(segId, body)
    await load(presetId)
  }

  async function removeSegment(presetId: string, segId: string) {
    await promptsApi.removeSegment(segId)
    await load(presetId)
  }

  return {
    presets,
    current,
    loadList,
    load,
    create,
    remove,
    addSegment,
    updateSegment,
    removeSegment,
  }
})
