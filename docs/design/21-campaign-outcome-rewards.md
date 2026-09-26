# 21. Campaign 终局带出与账号奖励

> 本文描述当前已经实现的通用 Campaign 终局闭环。它不属于修仙模块；破界、撤离、远征归来、赛季结算等题材都使用同一平台协议。

## 1. 核心语义

Campaign 中的库存、装备和角色成长属于单个长期存档，账号奖励属于玩家账号。两者不能直接共用余额，也不能由模型宣告互换。

玩法包通过版本化 `challenge_reward_rules` 声明终局奖励。带有 `campaign_outcome_key` 的终局任务通过后端目标核验后，平台在同一 Campaign 动作事务内依次完成任务结算、终局确认、账号奖励计算、局内来源扣除、不可变账本写入和 Campaign 完成。任一步失败都会整体回滚。

当前结算是规则驱动的自动带出：所有满足条件的启用规则都会执行，不要求玩家在终局时手动挑选。玩家或模型都不能提交奖励数量。

## 2. 支持的计算器

### `core.fixed`

授予固定正整数，适合徽章、通关凭证和固定货币：

~~~json
{
  "calculator_key": "core.fixed",
  "calculator_config": {"amount": 1}
}
~~~

不可叠加奖励已经拥有时不会重复入账，也不会阻止 Campaign 完成。

### `campaign.extract_item_stack`

把终局结算时实际存在的局内堆叠物按有界比例转为账号资源：

~~~json
{
  "calculator_key": "campaign.extract_item_stack",
  "calculator_config": {
    "item_key": "world-fragment",
    "quality_key": "common",
    "source_quantity": 3,
    "reward_quantity": 2,
    "maximum_reward_quantity": 12,
    "consume": true
  }
}
~~~

- `source_quantity`：每批需要的局内数量；
- `reward_quantity`：每批授予的账号数量；
- `maximum_reward_quantity`：单个 Campaign 对该规则的最大授予量，必须是每批奖励的整数倍；
- `consume=true`：只扣除实际参与兑换且未超过上限的来源物，余数和超出上限部分保留；
- `consume=false`：只按局内事实估值，不扣除来源物；
- 数量不足一批时跳过该规则，不视为结算失败。

终局任务目标消耗和任务局内奖励先执行，提取规则读取两者完成后的权威库存。例如终局任务先交付 6 个碎片、再奖励 1 个碎片，带出计算看到的是交付和任务奖励后的数量。

## 3. 发布约束

发布校验必须同时满足：

- Runtime 是 `campaign`，且所选可信模块明确允许该计算器；
- 奖励绑定的 outcome 被当前版本中的真实终局任务引用；
- `item_key` 引用当前固定版本中的可堆叠 `item`；
- 目标账号奖励是可叠加定义；
- 所有 key、整数、布尔值和上限合法，未知配置字段直接拒绝；
- 同一 outcome 不能配置两条规则读取相同 item key 和 quality，以免规则竞争同一来源；
- 同一 outcome 不能对同一账号奖励定义重复绑定规则。

平台不会执行玩法包携带的脚本、表达式或 SQL。新增其他来源类型必须先实现可信计算器和存储事务，再由模块 descriptor 显式开放。

## 4. 事务、幂等与审计

每笔实际授予使用稳定幂等键：

~~~text
campaign:{campaign_id}:outcome:{outcome_key}:{reward_key}
~~~

即使终局动作以不同 action ID 再次到达，账号余额、来源扣除和账本都只发生一次。账本 `source_type=campaign_outcome`、`source_id=campaign_id`，metadata 固定记录：

- outcome key 与 challenge version ID；
- 奖励 key、类型、名称、单位和图标快照；
- 提取来源 key、名称、品质、结算前数量和实际扣除数量。

`reward_ledger` 只追加；余额是账本的当前投影。Campaign 加载完成态时按账本恢复本次带出列表，不依赖聊天消息、Memory 或模型上下文。

## 5. 玩家快照

平台在玩法 Projection Provider 返回后追加保留字段 `outcome_rewards`，Provider 不能覆盖。完成态示例：

~~~json
{
  "outcome_rewards": [
    {
      "key": "some-pack.world-essence",
      "kind": "currency",
      "name": "世界精粹",
      "unit": "份",
      "icon": "sparkles",
      "quantity": 4,
      "balance": 11,
      "source": {
        "type": "item_stack",
        "key": "world-fragment",
        "name": "世界碎片",
        "quality_key": "common",
        "available_quantity": 7,
        "consumed_quantity": 6
      }
    }
  ]
}
~~~

前端终局状态展示本次带出数量、来源转换和当前账号余额。普通奖励页继续展示跨 Campaign 的总余额与账本。

## 6. 当前边界

当前提取来源只支持堆叠物，不直接携带局内装备实例、角色状态或任意 JSON。需要把法宝、队员、科技蓝图等带出时，应先定义明确的账号奖励语义和可信计算器，不能把整个 Campaign 领域行复制到账户层。

当前没有终局手动选择、容量格或多来源配方。若玩法需要“在若干战利品中选 N 件撤离”，应增加独立的终局选择状态和服务端提交动作，并继续复用相同账本与最终事务，不应把选择权交给 Prompt。

## 7. 关键入口

| 关注点 | 入口 |
| --- | --- |
| 配置解析与有界计算 | `backend/internal/campaignreward/rules.go` |
| 计算器注册与模块白名单 | `backend/internal/gameplay/registry.go`、对应模块 descriptor |
| 玩法包发布校验 | `backend/internal/challenge/service.go` |
| 事务内扣除、授予和历史加载 | `backend/internal/storage/campaign_domain.go` |
| 终局任务调用 | `backend/internal/gameplay/cultivation/campaignactions/world_actions.go` |
| 平台 Snapshot 保留字段 | `backend/internal/campaign/service.go` |
| 玩家终局展示 | `frontend/src/components/CampaignViewRenderer.vue` |

## 8. 客户端直连不带出

`client` 路由的 Campaign 照常完成任务与终局（状态、事件、结局展示不变），但 `GrantCampaignOutcomeRewards` 在事务内直接返回空：不计算奖励、不扣来源物、不写账本。结果与 `campaign.completed` 事件、`outcome_rewards` 快照附 `rewards_disabled_reason=client_llm_route`，前端据此显示“客户端直连模式不发放奖励”替代奖励列表。今后任何跨 Campaign 读取终局奖励（统计、排行、成就、解锁）必须排除 `client` 路由对象。
