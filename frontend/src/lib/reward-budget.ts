type RecordValue = Record<string, unknown>
const record = (value: unknown): RecordValue =>
  value && typeof value === 'object' && !Array.isArray(value) ? (value as RecordValue) : {}

export function outcomeRewardBudgets(
  definitions: { kind: string; content: unknown }[],
  schemas: Record<string, unknown>,
  outcomeKey: string,
  finalPoints: number,
) {
  return definitions.flatMap((definition) => {
    const annotation = record(record(schemas[definition.kind])['x-reward-budget'])
    if (
      !['stages', 'points', 'outcomes', 'total'].every((key) => typeof annotation[key] === 'string')
    )
      return []
    const content = record(definition.content)
    const outcomes = content[annotation.outcomes as string]
    if (!Array.isArray(outcomes) || !outcomes.includes(outcomeKey)) return []
    const stages = content[annotation.stages as string]
    const expected = content[annotation.total as string]
    if (!Array.isArray(stages)) return [{ text: '阶段积分配置不完整', valid: false }]
    const points = stages.map((stage) => record(stage)[annotation.points as string])
    const amounts = [...points, finalPoints, expected]
    if (!amounts.every((n) => typeof n === 'number' && Number.isSafeInteger(n) && n >= 0)) {
      return [{ text: '请填写有效的阶段、终局和总积分', valid: false }]
    }
    const stagePoints = (points as number[]).reduce((sum, points) => sum + points, 0)
    const total = stagePoints + finalPoints
    return [
      {
        text: `阶段 ${stagePoints} + 终局 ${finalPoints} = 合计 ${total} 积分（配置预算 ${expected}）`,
        valid: Number.isSafeInteger(total) && total === expected,
      },
    ]
  })
}
