import { ref } from 'vue'
import { defineStore } from 'pinia'
import { useChatStore } from './chat'
import { useConversationStore } from './conversations'
import { useStateStore } from './state'
import { useGuidanceStore } from './guidance'
import { useRuntimeStore } from './runtime'
import { useRewardStore } from './rewards'
import { useCampaignStore } from './campaign'
import { useContextStore } from './context'

export type ConversationResource =
  | 'conversation'
  | 'messages'
  | 'state'
  | 'guidance'
  | 'endings'
  | 'runtime'
  | 'rewards'
  | 'campaign'
  | 'inspector'
  | 'actions'

type Loader = () => Promise<unknown>

// 对话资源统一失效/刷新入口：
// - 同一资源同一时刻最多一个请求；
// - 在途期间再次失效不丢标记，完成后最多补读一次；
// - 隐藏面板只置 dirty，不产生网络请求；
// - 切换对话后旧请求的结果和 finally 不能影响新上下文。
export const useConversationResources = defineStore('conversationResources', () => {
  const currentID = ref('')
  const suspended = ref(false)
  const visible = ref<Record<'actions' | 'inspector', boolean>>({
    actions: false,
    inspector: true,
  })
  const dirty = new Map<ConversationResource, number>()
  const revisions = new Map<ConversationResource, number>()
  const timers = new Map<ConversationResource, ReturnType<typeof setTimeout>>()
  const flights = new Map<ConversationResource, Promise<unknown>>()
  const loaders = new Map<ConversationResource, Loader>()
  let epoch = 0
  let failureHandler: ((error: unknown) => void) | null = null
  const WINDOW = 50

  function markDirty(resource: ConversationResource) {
    dirty.set(resource, (dirty.get(resource) ?? 0) + 1)
  }

  function canLoad(resource: ConversationResource) {
    if (resource === 'actions') return visible.value.actions
    if (resource === 'inspector') return visible.value.inspector
    return true
  }

  async function loaderFor(
    resource: ConversationResource,
    convId: string,
    localEpoch: number,
  ): Promise<unknown> {
    const custom = loaders.get(resource)
    if (custom) return custom()
    switch (resource) {
      case 'conversation':
        return useConversationStore().load(convId)
      case 'messages':
        return useChatStore().load(convId)
      case 'state':
        return useStateStore().load(convId)
      case 'guidance':
        return useGuidanceStore().load(convId)
      case 'endings':
        // 首屏对话详情也由 ready 后加载；结局投影校验依赖它的 ID/分支。
        if (flights.has('conversation')) await flights.get('conversation')
        else if (useConversationStore().current?.id !== convId) await load('conversation')
        if (localEpoch !== epoch || currentID.value !== convId || suspended.value) return
        return useConversationStore().loadEndingState(convId)
      case 'runtime':
        return useRuntimeStore().load(convId)
      case 'rewards':
        return useRewardStore().load()
      case 'campaign': {
        const campaignID = useConversationStore().current?.campaign_id
        if (!campaignID) return Promise.resolve()
        return useCampaignStore().load(campaignID)
      }
      case 'inspector': {
        const context = useContextStore()
        if (!context.inspectorVisible || !context.inspectorDirty) return Promise.resolve()
        return context.loadRuntimeInspector(convId)
      }
      case 'actions':
        return Promise.resolve()
    }
  }

  function clearTimer(resource: ConversationResource) {
    const timer = timers.get(resource)
    if (timer) clearTimeout(timer)
    timers.delete(resource)
  }

  function schedule(resource: ConversationResource, immediate = false) {
    if (!currentID.value || suspended.value || !canLoad(resource)) return
    if (flights.has(resource)) return
    if (immediate) {
      clearTimer(resource)
      void load(resource).catch(() => {})
      return
    }
    if (timers.has(resource)) return
    const id = currentID.value
    const localEpoch = epoch
    timers.set(
      resource,
      setTimeout(() => {
        timers.delete(resource)
        if (localEpoch !== epoch || currentID.value !== id || suspended.value || !canLoad(resource))
          return
        void load(resource).catch(() => {})
      }, WINDOW),
    )
  }

  async function load(resource: ConversationResource): Promise<unknown> {
    const convId = currentID.value
    if (!convId || suspended.value) return
    const existing = flights.get(resource)
    if (existing) return existing
    if (!canLoad(resource)) {
      markDirty(resource)
      return
    }
    const localEpoch = epoch
    const revision = revisions.get(resource) ?? 0
    clearTimer(resource)
    dirty.delete(resource)
    const flight = Promise.resolve().then(() => {
      if (localEpoch !== epoch || currentID.value !== convId || suspended.value) return
      return loaderFor(resource, convId, localEpoch)
    })
    flights.set(resource, flight)
    try {
      return await flight
    } catch (error) {
      if (localEpoch === epoch && currentID.value === convId && !suspended.value) {
        markDirty(resource)
        failureHandler?.(error)
      }
      throw error
    } finally {
      if (flights.get(resource) === flight) flights.delete(resource)
      if (
        localEpoch === epoch &&
        currentID.value === convId &&
        (revisions.get(resource) ?? 0) !== revision
      ) {
        // 只补读一轮；持续变化留到下一个合并窗口，避免立即循环。
        markDirty(resource)
        schedule(resource)
      }
    }
  }

  function invalidate(resource: ConversationResource, immediate = false) {
    if (!currentID.value || suspended.value) return
    markDirty(resource)
    revisions.set(resource, (revisions.get(resource) ?? 0) + 1)
    if (!canLoad(resource)) return
    schedule(resource, immediate)
  }

  function invalidateMany(resources: ConversationResource[], immediate = false) {
    for (const resource of resources) invalidate(resource, immediate)
  }

  async function refresh(resources: ConversationResource[]) {
    return Promise.allSettled(resources.map((resource) => load(resource)))
  }

  function setVisible(resource: 'actions' | 'inspector', value: boolean) {
    visible.value[resource] = value
    if (value && !suspended.value && dirty.has(resource)) schedule(resource, true)
  }

  function registerLoader(resource: ConversationResource, loader: Loader) {
    loaders.set(resource, loader)
    return () => {
      if (loaders.get(resource) === loader) loaders.delete(resource)
    }
  }

  function markLoaded(resource: ConversationResource) {
    dirty.delete(resource)
  }

  function enter(convId: string) {
    if (currentID.value === convId) return
    resetInFlight()
    currentID.value = convId
    suspended.value = false
    for (const timer of timers.values()) clearTimeout(timer)
    timers.clear()
    dirty.clear()
    revisions.clear()
    flights.clear()
    loaders.clear()
    failureHandler = null
    visible.value.actions = false
    visible.value.inspector = true
  }

  // 连接上下文失效：旧请求可以完成，但不能再触发补读或复用其 Promise。
  function resetInFlight() {
    epoch++
    // 同时失效实际提交结果的 store；只清 Promise 不能阻止旧响应写回。
    useConversationStore().invalidateRequests()
    useChatStore().invalidateRequests()
    useStateStore().invalidateRequests()
    useGuidanceStore().invalidateRequests()
    useRuntimeStore().invalidateRequests()
    useRewardStore().invalidateRequests()
    useCampaignStore().invalidateRequests()
    for (const timer of timers.values()) clearTimeout(timer)
    timers.clear()
    dirty.clear()
    revisions.clear()
    flights.clear()
  }

  function suspend() {
    suspended.value = true
    resetInFlight()
  }

  function resume() {
    if (suspended.value) suspended.value = false
  }

  function reset() {
    resetInFlight()
    currentID.value = ''
    suspended.value = false
    for (const timer of timers.values()) clearTimeout(timer)
    timers.clear()
    dirty.clear()
    revisions.clear()
    flights.clear()
    loaders.clear()
    failureHandler = null
    visible.value.actions = false
    visible.value.inspector = false
  }

  return {
    currentID,
    visible,
    enter,
    reset,
    resetInFlight,
    suspend,
    resume,
    invalidate,
    invalidateMany,
    refresh,
    load,
    setVisible,
    registerLoader,
    setFailureHandler: (handler: (error: unknown) => void) => {
      failureHandler = handler
    },
    markLoaded,
    isDirty: (resource: ConversationResource) => dirty.has(resource),
  }
})
