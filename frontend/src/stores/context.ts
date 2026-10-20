import { defineStore } from 'pinia'
import { ref } from 'vue'
import { activityApi, contextApi, runtimeInspectorApi } from '@/api'
import type { ContextBuildLog, ConversationActivity, RuntimeInspector } from '@/types'

const emptyActivity = (): ConversationActivity => ({
  narrative: 'idle',
  memory: 'idle',
  can_continue: false,
  can_regenerate: false,
  regenerate_cost_quantity: 0,
})

export const useContextStore = defineStore('context', () => {
  const lastBuild = ref<ContextBuildLog | null>(null)
  const activity = ref<ConversationActivity>(emptyActivity())
  const activityStale = ref(true)
  const runtimeInspector = ref<RuntimeInspector | null>(null)
  const inspectorVisible = ref(false)
  const inspectorDirty = ref(true)
  const recoverySuspended = ref(false)
  let conversationId: string | null = null
  let generation = 0
  let phaseRevision = 0
  let phaseAttempt: string | null = null
  let targetVersion = 0
  let inFlight: Promise<ConversationActivity | null> | null = null
  let inspectorFlight: Promise<void> | null = null
  let inspectorRevision = 0
  let inspectorTimer: ReturnType<typeof setTimeout> | null = null

  function clearInspectorTimer() {
    if (inspectorTimer) clearTimeout(inspectorTimer)
    inspectorTimer = null
  }

  function enter(convId: string) {
    if (conversationId === convId) return
    reset()
    conversationId = convId
  }

  async function loadLast(convId: string) {
    const epoch = generation
    try {
      const next = await contextApi.last(convId)
      if (epoch === generation) lastBuild.value = next
    } catch {
      /* Optional metadata leaves the last successful view intact. */
    }
  }

  async function loadById(id: string) {
    const epoch = generation
    try {
      const next = await contextApi.get(id)
      if (epoch === generation) lastBuild.value = next
    } catch {
      /* The build log may not yet be readable. */
    }
  }

  function applyActivitySnapshot(next: ConversationActivity, requestPhase?: number): boolean {
    if (next.conversation_id && next.conversation_id !== conversationId) return false
    const previous = activity.value
    if (
      phaseAttempt &&
      next.attempt_id &&
      next.attempt_id !== phaseAttempt &&
      next.attempt_id === previous.attempt_id
    )
      return false
    if ((next.branch_epoch ?? 0) < (previous.branch_epoch ?? 0)) return false
    if (
      previous.activity_version !== undefined &&
      (next.activity_version === undefined || next.activity_version < previous.activity_version)
    )
      return false
    const sameAttempt = next.attempt_id === (phaseAttempt ?? previous.attempt_id)
    const keepPhase =
      next.is_processing !== false &&
      sameAttempt &&
      (requestPhase === undefined ? phaseRevision > 0 : requestPhase !== phaseRevision)
    activity.value = {
      ...next,
      ...(keepPhase
        ? {
            narrative: previous.narrative,
            step_no: previous.step_no,
            tool_category: previous.tool_category,
          }
        : {}),
      ...(next.is_processing === false
        ? { narrative: 'idle', step_no: undefined, tool_category: undefined }
        : {}),
    }
    if (!sameAttempt || next.is_processing === false) {
      phaseRevision = 0
      phaseAttempt = null
    }
    activityStale.value = false
    return true
  }

  // All triggers share one request. A newer invalidation during the read raises
  // the target; it cannot be accidentally satisfied by an older in-flight result.
  function loadActivity(convId: string, minimumVersion = 0): Promise<ConversationActivity | null> {
    if (recoverySuspended.value) return Promise.resolve(null)
    enter(convId)
    targetVersion = Math.max(targetVersion, minimumVersion)
    if (inFlight) return inFlight
    const epoch = generation
    const request = (async () => {
      try {
        for (let reads = 0; reads < 2; reads++) {
          const phase = phaseRevision
          const next = await activityApi.get(convId)
          if (epoch !== generation) return null
          applyActivitySnapshot(next, phase)
          if ((activity.value.activity_version ?? 0) >= targetVersion) return activity.value
        }
        throw new Error('Activity snapshot has not reached the requested version')
      } catch (error) {
        if (epoch === generation) activityStale.value = true
        throw error
      }
    })()
    inFlight = request
    void request
      .finally(() => {
        if (epoch === generation && inFlight === request) inFlight = null
      })
      .catch(() => {})
    return request
  }

  function setNarrative(
    status: ConversationActivity['narrative'],
    details: Pick<ConversationActivity, 'step_no' | 'tool_category'> = {},
  ) {
    phaseRevision++
    activity.value = {
      ...activity.value,
      narrative: status,
      step_no: details.step_no,
      tool_category: details.tool_category,
    }
  }

  function notePhaseAttempt(attemptId: string) {
    if (phaseAttempt !== attemptId) phaseRevision = 0
    phaseAttempt = attemptId
  }

  function invalidateRequests() {
    generation++
    inFlight = null
    inspectorFlight = null
    clearInspectorTimer()
    inspectorRevision++
    inspectorDirty.value = true
    activityStale.value = true
  }

  function setRecoverySuspended(value: boolean) {
    recoverySuspended.value = value
    if (value) clearInspectorTimer()
  }

  function loadRuntimeInspector(convId: string): Promise<void> {
    if (recoverySuspended.value) return Promise.resolve()
    if (inspectorFlight) return inspectorFlight
    const epoch = generation
    const revision = inspectorRevision
    const phase = phaseRevision
    const request = (async () => {
      try {
        const next = await runtimeInspectorApi.get(convId)
        if (epoch !== generation || conversationId !== convId) return
        runtimeInspector.value = next
        lastBuild.value = next.request || null
        applyActivitySnapshot(next.activity, phase)
        inspectorDirty.value = inspectorRevision !== revision
      } catch {
        if (epoch === generation) inspectorDirty.value = true
      }
    })()
    inspectorFlight = request
    void request
      .finally(() => {
        if (epoch !== generation || inspectorFlight !== request) return
        inspectorFlight = null
        // 在途请求之后收到的新失效不能被一次旧读取消费。
        if (inspectorRevision !== revision && inspectorDirty.value) scheduleInspector()
      })
      .catch(() => {})
    return request
  }

  function invalidateInspector() {
    inspectorRevision++
    inspectorDirty.value = true
    scheduleInspector()
  }

  function scheduleInspector() {
    // Agent 终态和 activity 终态可能在同一 tick 各调用一次；合并成一次读取。
    if (recoverySuspended.value || !inspectorVisible.value || !conversationId) return
    const id = conversationId
    const epoch = generation
    clearInspectorTimer()
    inspectorTimer = setTimeout(() => {
      inspectorTimer = null
      if (epoch !== generation || !inspectorVisible.value || conversationId !== id) return
      if (!inspectorDirty.value) return
      void loadRuntimeInspector(id)
    }, 50)
  }

  function setInspectorVisible(visible: boolean) {
    inspectorVisible.value = visible
    if (!visible) {
      clearInspectorTimer()
      return
    }
    if (!recoverySuspended.value && inspectorDirty.value && conversationId) {
      clearInspectorTimer()
      void loadRuntimeInspector(conversationId)
    }
  }

  function reset() {
    generation++
    phaseRevision = 0
    phaseAttempt = null
    targetVersion = 0
    inFlight = null
    inspectorFlight = null
    clearInspectorTimer()
    conversationId = null
    inspectorRevision++
    inspectorDirty.value = true
    activityStale.value = true
    lastBuild.value = null
    runtimeInspector.value = null
    activity.value = emptyActivity()
  }

  return {
    lastBuild,
    activity,
    activityStale,
    runtimeInspector,
    inspectorVisible,
    inspectorDirty,
    enter,
    invalidateRequests,
    notePhaseAttempt,
    loadLast,
    loadById,
    loadActivity,
    applyActivitySnapshot,
    loadRuntimeInspector,
    invalidateInspector,
    setInspectorVisible,
    setRecoverySuspended,
    setNarrative,
    reset,
  }
})
