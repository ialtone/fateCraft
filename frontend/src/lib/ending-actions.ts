import type { ModuleActionCapability, ModuleActionPlan } from '@/types/module-actions'

export interface EndingEntry {
  capability: ModuleActionCapability
  plan?: ModuleActionPlan
  label: string
  arguments: Record<string, string | number | boolean>
}

// Build the generic ending buttons for capabilities that the pinned
// Presentation does not already render. This keeps player-only ending actions
// reachable for packs whose campaign_view intentionally exposes fields only.
export function collectEndingEntries(
  capabilities: ModuleActionCapability[] | undefined,
  abandonAction: string | undefined,
  renderedKeys: Set<string>,
): EndingEntry[] {
  if (!capabilities?.length) return []
  const entries: EndingEntry[] = []
  for (const capability of capabilities) {
    if (!capability.available || capability.key === abandonAction) continue
    // Plan-based endings (for example the final cultivation paths) may be
    // present in the action catalog while their per-item buttons are hidden.
    // Keep them in the generic ending area so the player can still finish.
    if (renderedKeys.has(capability.key) && !capability.plans?.length) continue
    if (capability.plans?.length) {
      for (const plan of capability.plans) {
        entries.push({
          capability,
          plan,
          label: plan.label || capability.metadata?.label || capability.description,
          arguments: plan.arguments,
        })
      }
      continue
    }
    entries.push({
      capability,
      label: capability.metadata?.label || capability.description,
      arguments: {},
    })
  }
  return entries
}
