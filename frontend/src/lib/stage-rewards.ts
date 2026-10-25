import type { StageProgress } from '@/types/endings'

// 局中阶段提示：这些阶段只是已记录在当前分支，终局才统一结算，不能提示“到账”。
export function stageProgressNotice(stages: StageProgress[] | undefined): string {
  return (stages ?? [])
    .map((stage) =>
      stage.disabled_reason === 'client_llm_route'
        ? `${stage.name}（客户端直连不发积分）`
        : stage.configured_points > 0
          ? `${stage.name}（${stage.configured_points} 积分待结算）`
          : `${stage.name}（本版本配置为零分）`,
    )
    .join('、')
}
