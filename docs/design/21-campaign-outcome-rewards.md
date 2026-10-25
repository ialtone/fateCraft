# 21. 阶段与终局积分

局内灵石、材料和装备属于各自存档，账号积分来自固定版本声明的奖励，二者不互相折算。最终行为与本地验证见 [25 核心玩法验证](25-core-gameplay-validation.md)，数据库真实验收仍由维护者执行。

## 内置成功结局定价

内置玩法只给成功结局配置正积分，失败、中性、撤离、超时与主动终止一律为 0。首版价格如下；具体 outcome key 以固定版本为准：

| 玩法 | 模式 | 成功结局积分 | 阶段积分 |
| --- | --- | ---: | ---: |
| 荒岛求援 | 单局 | 150 | 25 + 25 + 25 |
| 失落墓室 | 单局 | 150 | 25 + 25 + 25 |
| 夜入封锁区 | 单局 | 200 | 30 + 30 + 40 |
| 地下哨塔 | 单局 | 200 | 30 + 30 + 40 |
| 小镇旧约 | Campaign | 100 / 100（两个互斥成功结局） | 15 + 15 + 20 |
| 山间客栈 | Campaign | 300 | 50 + 50 + 50 |
| 青岚宗重建 | Campaign | 500 | 75 + 75 + 100 |
| 百年飞升劫 | 单局 | 仙缘 150 / 虚空 200 / 正统 300 | 25 + 25 + 25 |
| 青河修仙志 | Campaign | 700 / 700 | 100 + 100 + 100 |

100 分约等于一次剧情改写，是兑换锚点，不是时薪承诺。新发布版本可以调价；已有对话仍按固定版本发放，已结算回执不追溯修改。

## 最终结算口径

`本局实际积分 = 已达成阶段的实际结算积分 + 成功结局的终局积分`

- 成功：结算当前分支已达成的全部阶段，再加所选成功结局的终局积分。
- 失败、死亡、寿尽、超时、撤离、主动终止：只结算已达成阶段，终局积分为 0。
- 未达成任何阶段即终止：0 分。
- 局中只记录进度，不写账本、不增加余额、不创建领取回执；暂停、断线、取消生成和普通删除都不触发结算。
- 新建一局是新的结算作用域，可以重新获得奖励；不设 24 小时限领窗口。

## 固定版本预算

通用 Campaign 终局使用 `pack.json.outcomes[].reward_points`，缺省或 0 不发积分。经营、关系和宗门模块只提交合法完成事实，平台在原事务读取当前固定版本金额。

阶段使用平台定义 `challenge.progress_rewards`，Challenge 与 Campaign 共用；青河保留领域定义 `cultivation.progress_rewards`。九包阶段均为固定版本声明，金额与可信条件如下：

| 玩法 / 包 | 阶段（固定顺序） | 条件来源 |
| --- | --- | --- |
| 荒岛求援 / `island-rescue` | 探索周边 25、备齐求援材料 25、完成求援装置 25 | `explored_count ≥ 2`、`materials_ready`、`signal_ready` |
| 失落墓室 / `lost-chambers` | 深入墓道 25、解除关键机关 25、取得墓印 25 | `visited_count ≥ 2`、`resolved_count ≥ 1`、`goal_items ≥ 1` |
| 夜入封锁区 / `night-blockade` | 突破外围 30、取得关键情报 30、取得任务目标 40 | `discovered_count ≥ 2`、`has_intel`、`has_target` |
| 地下哨塔 / `underground-watchtower` | 探索哨塔 30、击败塔内守卫 30、击败指挥官 40 | `visited_count ≥ 2`、`defeated_count ≥ 1`、`boss_defeated` |
| 小镇旧约 / `old-town-promise` | 倾听坊间往事 15、回应核心关系事件 15、推进小镇旧约 20 | `fact_count ≥ 1`、`event_choice_count ≥ 1`、`event_choice_count ≥ 2` |
| 山间客栈 / `mountain-inn` | 恢复经营 50、完成实质经营目标 50、形成稳定经营成果 50 | `delivered ≥ 1`、`delivered ≥ 2`、`facility_level_sum ≥ 3` |
| 青岚宗重建 / `qinglan-sect` | 恢复基础 75、完成关键建设或弟子目标 75、取得重建里程碑 100 | `facility_level_sum ≥ 1`、`disciple_count ≥ 4` 或 `facility_level_sum ≥ 3`、`successful_missions ≥ 1` |
| 百年飞升劫 / `reincarnation-trials` | 首次结丹 25、首次元婴 25、首次化神 25 | `realm_key` 达到对应境界或更高 |
| 青河修仙志 / `mortal-path` | 青河立足 100、星海立业 100、守界有成 100 | 境界、主线任务与真实遭遇胜利，见 [16](16-cultivation-campaign-agent-reference.md) |

