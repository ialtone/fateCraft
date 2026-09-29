import type { RewardRecord } from '@/types/rewards'
export function rewardStatus(record: RewardRecord) {
  if (record.reversal_ledger_id) return '已冲正'
  if (record.status === 'skipped')
    return record.skip_reason === 'already_owned' ? '已拥有，未重复发放' : '数量为零，未发放'
  return (
    { pending: '待领取', granted: '已到账', cancelled: '已撤销' }[record.status] || record.status
  )
}
export function rewardSource(record: RewardRecord) {
  if (record.source_type === 'admin_grant')
    return record.purpose === 'compensation' ? '管理员补偿' : '管理员赠送'
  return (
    (
      {
        challenge_run: '挑战结算',
        campaign_outcome: '长期世界终局',
        user_registration: '注册赠送',
        reward_exchange: '积分兑换',
      } as Record<string, string>
    )[record.source_type] || '奖励'
  )
}
export function rewardError(error: unknown) {
  return (error as { message?: string })?.message || '请求失败，请稍后重试'
}
