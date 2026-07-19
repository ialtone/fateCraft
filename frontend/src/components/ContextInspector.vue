<script setup lang="ts">
import { computed } from 'vue'
import { NProgress, NTag } from 'naive-ui'
import { useContextStore } from '@/stores/context'

const context = useContextStore()
const log = computed(() => context.lastBuild)

const usagePct = computed(() => {
  if (!log.value || !log.value.max_context_tokens) return 0
  return Math.min(
    100,
    Math.round((log.value.used_prompt_tokens / log.value.max_context_tokens) * 100),
  )
})
</script>

<template>
  <aside class="inspector scroll">
    <div class="head"><span class="orn">◆</span> 上下文检查器</div>
    <div v-if="!log" class="faint pad empty">
      尚无上下文记录。<br />发送一条消息后，这里会显示本次请求用到的 Prompt、世界书和历史明细。
    </div>
    <template v-else>
      <div class="pad">
        <div class="row spread sm">
          <span class="faint">模型</span><span class="mono">{{ log.model }}</span>
        </div>
        <div class="row spread sm">
          <span class="faint">已用 / 上限</span>
          <span class="mono">{{ log.used_prompt_tokens }} / {{ log.max_context_tokens }}</span>
        </div>
        <div class="row spread sm">
          <span class="faint">预留回复</span
          ><span class="mono">{{ log.reserved_response_tokens }}</span>
        </div>
        <NProgress
          type="line"
          :percentage="usagePct"
          :height="6"
          :border-radius="3"
          :show-indicator="false"
          style="margin-top: 10px"
        />
        <div class="pct faint">{{ usagePct }}%</div>
      </div>

      <div class="section-title">Prompt 段明细</div>
      <div class="pad">
        <div
          v-for="(seg, i) in log.segments"
          :key="i"
          class="seg"
          :class="{ skipped: !seg.included }"
        >
          <div class="row spread">
            <span class="row" style="gap: 7px">
              <span class="dot" :class="seg.included ? 'on' : 'off'"></span>
              <span class="seg-key">{{ seg.key }}</span>
              <NTag v-if="seg.required" size="tiny" round type="warning" :bordered="false"
                >必选</NTag
              >
            </span>
            <span class="faint mono">{{ seg.tokens }}t</span>
          </div>
          <div v-if="seg.skip_reason" class="faint xs">已裁剪：{{ seg.skip_reason }}</div>
        </div>
      </div>

      <div class="section-title">历史 / 世界书</div>
      <div class="pad sm">
        <div class="row spread">
          <span class="faint">纳入历史消息</span
          ><span class="mono">{{ log.included_message_ids?.length || 0 }}</span>
        </div>
        <div class="row spread">
          <span class="faint">激活世界书条目</span
          ><span class="mono">{{ log.activated_world_entry_ids?.length || 0 }}</span>
        </div>
      </div>
    </template>
  </aside>
</template>

<style scoped>
.inspector {
  height: 100%;
  background: var(--bg-panel);
  border-left: 1px solid var(--border-soft);
  display: flex;
  flex-direction: column;
}

.head {
  padding: 13px 16px;
  border-bottom: 1px solid var(--border-soft);
  font-weight: 600;
  letter-spacing: 2px;
  font-size: 13.5px;
  flex-shrink: 0;
}

.orn {
  color: var(--accent);
  font-size: 9px;
  margin-right: 4px;
  vertical-align: 2px;
}

.pad {
  padding: 12px 16px;
}

.empty {
  line-height: 1.9;
}

.section-title {
  padding: 7px 16px;
  font-size: 11px;
  letter-spacing: 2px;
  color: var(--text-faint);
  background: var(--bg-soft);
  border-top: 1px solid var(--border-soft);
  border-bottom: 1px solid var(--border-soft);
}

.sm {
  font-size: 12.5px;
}

.sm + .sm {
  margin-top: 4px;
}

.xs {
  font-size: 11px;
  margin-top: 2px;
}

.mono {
  font-family: Consolas, Menlo, monospace;
  font-size: 12px;
}

.pct {
  text-align: right;
  font-size: 11px;
  margin-top: 3px;
}

.seg {
  padding: 6px 0;
  border-bottom: 1px dashed var(--border-soft);
  font-size: 12.5px;
}

.seg:last-child {
  border-bottom: none;
}

.seg.skipped {
  opacity: 0.45;
}

.seg-key {
  letter-spacing: 0.5px;
}

.dot {
  display: inline-block;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  flex-shrink: 0;
}

.dot.on {
  background: var(--ok);
  box-shadow: 0 0 5px var(--ok);
}

.dot.off {
  background: var(--text-faint);
}
</style>
