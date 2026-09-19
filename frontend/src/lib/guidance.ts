import type {
  CampaignViewBinding,
  CampaignViewCondition,
  GuidanceGroup,
  GuidanceItem,
} from '@/types'

export interface GuidanceContext {
  state?: unknown
  snapshot?: unknown
  item?: unknown
  parent?: unknown
}

function readRelative(path: string, root: unknown): unknown {
  let current = root
  for (const part of path.split('.')) {
    if (!current || typeof current !== 'object' || Array.isArray(current)) return undefined
    current = (current as Record<string, unknown>)[part]
  }
  return current
}

export function readGuidancePath(path: string, context: GuidanceContext): unknown {
  if (!path) return undefined
  const separator = path.indexOf('.')
  const root = separator < 0 ? path : path.slice(0, separator)
  const relative = separator < 0 ? '' : path.slice(separator + 1)
  if (root === 'item') return readRelative(relative, context.item)
  if (root === 'parent') return readRelative(relative, context.parent)
  if (root === 'state') return readRelative(relative, context.state)
  if (root === 'snapshot') return readRelative(relative, context.snapshot)
  // Runtime contracts may use state paths without a prefix.  Prefer the
  // conversation state, then the campaign snapshot for backwards-compatible
  // campaign view conditions.
  return readRelative(path, context.state) ?? readRelative(path, context.snapshot)
}

export function guidanceConditionMatches(
  condition: CampaignViewCondition | undefined,
  context: GuidanceContext,
): boolean {
  if (!condition) return true
  const actual = readGuidancePath(condition.path, context)
  if (condition.exists !== undefined) return condition.exists === (actual !== undefined && actual !== null)
  if (condition.equals !== undefined) return String(actual) === String(condition.equals)
  if (condition.not_equals !== undefined) return String(actual) !== String(condition.not_equals)
  if (condition.operator && condition.value !== undefined) {
    const number = Number(actual)
    const expected = Number(condition.value)
    if (!Number.isFinite(number) || !Number.isFinite(expected)) return false
    switch (condition.operator) {
      case 'gte': return number >= expected
      case 'gt': return number > expected
      case 'lte': return number <= expected
      case 'lt': return number < expected
      case 'eq': return number === expected
      default: return false
    }
  }
  return false
}

export function resolveGuidanceArguments(
  bindings: Record<string, CampaignViewBinding> | undefined,
  context: GuidanceContext,
): Record<string, unknown> {
  const args: Record<string, unknown> = {}
  for (const [name, binding] of Object.entries(bindings ?? {})) {
    if (binding.from) args[name] = readGuidancePath(binding.from, context)
    else if (binding.value !== undefined) args[name] = binding.value
  }
  return args
}

export interface ResolvedGuidanceGroup {
  group: GuidanceGroup
  items: GuidanceItem[]
}

export function resolveGuidanceGroups(
  groups: GuidanceGroup[] | undefined,
  context: GuidanceContext,
  placement?: string | string[],
): ResolvedGuidanceGroup[] {
  return (groups ?? [])
    .filter((group) => {
      if (!placement || !group.placement) return true
      return Array.isArray(placement) ? placement.includes(group.placement) : group.placement === placement
    })
    .map((group) => {
      const items = group.items
        .filter((item) => guidanceConditionMatches(item.visible_when, context))
        .sort((left, right) => left.key.localeCompare(right.key))
        .slice(0, group.max_items && group.max_items > 0 ? group.max_items : undefined)
      return { group, items }
    })
    .filter(({ items }) => items.length > 0)
}
