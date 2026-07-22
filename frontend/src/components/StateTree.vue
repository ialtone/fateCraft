<script lang="ts">
import { inject, provide, reactive } from 'vue'
import type { InjectionKey } from 'vue'

// 展开状态的注入键：状态表由状态树根实例创建并向下共享，随根实例销毁一并回收。
// 不再放在模块级——那会让路径键跨对话无限累积（内存缓慢增长），
// 还会把上一个对话的折叠偏好带进下一个对话。
const expandedKey: InjectionKey<Record<string, boolean>> = Symbol('state-tree-expanded')
</script>

<script setup lang="ts">
// 递归渲染 JSON 状态树。
defineOptions({ name: 'StateTree' })

const props = defineProps<{ data: any; depth?: number; parentPath?: string }>()

// 根实例（无上级可注入）创建展开状态表；递归子实例注入并复用根的同一份，
// 折叠父节点再展开时子节点的折叠状态仍被记住，交互与原全局表完全一致。
const expanded =
  inject<Record<string, boolean> | null>(expandedKey, null) ?? reactive<Record<string, boolean>>({})
provide(expandedKey, expanded)

function typeOf(v: any): string {
  if (Array.isArray(v)) return 'array'
  if (v === null) return 'null'
  return typeof v
}

function getFullPath(key: string): string {
  return props.parentPath ? `${props.parentPath}.${key}` : key
}

function toggleExpand(key: string) {
  const path = getFullPath(key)
  // 首次点击时初始化为 true，然后取反
  if (expanded[path] === undefined) {
    expanded[path] = true
  }
  expanded[path] = !expanded[path]
}

function isExpanded(key: string): boolean {
  const path = getFullPath(key)
  // 未初始化时默认展开
  if (expanded[path] === undefined) {
    return true
  }
  return expanded[path]
}

// 状态键的中文映射
const keyLabels: Record<string, string> = {
  // 顶层命名空间
  profile: '档案',
  attributes: '属性',
  inventory: '物品',
  relationships: '关系',
  location: '位置',
  quests: '任务',
  risks: '风险',
  traits: '当前特质',
  world_flags: '世界标记',
  cultivation: '修炼状态',
  ascension: '飞升准备',

  // 通用字段
  id: '标识',
  name: '姓名',
  age: '年龄',
  gender: '性别',
  race: '种族',
  class: '职业',
  background: '背景',
  appearance: '外貌',
  description: '描述',
  title: '标题',
  status: '状态',
  notes: '备注',
  type: '类型',
  value: '数值',
  count: '数量',
  amount: '金额',
  talent: '天赋',
  clothing: '服饰',
  attire: '服饰',

  // attributes 下的属性
  health: '生命值',
  mana: '法力值',
  stamina: '体力值',
  strength: '力量',
  agility: '敏捷',
  intelligence: '智力',
  wisdom: '智慧',
  charisma: '魅力',
  constitution: '体质',
  level: '等级',
  experience: '经验值',
  experiences: '经历',
  max_health: '最大生命值',
  current_health: '当前生命值',
  max_mana: '最大法力值',
  current_mana: '当前法力值',

  // inventory 下的字段
  items: '持有物',
  gold: '金币',
  currency: '货币',
  equipment: '装备',
  weapon: '武器',
  armor: '护甲',
  accessories: '配饰',
  consumables: '消耗品',

  // location 下的字段
  region: '地区',
  place: '地点',
  area: '区域',
  coordinates: '坐标',
  current: '当前位置',

  // relationships 下的字段
  affinity: '好感度',
  affection: '亲密度',
  trust: '信任度',
  reputation: '声望',
  faction: '阵营',

  // quests 和 risks 下的字段
  objective: '目标',
  progress: '进度',
  reward: '奖励',
  deadline: '期限',
  priority: '优先级',
  severity: '严重程度',
  trigger: '触发条件',
  completed: '已完成',
  active: '进行中',
  failed: '已失败',

  // 修真相关（轮回试炼）
  cultivation_level: '修为境界',
  cultivation_stage: '修为阶段',
  cultivation_progress: '修炼进度',
  remaining_lifespan: '剩余寿元',
  spiritual_root: '灵根',
  technique: '功法',
  techniques: '功法',
  spirit_stones: '灵石',
  fate: '命运',
  karma: '因果',
  dao_heart: '道心',
  lifespan: '寿元',
  realm: '境界',
  quantity: '数量',
  breakthrough_attempts: '突破尝试次数',
  failed_breakthroughs: '突破失败次数',
  talent_awakened: '天赋已觉醒',
  cultivation_method: '当前功法',
  void_rift_discovered: '已发现虚空裂缝',
  ascension_token_owned: '持有飞升符',
  tribulation_preparation: '渡劫准备度',
  karma_debt: '因果业债',
}

// 未出现在完整映射中的 snake_case/camelCase 字段，仅在每个词段都可识别时
// 自动组合中文标签。这样新增同类状态不会直接把整段英文暴露给玩家。
const keyTokenLabels: Record<string, string> = {
  current: '当前',
  max: '最大',
  remaining: '剩余',
  cultivation: '修炼',
  ascension: '飞升',
  spirit: '灵',
  stones: '石',
  lifespan: '寿元',
  level: '等级',
  stage: '阶段',
  progress: '进度',
  breakthrough: '突破',
  attempts: '次数',
  failed: '失败',
  talent: '天赋',
  awakened: '已觉醒',
  method: '功法',
  void: '虚空',
  rift: '裂缝',
  discovered: '已发现',
  token: '符令',
  owned: '持有',
  tribulation: '天劫',
  preparation: '准备度',
  karma: '因果',
  debt: '业债',
  world: '世界',
  flags: '标记',
  is: '是否',
  has: '拥有',
}

