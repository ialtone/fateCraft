type RecordValue = Record<string, unknown>
const record = (value: unknown): RecordValue =>
  value && typeof value === 'object' && !Array.isArray(value) ? (value as RecordValue) : {}

// Single-run challenges declare a relative cap derived from the lowest positive
// success outcome; Campaigns declare an absolute total. A stage definition
// carries exactly one of the two.
export function outcomeRewardBudgets(
  definitions: { kind: string; content: unknown }[],
  schemas: Record<string, unknown>,
  outcomeKey: string,
  finalPoints: number,
) {
  return definitions.flatMap((definition) => {
    const annotation = record(record(schemas[definition.kind])['x-reward-budget'])
    const hasTotal = typeof annotation.total === 'string'
    const hasCap = typeof annotation.cap === 'string'
    if (
      !['stages', 'points', 'outcomes'].every((key) => typeof annotation[key] === 'string') ||
      (!hasTotal && !hasCap)
    )
      return []
    const content = record(definition.content)
    const outcomes = content[annotation.outcomes as string]
    if (!Array.isArray(outcomes) || !outcomes.includes(outcomeKey)) return []
    const stages = content[annotation.stages as string]
    if (!Array.isArray(stages)) return [{ text: '阶段积分配置不完整', valid: false }]
    const points = stages.map((stage) => record(stage)[annotation.points as string])
    if (!points.every((n) => typeof n === 'number' && Number.isSafeInteger(n) && n >= 0)) {
      return [{ text: '请填写有效的阶段积分', valid: false }]
    }
    const stagePoints = (points as number[]).reduce((sum, points) => sum + points, 0)
    if (hasCap) {
      const cap = content[annotation.cap as string]
      if (typeof cap !== 'number' || !Number.isSafeInteger(cap) || cap < 0) {
        return [{ text: '请填写有效的阶段积分上限', valid: false }]
      }
      return [
        {
          text: `阶段 ${stagePoints} 积分（上限 ${cap}），终局 ${finalPoints} 积分单独结算`,
          valid: stagePoints <= cap,
        },
      ]
    }
    const expected = content[annotation.total as string]
    if (typeof expected !== 'number' || !Number.isSafeInteger(expected) || expected < 0) {
      return [{ text: '请填写有效的阶段、终局和总积分', valid: false }]
    }
    const total = stagePoints + finalPoints
    return [
      {
        text: `阶段 ${stagePoints} + 终局 ${finalPoints} = 合计 ${total} 积分（配置预算 ${expected}）`,
        valid: Number.isSafeInteger(total) && total === expected,
      },
    ]
  })
}
