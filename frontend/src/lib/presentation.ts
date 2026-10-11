import type { RuntimePresentation } from '@/types'

const technicalValuePattern = /^[a-z][a-z0-9._:-]*$/i

/** Resolve a gameplay value using the presentation metadata of the pinned runtime. */
export function presentValue(
  presentation: RuntimePresentation | null | undefined,
  namespace: string,
  value: unknown,
  fallback = '未标注',
): string {
  if (value === undefined || value === null || value === '') return fallback
  const text = String(value).trim()
  const labels = presentation?.value_labels?.[namespace]
  if (labels && Object.prototype.hasOwnProperty.call(labels, text)) return labels[text]
  return technicalValuePattern.test(text) ? fallback : text
}

export function presentKey(
  presentation: RuntimePresentation | null | undefined,
  namespace: string,
  value: unknown,
  fallback = '未标注',
): string {
  return presentValue(presentation, namespace, value, fallback)
}

const platformLabels: Record<string, Record<string, string>> = {
  reward_kind: {
    currency: '货币',
    credit: '额度',
    item: '物品',
    badge: '徽章',
    entitlement: '权限',
  },
  reward_source: {
    challenge_run: '挑战结算',
    campaign_outcome: '长期世界终局',
    campaign_stage: '长期世界阶段',
    user_registration: '注册赠送',
    admin_grant: '管理员发放',
    admin_reversal: '管理员冲正',
    message_rewrite_refund: '改写失败返还',
    challenge_settlement: '挑战结算',
    message_rewrite: '剧情改写',
    registration: '新用户赠送',
  },
  reward_operation: {
    grant: '获得',
    consume: '使用',
    refund: '返还',
    reverse: '冲正',
  },
}

export function platformValue(namespace: string, value: unknown, fallback = '其他') {
  const text = value === undefined || value === null ? '' : String(value)
  return platformLabels[namespace]?.[text] || fallback
}
