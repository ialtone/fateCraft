import type { RewardKey } from '@/types/rewards'
export const POINTS: RewardKey = 'fatecraft.points'
export const REWRITE_CREDITS: RewardKey = 'fatecraft.message-rewrite-credit'
export const rewardOptions = [
  { label: '积分', value: POINTS },
  { label: '剧情改写次数', value: REWRITE_CREDITS },
]
export function rewardName(key: string) {
  return rewardOptions.find((o) => o.value === key)?.label || '未知项目'
}
export function rewardSource(source: string) {
  return (
    (
      {
        challenge_run: '挑战通关',
        campaign_outcome: '长期世界终局',
        user_registration: '注册赠送',
        admin_grant: '管理员发放',
        admin_reversal: '撤回管理员发放',
        reward_exchange: '积分兑换',
        message_rewrite: '剧情改写',
        message_regenerate: '重新生成',
        message_rewrite_refund: '改写失败返还',
      } as Record<string, string>
    )[source] || source
  )
}
export function rewardError(error: unknown) {
  return (error as { message?: string })?.message || '请求失败，请稍后重试'
}
