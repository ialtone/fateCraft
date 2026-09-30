# 21. Campaign 终局积分

## 固定版本、固定积分

Campaign 的成长、库存、装备和市场属于单个存档。账号收益只取固定版本结局的 `reward_points`，不读取财富倍率或终局库存来决定积分，也不会为了账号收益额外扣除来源物。正常任务交付、消耗和局内奖励仍由原领域动作执行。

带有 `campaign_outcome_key` 的终局任务经过后端目标核验后，在原 Campaign 动作事务内完成任务结算、积分发放、终局事件与 Campaign 完成。模型只能提出动作，不能提交积分金额。

`pack.json` 中示例：

```json
{
  "outcomes": [{
    "outcome_key": "world_warden",
    "category": "success",
    "title": "执掌天关",
    "tone": "success",
    "verifier_key": "core.always",
    "verifier_config": {},
    "reward_points": 1000
  }]
}
```

此处 `core.always` 不替代终局任务的领域核验。发布时要求终局任务引用当前版本已启用结局，正积分结局必须有真实终局任务。金额为非负安全整数，缺省或 0 表示不发积分，完成事实仍保存 `reward_points: 0`。

积分值进入内容哈希并随版本复制；编辑已发布内容需要创建新草稿。青河修仙志的“执掌天关”和“携律远行”初始各 1000 积分，数值维护在内置 `pack.json`。

## 事务与幂等

`GrantCampaignOutcomeRewards` 使用当前动作的 GORM 事务调用统一 `GrantInCurrentTx`。来源固定为 `campaign_outcome`，`source_id` 为 Campaign ID，幂等键为 `campaign:{campaign_id}:points`。metadata 保存结局 key 与固定版本 ID。

Campaign 行锁、动作提交记录、来源唯一约束和账本幂等共同防止重复终局。更换动作请求 ID 或重复调用同一终局不能重新发分。余额、账本、领域变更、事件与 Campaign 完成中任何一步失败，全部回滚。

0 积分不产生账本，正常终局结果和 `campaign.completed` 事件仍保存实际积分。完成态重新加载从真实账本恢复收益，不从聊天、Memory、自由状态或模型上下文推断。

## 玩家投影

平台在玩法 Projection Provider 返回后追加保留字段 `outcome_rewards`、`reward_points`，并按路由追加 `rewards_disabled_reason`；Provider 不能覆盖这些字段。

```json
{
  "reward_points": 1000,
  "outcome_rewards": [{
    "key": "fatecraft.points",
    "name": "积分",
    "unit": "分",
    "icon": "sparkles",
    "quantity": 1000,
    "balance": 1300
  }]
}
```

`balance` 为入账后的余额快照，当前总余额以账号余额接口为准。没有正积分流水时列表为空，`reward_points` 为 0。通用 Campaign 视图在完成态显示本次所得；玩家奖励页汇总账号余额、兑换订单和收支。

## 直连边界

`client` 路由的任务与终局照常核验和完成，存储层直接跳过通关入账。结果、完成事件和玩家快照包含 `rewards_disabled_reason=client_llm_route`，界面显示“客户端直连模式不发放奖励”。正常任务消耗继续生效。

后续排行、积分统计和解锁不得把直连结局当成可信积分来源。注册赠送、管理员发放与已有合法余额依然可用。

## 维护与验证

| 关注点 | 入口 |
| --- | --- |
| 版本校验与内容哈希 | `backend/internal/challenge/service.go` |
| 事务入账与完成态加载 | `backend/internal/storage/campaign_domain.go` |
| 可信终局任务与事件 | `backend/internal/gameplay/cultivation/campaignactions/world_actions.go` |
| 平台保留字段 | `backend/internal/campaign/service.go` |
| 完成态显示 | `frontend/src/components/CampaignViewRenderer.vue` |

真实环境应验证两条内置结局、0 积分、直连、重复终局、事务失败回滚以及局内库存未被奖励层额外扣除。环境验收仍由用户执行，完整分工和检查表见 [22 奖励管理](22-reward-management-and-exchange.md)。积分兑换和误发撤回只操作账号层，不重开 Campaign；只有原管理员发放可撤回，终局积分没有管理员通用冲正入口。
