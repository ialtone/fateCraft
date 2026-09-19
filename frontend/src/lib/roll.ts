import type {
  RollBreakdown,
  RollConditionSnapshot,
  RollOutcomeDetail,
  RollOutcomeRange,
  RollResultMetadata,
} from '@/types'

const MAX_CONDITIONS = 32
const MAX_TEXT_LENGTH = 256

function record(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === 'object' && !Array.isArray(value)
    ? (value as Record<string, unknown>)
    : null
}

function finiteInteger(value: unknown): value is number {
  return typeof value === 'number' && Number.isFinite(value) && Number.isInteger(value)
}

function range(value: unknown): RollOutcomeRange | null {
  const object = record(value)
  if (!object || !finiteInteger(object.range_start) || !finiteInteger(object.range_end)) return null
  if (object.range_start < 1 || object.range_end < object.range_start) return null
  const probability =
    typeof object.probability === 'number' && Number.isFinite(object.probability)
      ? object.probability
      : undefined
  return { range_start: object.range_start, range_end: object.range_end, probability }
}

function breakdown(value: unknown): RollBreakdown | null {
  const object = record(value)
  if (!object) return null
  const fields = [
    'base',
    'attributes',
    'traits',
    'context',
    'reward_cap',
    'history',
    'difficulty_mode',
    'final',
  ]
  if (!fields.every((field) => finiteInteger(object[field]))) return null
  const values = fields.map((field) => object[field] as number)
  return {
    base: values[0],
    attributes: values[1],
    traits: values[2],
    context: values[3],
    reward_cap: values[4],
    history: values[5],
    difficulty_mode: values[6],
    final: values[7],
  }
}

function outcome(value: unknown): RollOutcomeDetail | null {
  const object = record(value)
  if (
    !object ||
    !finiteInteger(object.rolled) ||
    !finiteInteger(object.sides) ||
    !finiteInteger(object.success_rate) ||
    typeof object.success !== 'boolean' ||
    typeof object.level !== 'string'
  )
    return null
  const parsedBreakdown = breakdown(object.breakdown)
  if (!parsedBreakdown) return null
  const parsedOutcomes = record(object.outcomes)
  const successRange =
    range(object.success_range) || (parsedOutcomes ? range(parsedOutcomes.success) : null)
  if (!successRange) return null
  const outcomes = parsedOutcomes
    ? {
        critical_success: range(parsedOutcomes.critical_success) || undefined,
        success: range(parsedOutcomes.success) || undefined,
        failure: range(parsedOutcomes.failure) || undefined,
        critical_failure: range(parsedOutcomes.critical_failure) || undefined,
      }
    : undefined
  return {
    rolled: object.rolled,
    sides: object.sides,
    success_rate: object.success_rate,
    success: object.success,
    level: object.level,
    breakdown: parsedBreakdown,
    success_range: successRange,
    outcomes,
  }
}

function condition(value: unknown): RollConditionSnapshot | null {
  const object = record(value)
  if (
    !object ||
    object.public !== true ||
    typeof object.label !== 'string' ||
    typeof object.met !== 'boolean'
  )
    return null
  const expected = typeof object.display_expected === 'string' ? object.display_expected : undefined
  const actual = typeof object.display_actual === 'string' ? object.display_actual : undefined
  return {
    key: typeof object.key === 'string' ? object.key : undefined,
    label: object.label.slice(0, MAX_TEXT_LENGTH),
    display_expected: expected?.slice(0, MAX_TEXT_LENGTH),
    display_actual: actual?.slice(0, MAX_TEXT_LENGTH),
    met: object.met,
    public: true,
  }
}

/** Parse only the versioned, player-safe projection of roll metadata. */
export function parseRollResultDetails(metadata: unknown): RollResultMetadata | null {
  const object = record(metadata)
  if (!object || object.details_version !== 1) return null
  const parsedOutcome = outcome(object.roll_outcome)
  if (!parsedOutcome) return null
  const conditions = Array.isArray(object.conditions)
    ? object.conditions
        .map(condition)
        .filter((item): item is RollConditionSnapshot => item !== null)
        .slice(0, MAX_CONDITIONS)
    : []
  return {
    roll_key: typeof object.roll_key === 'string' ? object.roll_key : undefined,
    roll_title: typeof object.roll_title === 'string' ? object.roll_title : undefined,
    roll_category: typeof object.roll_category === 'string' ? object.roll_category : undefined,
    roll_outcome: parsedOutcome,
    conditions,
    details_version: 1,
  }
}
