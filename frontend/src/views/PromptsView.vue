<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { NButton, NEmpty, NIcon, NInput, useDialog, useMessage } from 'naive-ui'
import { AddOutline } from '@vicons/ionicons5'
import PromptSegmentList from '@/components/PromptSegmentList.vue'
import { usePromptStore } from '@/stores/prompts'

const store = usePromptStore()
const dialog = useDialog()
const message = useMessage()
const newName = ref('')

onMounted(() => store.loadList())

async function select(id: string) {
  await store.load(id)
}

async function createPreset() {
  const name = newName.value.trim()
  if (!name) return
  try {
    const p = await store.create(name)
    newName.value = ''
    await select(p.id)
  } catch (err: any) {
    message.error(`创建预设失败：${err.message || '未知错误'}`)
  }
}

function removePreset(id: string, name: string) {
  dialog.warning({
    title: '删除预设',
    content: `删除 Prompt 预设「${name}」？`,
    positiveText: '删除',
    negativeText: '取消',
    onPositiveClick: () => store.remove(id),
  })
}

async function onAdd(body: Record<string, any>) {
  if (store.current) await store.addSegment(store.current.id, body)
}
async function onUpdate(id: string, body: Record<string, any>) {
  if (store.current) await store.updateSegment(store.current.id, id, body)
}
async function onRemove(id: string) {
  if (store.current) await store.removeSegment(store.current.id, id)
}
</script>

<template>
  <div class="page">
    <div class="page-title">
      <h2>Prompt 预设</h2>
      <span class="ornament">◆</span>
    </div>
    <p class="page-sub">叙事引擎的总纲——分段按位次拼装进每次请求。</p>

    <div class="layout">
      <div class="col">
        <div class="row" style="gap: 8px; margin-bottom: 4px">
          <NInput
            v-model:value="newName"
            placeholder="新预设名称"
            size="small"
            @keyup.enter="createPreset"
          />
          <NButton type="primary" size="small" @click="createPreset">
            <template #icon><NIcon :component="AddOutline" /></template>
          </NButton>
        </div>
        <div
          v-for="p in store.presets"
          :key="p.id"
          class="item"
          :class="{ active: store.current?.id === p.id }"
          @click="select(p.id)"
        >
          <div class="row spread">
            <strong class="item-name">{{ p.name }}</strong>
            <NButton
              quaternary
              size="tiny"
              type="error"
              class="del"
              @click.stop="removePreset(p.id, p.name)"
            >
              删
            </NButton>
          </div>
        </div>
      </div>

      <div class="col">
        <NEmpty
          v-if="!store.current"
          description="选择一个预设编辑其分段。"
          style="padding: 40px 0"
        />
        <template v-else>
          <h3 class="seg-title">{{ store.current.name }} · 分段</h3>
          <PromptSegmentList
            :segments="store.current.segments || []"
            @add="onAdd"
            @update="onUpdate"
            @remove="onRemove"
          />
        </template>
      </div>
    </div>
  </div>
</template>

<style scoped>
.page {
  max-width: 1100px;
  margin: 0 auto;
}

.layout {
  display: grid;
  grid-template-columns: 270px 1fr;
  gap: 18px;
  align-items: start;
  margin-top: 6px;
}

.col {
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.item {
  position: relative;
  background: var(--bg-panel);
  border: 1px solid var(--border-soft);
  border-radius: var(--radius);
  padding: 11px 13px;
  cursor: pointer;
  transition: border-color 0.2s ease;
  overflow: hidden;
}

.item::before {
  content: '';
  position: absolute;
  left: 0;
  top: 0;
  bottom: 0;
  width: 3px;
  background: transparent;
  transition: background 0.2s ease;
}

.item:hover {
  border-color: var(--border);
}

.item.active {
  border-color: var(--accent);
}

.item.active::before {
  background: var(--accent);
}

.item-name {
  letter-spacing: 1px;
}

.del {
  opacity: 0;
  transition: opacity 0.15s ease;
}

.item:hover .del {
  opacity: 1;
}

.seg-title {
  margin: 0 0 4px;
  font-size: 15px;
  letter-spacing: 1px;
}
</style>
