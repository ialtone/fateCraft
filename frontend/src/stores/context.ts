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
  // 运行面板请求代际：切换对话或 reset 后，迟到的旧响应直接丢弃。
  let inspectorRequestVersion = 0

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
        activity.value = {
          narrative: 'idle',
          memory: 'idle',
          can_continue: false,
          can_regenerate: false,
          regenerate_cost_quantity: 0,
        }
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
    const version = ++inspectorRequestVersion
    const activityVersion = activityRequestVersion
    try {
      const next = await runtimeInspectorApi.get(convId)
      if (version !== inspectorRequestVersion) return
      runtimeInspector.value = next
      lastBuild.value = next.request || null
      // 请求期间若已有更新的实时活动状态，就不用这次响应覆盖它。
      if (activityVersion === activityRequestVersion) activity.value = next.activity
    } catch {
      // 运行面板不阻塞对话。
    }
  }

  // 终止类 agent 事件先于后续的 activity 请求到达。这里立即写入恢复能力，
  // 避免失败或取消的回合短暂沿用上一轮已完成回合的数据、误弹付费确认框。
  function setTurnCapabilities(
    details: Pick<
      ConversationActivity,
      'turn_status' | 'can_regenerate' | 'regenerate_cost_quantity'
    >,
  ) {
    activityRequestVersion++
    activity.value = { ...activity.value, ...details }
  }

  function reset() {
    activityRequestVersion++
    inspectorRequestVersion++
    lastBuild.value = null
    runtimeInspector.value = null
    activity.value = {
      narrative: 'idle',
      memory: 'idle',
      can_continue: false,
      can_regenerate: false,
      regenerate_cost_quantity: 0,
    }
  }

  return {
    lastBuild,
    activity,
    runtimeInspector,
    loadLast,
    loadById,
    loadActivity,
    loadRuntimeInspector,
    setActivity,
    setNarrative,
    setTurnCapabilities,
    reset,
  }
})
