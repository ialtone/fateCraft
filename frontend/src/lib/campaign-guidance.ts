// Platform fallback for Campaign action hints. The server guidance proposal is
// optional and model-authored; when the model does not present one the player
// still needs a visible next step. These options are plain player intents, not
// bound capabilities: submitting one goes through the normal message path and
// the server revalidates every fact before committing anything.
export interface FallbackGuidanceItem {
  key: string
  label: string
  message: string
}

type RecordValue = Record<string, unknown>

function record(value: unknown): RecordValue | null {
  return value && typeof value === 'object' && !Array.isArray(value) ? (value as RecordValue) : null
}

function items(value: unknown): RecordValue[] {
  return Array.isArray(value) ? value.map(record).filter((item): item is RecordValue => !!item) : []
}

function text(value: unknown): string {
  return typeof value === 'string' ? value.trim() : ''
}

function readable(name: string): boolean {
  return !!name && !/^[a-z][a-z0-9._:-]*$/i.test(name)
}

export function campaignFallbackGuidance(snapshot: unknown, limit = 4): FallbackGuidanceItem[] {
  const root = record(snapshot)
  if (!root) return []
  const out: FallbackGuidanceItem[] = []
  const seen = new Set<string>()
  const add = (item: FallbackGuidanceItem) => {
    if (out.length >= limit || seen.has(item.label) || seen.has(item.message)) return
    seen.add(item.label)
    seen.add(item.message)
    out.push(item)
  }

  for (const encounter of items(root.encounters)) {
    if (text(encounter.status) !== 'active') continue
    const name = text(encounter.opponent_name)
    add({
      key: 'fallback:encounter',
      label: name ? `应对${name}` : '应对当前遭遇',
      message: name ? `我继续应对${name}。` : '我继续应对当前遭遇。',
    })
    break
  }

  for (const quest of items(root.quests)) {
    if (quest.can_submit !== true) continue
    const name = text(quest.name)
    if (!readable(name)) continue
    const label =
      text(quest.quest_type) === 'obligation' ? `履行承诺 · ${name}` : `提交委托 · ${name}`
    add({ key: `fallback:quest-submit:${text(quest.key)}`, label, message: `我提交「${name}」。` })
  }

  for (const quest of items(root.quests)) {
    if (quest.can_accept !== true) continue
    const name = text(quest.name)
    if (!readable(name)) continue
    const label =
      text(quest.quest_type) === 'obligation' ? `接受承诺 · ${name}` : `接受委托 · ${name}`
    add({ key: `fallback:quest-accept:${text(quest.key)}`, label, message: `我接受「${name}」。` })
  }

  const world = record(root.world)
  const currentLocation = text(record(world?.current)?.key)
  for (const relation of items(root.relationships)) {
    const name = text(relation.name)
    if (!readable(name)) continue
    if (text(relation.status) && text(relation.status) !== 'active') continue
    if (currentLocation && text(relation.location_key) !== currentLocation) continue
    add({
      key: `fallback:talk:${text(relation.instance_key)}`,
      label: `与${name}交谈`,
      message: `我与${name}交谈。`,
    })
  }

  for (const route of items(world?.available_routes)) {
    if (route.accessible === false) continue
    const name = text(route.name)
    if (!readable(name)) continue
    add({
      key: `fallback:travel:${text(route.location_key)}`,
      label: `前往${name}`,
      message: `我前往${name}。`,
    })
  }

  for (const plan of items(root.training)) {
    if (plan.available !== true) continue
    const name = text(plan.name)
    if (!readable(name)) continue
    add({
      key: `fallback:training:${text(plan.key)}`,
      label: `按「${name}」修行`,
      message: `我按「${name}」修行。`,
    })
  }

  return out
}

// campaign_view.actions with a source_path are entity-bound: the campaign
// panel already renders one button per quest/NPC/location/technique. The
// capability panel must not render the same (action_key, entity) again.
export function entityBoundActionKeys(
  actions: Array<{ key?: unknown; source_path?: unknown }> | undefined,
): Set<string> {
  const keys = new Set<string>()
  for (const action of actions ?? []) {
    const key = text(action?.key)
    if (key && text(action?.source_path)) keys.add(key)
  }
  return keys
}

export function withoutEntityBoundActions<T extends { key: string }>(
  capabilities: T[] | undefined,
  actions: Array<{ key?: unknown; source_path?: unknown }> | undefined,
): T[] {
  const hidden = entityBoundActionKeys(actions)
  if (!hidden.size) return capabilities ?? []
  return (capabilities ?? []).filter((capability) => !hidden.has(capability.key))
}