阶段预算上限为最低正积分成功结局金额的 50%（向下取整）；有多个互斥成功结局时不求和。青河保留三个阶段各 100 与两个 700 终局，`cultivation.progress_rewards.total_points` 继续按 1000 配平。发布时要求 `challenge.progress_rewards.stage_cap` 正好等于该上限，阶段总额不得超过它。

发布验证阶段 key 唯一、金额与顺序合法、verifier 受信、Challenge 条件字段在 state schema 中由 module/system/immutable 维护、Campaign 条件路径属于玩家可见的 Campaign 存档投影，并拒绝模型可写字段与终局字段。后台依据模块 Schema 的 `x-reward-budget` 元数据显示同样的预算关系，错误配置仍由后端最终拒绝。

## 局中记录

阶段进度属于当前分支的玩法状态，不进入账号资产：

- 单局保存在 `conversation_states.state._platform.stage_progress`，随 `state_revisions` 的 `AfterState` 一起恢复；模型、客户端和模块 patch 不能写该平台保留区。
- Campaign 保存在 `campaign_stage_progress`，唯一键 `(campaign_id, stage_key)`，并纳入 `campaignCheckpointTables`、`CampaignStateHash` 和检查点捕获/恢复。
- 进度只保存阶段 key、来源回合/动作、来源分支和达成时间；名称与金额始终从固定版本读取，前端不能提交可发分数值。

非终局动作在原有事务内依次执行：保存可信状态 → 核验阶段 → 合并新增进度 → 保存动作结果。进度随分支回档；当前分支内一旦合法达成，后续消耗、属性下降或死亡不会撤销它。

## 终局结算

在同一事务内依次完成：

1. 校验请求、版本、所有权、分支和修订，按既有顺序获取对话/Campaign 锁。
2. 执行最后一个可信动作，合并该动作中新达成的阶段。
3. 读取当前分支进度，按固定版本生成本局阶段应发金额。
4. 写阶段领取回执；正积分通过统一账本入账；成功结局另发终局积分。
5. 保存结算标记、玩法与对话终态、完整 CompletionView 及原动作幂等结果。
6. 提交后才发送 WebSocket 事件与最终到账通知。

阶段与死亡发生在同一动作时，以动作内可信转换的先后事实判定：同一动作完成的目标不能因末尾生命归零而丢失，死亡也不能替代尚未完成的目标。成功、失败、超时、撤离、主动终止和模型工具终局都走同一结算入口。

## 回执、幂等与回档

`challenge_stage_rewards` 与 `campaign_stage_rewards` 是终局才写入的不可变结算回执：

- 单局唯一键 `(conversation_id, stage_key)`，账本来源 `challenge_stage`，来源 ID `<conversationID>:<stageKey>`，幂等键 `challenge-stage:<conversationID>:<stageKey>`。
- Campaign 唯一键 `(campaign_id, stage_key)`，账本来源 `campaign_stage`，来源 ID `campaignID:stageKey`。
- 0 分与客户端直连仍写实际为 0、原因 `client_llm_route` 的回执，不生成 0 元账本。
- 回执的 `granted_at` 是真实结算时间；阶段达成来源回合与来源分支单独保存。
- 局级结算标记是总保护，唯一键与账本幂等键是补充保护；重复请求、并发终局、重放和重开页面至多结算一次。
- 已经结算时返回原持久化回执，不从当前状态重算，不因前端缓存缺失而补发。

