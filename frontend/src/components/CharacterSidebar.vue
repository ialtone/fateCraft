<script setup lang="ts">
import { ref, watch } from 'vue'
import { NIcon } from 'naive-ui'
import { ChatbubbleEllipsesOutline, PersonCircleOutline } from '@vicons/ionicons5'
import StatePanel from '@/components/StatePanel.vue'
import CampaignViewRenderer from '@/components/CampaignViewRenderer.vue'
import InquiryPanel from '@/components/InquiryPanel.vue'
import ModuleActionPanel from '@/components/ModuleActionPanel.vue'
import { useInquiryStore } from '@/stores/inquiry'
import { useRuntimeStore } from '@/stores/runtime'

const props = defineProps<{ convId: string; campaign: boolean }>()
const active = ref<'state' | 'inquiry' | 'actions'>('state')
const inquiry = useInquiryStore()
const runtime = useRuntimeStore()
watch(
  () => props.convId,
  () => {
    active.value = 'state'
  },
)

watch(
  [active, () => props.convId],
  ([value]) => {
    if (value === 'inquiry' && !inquiry.thread && !inquiry.loading) {
      inquiry.load(props.convId).catch(() => {})
    }
  },
  { immediate: true },
)
</script>

<template>
  <aside class="character-sidebar">
    <nav class="sidebar-tabs" aria-label="角色栏视图">
      <button type="button" :class="{ active: active === 'state' }" @click="active = 'state'">
        <NIcon :component="PersonCircleOutline" />
        <span>状态</span>
      </button>
      <button type="button" :class="{ active: active === 'inquiry' }" @click="active = 'inquiry'">
        <NIcon :component="ChatbubbleEllipsesOutline" />
        <span>询问</span>
      </button>
      <button
        v-if="runtime.runtime?.has_trusted_actions"
        type="button"
        :class="{ active: active === 'actions' }"
        @click="active = 'actions'"
      >
        <span>行动</span>
      </button>
    </nav>
    <CampaignViewRenderer
      v-if="
        active === 'state' &&
        campaign &&
        runtime.presentation?.campaign_view?.renderer === 'generic'
      "
      class="sidebar-content"
    />
    <StatePanel v-else-if="active === 'state'" class="sidebar-content" :conv-id="convId" />
    <InquiryPanel v-else-if="active === 'inquiry'" class="sidebar-content" :conv-id="convId" />
    <ModuleActionPanel
      v-if="runtime.runtime?.has_trusted_actions"
      v-show="active === 'actions'"
      class="sidebar-content"
      :conv-id="convId"
    />
  </aside>
</template>

<style scoped>
.character-sidebar {
  display: flex;
  width: 100%;
  min-width: 260px;
  max-width: 320px;
  height: 100%;
  min-height: 0;
  flex-direction: column;
  background: var(--bg-panel);
  border-right: 1px solid var(--border);
}

.sidebar-tabs {
  display: grid;
  grid-auto-flow: column;
  grid-auto-columns: minmax(0, 1fr);
  min-height: 42px;
  padding: 0 10px;
  border-bottom: 1px solid var(--border);
  background: var(--bg-soft);
}

.sidebar-tabs button {
  position: relative;
  display: flex;
  min-width: 0;
  align-items: center;
  justify-content: center;
  gap: 6px;
  padding: 0 8px;
  border: 0;
  color: var(--text-faint);
  background: transparent;
  font: inherit;
  font-size: 12px;
  cursor: pointer;
}

.sidebar-tabs button::after {
  position: absolute;
  right: 14px;
  bottom: 0;
  left: 14px;
  height: 2px;
  background: transparent;
  content: '';
}

.sidebar-tabs button.active {
  color: var(--accent);
}

.sidebar-tabs button.active::after {
  background: var(--accent);
}

.sidebar-content {
  flex: 1;
  width: 100%;
  min-width: 0;
  max-width: none;
  min-height: 0;
  border-right: 0;
}

@media (max-width: 768px) {
  .character-sidebar {
    min-width: 0;
    max-width: none;
    border-right: 0;
  }
}
</style>