function translateCompoundKey(key: string): string {
  const normalized = key
    .replace(/([a-z0-9])([A-Z])/g, '$1_$2')
    .toLowerCase()
    .split(/[_\-\s]+/)
    .filter(Boolean)

  if (normalized.length < 2 || normalized.some((part) => !keyTokenLabels[part])) return key
  return normalized.map((part) => keyTokenLabels[part]).join('')
}

function translateKey(key: string): string {
  return keyLabels[key] || translateCompoundKey(key)
}

const valueLabels: Record<string, string> = {
  active: '进行中',
  completed: '已完成',
  failed: '已失败',
  pending: '待处理',
  unknown: '未知',
  none: '无',
  male: '男',
  female: '女',
}

function formatValue(value: unknown): string {
  if (value === true) return '是'
  if (value === false) return '否'
  if (value === null || value === undefined || value === '') return '未设置'
  if (typeof value === 'string') return valueLabels[value.toLowerCase()] || value
  return String(value)
}

// 判断是否是物品对象（包含 name 和 description）
function isItemObject(val: any): boolean {
  return (
    val && typeof val === 'object' && !Array.isArray(val) && ('name' in val || 'description' in val)
  )
}

// 技术标识不属于玩家状态信息，避免把内部英文 slug/ID 暴露在状态栏。
const hiddenTechnicalKeys = new Set(['id', 'slug', 'internal_id', 'event_key'])

// 获取对象的可见键；物品对象优先展示名称、描述和数量。
function getObjectKeys(obj: any): string[] {
  const keys = Object.keys(obj).filter((key) => !hiddenTechnicalKeys.has(key))
  if (!isItemObject(obj)) return keys
  const order = ['name', 'description', 'quantity']
  return order.filter((k) => keys.includes(k)).concat(keys.filter((k) => !order.includes(k)))
}
</script>

<template>
  <div class="tree">
    <template v-if="typeOf(data) === 'object'">
      <div
        v-for="key in getObjectKeys(data)"
        :key="key"
        class="node"
      >
        <template v-if="typeOf(data[key]) === 'object' || typeOf(data[key]) === 'array'">
          <div class="key branch" @click="toggleExpand(String(key))">
            <span class="arrow" :class="{ collapsed: !isExpanded(String(key)) }">▼</span>
            <span>{{ translateKey(key as string) }}</span>
          </div>
          <StateTree
            v-if="isExpanded(String(key))"
            :data="data[key]"
            :depth="(depth || 0) + 1"
            :parent-path="getFullPath(String(key))"
            class="nested"
          />
        </template>
        <div v-else class="leaf">
          <span class="key">{{ translateKey(key as string) }}</span>
          <span class="val">{{ formatValue(data[key]) }}</span>
        </div>
      </div>
    </template>
    <template v-else-if="typeOf(data) === 'array'">
      <div v-if="!data.length" class="faint empty">（空）</div>
      <div v-for="(val, i) in data" :key="i" class="node">
        <StateTree
          v-if="typeOf(val) === 'object' || typeOf(val) === 'array'"
          :data="val"
          :depth="(depth || 0) + 1"
        />
        <div v-else class="leaf">
          <span class="bullet">◆</span><span class="val">{{ val }}</span>
        </div>
      </div>
    </template>
    <span v-else class="val">{{ formatValue(data) }}</span>
  </div>
</template>

<style scoped>
.tree {
  font-size: 13px;
  line-height: 1.6;
}

.nested {
  padding-left: 16px;
  border-left: 2px solid var(--border-soft);
  margin: 6px 0 10px;
}

.node {
  margin: 4px 0;
}

.key {
  color: var(--accent);
  font-weight: 600;
  letter-spacing: 0.3px;
  font-size: 13px;
}

.key.branch {
  margin: 8px 0 4px;
  cursor: pointer;
  user-select: none;
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 4px 8px;
  border-radius: 4px;
  background: var(--bg-soft);
  transition: all 0.15s ease;
}

.key.branch:hover {
  background: var(--bg-hover);
  transform: translateX(2px);
}

.arrow {
  font-size: 10px;
  transition: transform 0.2s ease;
  display: inline-block;
  color: var(--accent);
  opacity: 0.7;
}

.arrow.collapsed {
  transform: rotate(-90deg);
}

.leaf {
  display: flex;
  gap: 10px;
  align-items: baseline;
  padding: 3px 8px;
  border-radius: 3px;
  transition: background 0.15s ease;
}

.leaf:hover {
  background: var(--bg-soft);
}

.leaf .key {
  font-size: 12.5px;
  min-width: 60px;
  flex-shrink: 0;
}

.leaf .key::after {
  content: ':';
  color: var(--text-faint);
  margin-left: 4px;
  font-weight: 400;
}

.val {
  color: var(--text);
  word-break: break-word;
  font-size: 13px;
  line-height: 1.5;
}

.bullet {
  color: var(--accent);
  font-size: 8px;
  margin-right: 6px;
  opacity: 0.6;
}

.empty {
  font-size: 12px;
  color: var(--text-faint);
  font-style: italic;
  padding: 8px;
}
</style>
