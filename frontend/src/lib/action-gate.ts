// One availability decision shared by the campaign section buttons, the action
// panel, ending buttons and the guidance bar. Every caller must surface the
// returned message instead of silently returning.
export type ActionBlockCode = 'ended' | 'result_pending' | 'generating' | 'syncing' | 'conflict'

export interface ActionBlock {
  code: ActionBlockCode
  message: string
}

export interface ActionGateInput {
  /** The conversation or campaign is no longer active. */
  ended?: boolean
  /** The WebSocket can no longer write (exhausted/access denied). */
  connectionBlocked?: boolean
  /** A previous action was submitted but its result is not confirmed yet. */
  resultPending?: boolean
  /** Authoritative server lease or the local optimistic generation lock. */
  generating?: boolean
  /** Loading, gameplay-version check or a blocked connection. */
  syncing?: boolean
  /** The local snapshot revision no longer matches the expected revision. */
  conflict?: boolean
}

const MESSAGES: Record<ActionBlockCode, string> = {
  ended: '本局已结束，无法继续行动。',
  result_pending: '上一次动作结果尚未确认，请先查询后再操作。',
  generating: '正在生成叙事，请等待本轮完成。',
  syncing: '状态同步中，请稍候。',
  conflict: '状态已变化，正在同步，请稍后再试。',
}

export function actionBlock(input: ActionGateInput): ActionBlock | null {
  if (input.ended) return { code: 'ended', message: MESSAGES.ended }
  if (input.connectionBlocked) return { code: 'syncing', message: '连接已中断，请刷新页面后重试。' }
  if (input.resultPending) return { code: 'result_pending', message: MESSAGES.result_pending }
  if (input.generating) return { code: 'generating', message: MESSAGES.generating }
  if (input.syncing) return { code: 'syncing', message: MESSAGES.syncing }
  if (input.conflict) return { code: 'conflict', message: MESSAGES.conflict }
  return null
}