进度随分支回档，回执与账本不随回档撤回。回档到阶段之前时该阶段恢复为未达成；旧分支达到过的阶段不能累加到新分支；已经结算的终局与阶段同局不能再次领取。

终局积分只对 `category=success` 发放。`failure` 和 `neutral` 仍保存结局键、分类和标题，但终局积分为 0；`dead`、`abandoned` 与 `completed` 都可以是已结算状态。

## 玩家展示与交互

平台在玩法投影之后追加保留字段：

| 字段 | 含义 |
| --- | --- |
| `stage_progress` | 当前分支已达成、尚未结算的阶段（key、name、configured_points、达成信息） |
| `pending_stage_points` | 上述阶段的配置积分合计；只是预期值，不能兑换 |
| `stage_rewards` | 最终结算产生的阶段回执；结算前为空 |
| `stage_reward_points` | 已实际结算的阶段积分；结算前为 0 |
| `reward_points` | 已实际发放的终局积分 |
| `total_reward_points` | 实际阶段积分 + 实际终局积分 |
| `outcome_rewards` | 终局正积分流水对应的账号奖励视图 |
| `rewards_disabled_reason` | 如 `client_llm_route`，说明为何没有实际积分 |

`rewards` 仍只是终局账号奖励明细，客户端用 `stage_reward_points + reward_points`，不得把 `rewards` 内的积分重复计入。局中提示只显示“阶段已达成，结束时结算”，不弹“获得积分”，也不刷新出虚假余额；终局窗口显示“阶段积分 + 终局积分 = 本局实际所得”，失败也显示阶段明细。

青河面板区分当前阶段目标、尚未结算进度、已结算阶段、终局所得和整局累计。账号当前总余额以余额接口为准；回执里的 balance 是当时余额快照。

## 直连与管理边界

客户端直连照常执行任务、阶段与终局验证，但实际积分为 0：进度照常记录，终局写 0 分回执并标记 `client_llm_route`，不产生玩法账本。注册赠送、管理员发放、已有合法余额和积分兑换仍沿用账号层规则。

管理员误发撤回仅针对可撤回的管理员发放，不提供任意阶段或终局积分冲正。兑换只操作账号余额和订单，不改变 Campaign 任务、物品或结局。完整账号管理协议见 [22 奖励管理](22-reward-management-and-exchange.md)。

## 验证入口

- `challengeimport/progress_rewards_test.go`：九包阶段定义、金额、预算与青河领域定义。
- `challenge/progress_rewards_publish_test.go`、`gameplay/contract/progress_rewards_test.go`：平台定义结构、50% 预算、非成功结局零积分与 verifier 校验。
- `challenge/stage_rewards_test.go`：只按已记录进度生成回执金额，进度视图只报告新达成阶段。
- `campaignactions/progress_test.go`、`playthrough_test.go`：局中记录、终局一次结算、直连 0 分、死亡不回退进度与真实路线。
- `storage/challenge_stage_reward_postgres_test.go`、`storage/campaign_stage_reward_postgres_test.go`：并发唯一性、事务回滚、重放、直连 0 分与只追加保护。
- `challengeimport/module_acceptance_test.go`：九包真实导入、发布与运行时接受。

PostgreSQL 用例需要实际 `FATECRAFT_TEST_DATABASE_URL`；未配置时跳过，不能把跳过报告为事务验证通过。新项目表结构并入 `00001_init.sql`，没有 `00002` 或启动时补表。

## 主动退出与完成窗口

玩家主动终止或撤离会结算当前分支已达成的阶段，不发终局积分；不以修为、时间或资源兑换离场积分。实际阶段积分与终局积分在统一 CompletionView 中分别展示，读取结果不会重新发分。完整协议见 [26 玩法结局生命周期](26-gameplay-ending-lifecycle.md)。

## 玩法包交换与版本管理

阶段与终局都按 Campaign/Challenge 原固定版本结算；账本记录玩法包、实际版本/哈希、游戏与阶段/结局展示快照。新版入口不改变原预算；已结算回执和原结果保留。

完整协议见 [27 玩法包交换与版本生命周期](27-gameplay-package-exchange-and-versions.md)。
