import { defineStore } from 'pinia'
import { ref } from 'vue'
import { activityApi, contextApi, runtimeInspectorApi } from '@/api'
import type { ContextBuildLog, ConversationActivity, RuntimeInspector } from '@/types'

export const useContextStore = defineStore('context', () => {
  const lastBuild = ref<ContextBuildLog | null>(null)
  const activity = ref<ConversationActivity>({
    narrative: 'idle',
    memory: 'idle',
    can_continue: false,
    can_regenerate: false,
    regenerate_cost_quantity: 0,
  })
  const runtimeInspector = ref<RuntimeInspector | null>(null)
  let activityRequestVersion = 0

  async function loadLast(convId: string) {
    try {
      lastBuild.value = await contextApi.last(convId)
    } catch {
      lastBuild.value = null
    }
  }

  async function loadById(id: string) {
    try {
      lastBuild.value = await contextApi.get(id)
    } catch {
      // 忽略：日志可能尚未可读。
    }
  }

  async function loadActivity(convId: string) {
    const version = ++activityRequestVersion
    try {
      const next = await activityApi.get(convId)
      if (version === activityRequestVersion) activity.value = next
    } catch {
      if (version === activityRequestVersion) {
        activity.value = { narrative: 'idle', memory: 'idle', can_continue: false, can_regenerate: false, regenerate_cost_quantity: 0 }
      }
    }
  }

  function setActivity(kind: 'memory', status: ConversationActivity['memory']) {
    activityRequestVersion++
    activity.value = { ...activity.value, [kind]: status }
  }

  function setNarrative(
    status: ConversationActivity['narrative'],
    details: Pick<ConversationActivity, 'step_no' | 'tool_category'> = {},
  ) {
    activityRequestVersion++
    activity.value = {
      ...activity.value,
      narrative: status,
      step_no: details.step_no,
      tool_category: details.tool_category,
    }
  }
  async function loadRuntimeInspector(convId: string) {
    try { runtimeInspector.value = await runtimeInspectorApi.get(convId); activity.value = runtimeInspector.value.activity; lastBuild.value = runtimeInspector.value.request || null } catch { /* panel is non-blocking */ }
  }

  // Terminal agent events arrive before the follow-up activity request. Set
  // the recovery capability immediately so a failed/cancelled turn can never
  // briefly open the paid confirmation dialog from stale completed-turn data.
  function setTurnCapabilities(details: Pick<ConversationActivity, 'turn_status' | 'can_regenerate' | 'regenerate_cost_quantity'>) {
    activityRequestVersion++
    activity.value = { ...activity.value, ...details }
  }

  function reset() {
    activityRequestVersion++
    lastBuild.value = null; runtimeInspector.value = null
    activity.value = { narrative: 'idle', memory: 'idle', can_continue: false, can_regenerate: false, regenerate_cost_quantity: 0 }
  }

  return { lastBuild, activity, runtimeInspector, loadLast, loadById, loadActivity, loadRuntimeInspector, setActivity, setNarrative, setTurnCapabilities, reset }
})
