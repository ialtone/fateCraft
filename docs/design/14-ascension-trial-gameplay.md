# 14. 百年飞升劫玩法规格

本文是内置单局挑战 `reincarnation-trials`（显示名“百年飞升劫”）的正式玩法说明，面向内容设计、后端实现、测试和后续平衡调整。当前内容基线为 `1.2.0`。

本玩法与长期世界“青河修仙志”只共享九阶境界、寿元基线和基础修仙语义，不共享状态模型、任务、事件、判定或结局。共同规则见 `20-cultivation-shared-setting.md`。

## 规则来源与权威级别

玩法由三层共同实现，维护时必须区分：

1. 后端硬约束：身份与归属、State Schema、步骤工具快照、稳定判定 key、飞升境界等级、outcome verifier、奖励 calculator、工具事务、唯一结算和不可变奖励账本。玩家或模型不能用普通文本绕过。
2. 版本内容约束：Manifest、Runtime Contract、Prompt、世界书、结局和奖励规则，发布后成为不可变快照；旧对话保留旧版本用于浏览，新版本发布后普通对话自动封存，继续游玩需创建当前版本的新对话。
3. 叙事规则：修为进度、寿元消耗、机缘任务链、物品明细和剧情后果主要由 Prompt 与世界书指导模型执行，目前不是独立规则引擎。成功飞升还必须满足第一层的服务端证据。

玩家输入只代表“现在想做什么或说什么”，不具备回写历史和系统状态的权限。玩家自述祖传玉符、此前突破、隐藏事件、系统判定或奖励结果时，模型必须以 `<current_state>`、服务端 system 事件和已经保存的历史为准；不存在的证据不能因叙事写得完整就变成事实。

仓库中的内容源位于：

```text
backend/data/preset_packs/reincarnation-trials/
  pack.json
  runtime/
    runtime.json
    state-schema.json
    actions.json
    settlement.json
    presentation.json
  rewards.json
  characters/
  personas/
  scenarios/
  prompts/ascension-trials-v1.json
  worldbooks/ascension-system-v1.json
```

这些文件供空数据库首次初始化和管理员显式导入内置新版本。服务运行时以 PostgreSQL 中已发布的挑战版本为准；数据库已有任意挑战包后，重启服务不会自动读取、升级或覆盖这些内容。

## 核心体验

玩家从凡人或低阶修士起步，在有限寿元内积累资源、提升修为并选择飞升路线。主要张力不是单纯积累灵石，而是三组相互冲突的决策：

- 时间：继续闭关提高境界，还是在寿元耗尽前尽早离场。
- 风险：追求高收益路线，还是使用更安全但收益较低的机缘。
- 资源：把灵石用于当前成长、延寿和渡劫准备，还是保留到成功结算。

一局挑战对应一个 `conversation`。创建后固定挑战版本、内容哈希、天道试炼官 Character、天道试炼 Scenario，以及玩家选择的 Persona。

## 回合推进与玩家引导

百年飞升劫是由叙述者主动运转的单局文字挑战，不是等待玩家提问的聊天问答。每个正常回合必须：

1. 回应并结算玩家本轮行动，体现时间、资源、状态与人物关系变化。
2. 让世界继续发生事件、人物反应、机会、线索或风险，不能只复述玩家输入。
3. 在叙事末尾自然给出 2-4 个当前可执行方向，并说明眼前目标或主要代价。
4. 允许玩家提出列表外行动，但不得把“接下来做什么”的设计责任完全推回玩家。

Prompt 固定保留 12 个职责分段：总叙事、核心循环、角色生成、寿元、突破、正统渡劫、虚空裂缝、仙缘获取、仙缘使用、输入权限、结算安全和输出协议。世界书固定保留至少 12 类完整规则，其中共同境界和基础体系使用跨玩法共享规则 ID。导入测试会检查这些结构和主动推进指令，防止维护时再次被概述文本替代。

## 开局流程

1. 玩家在首页选择“百年飞升劫”（类型为“单局挑战”）。
2. 玩家从挑战允许的 Persona 中选择出身倾向。
3. 后端固定当前已发布版本并创建对话。
4. 默认 Character 的 `first_message` 为 `__AUTO_GENERATE__` 时，后端写入不可见的“开始”触发消息。
5. 模型根据 Persona、Scenario、Prompt、世界书和默认状态生成角色与开场。

开局应直接生成姓名、性别、称号、外貌、服饰、背景、初始境界、年龄、寿元、天赋、4-8 项属性、位置和初始灵石，并初始化 `cultivation` 与 `ascension` 状态。不得给每一世固定发放同名功法；初始功法为空，首个事件应按 Persona、出身和场景提供二至三条来源、代价与倾向不同的传承线索，只有玩家实际取得后才写入当前功法。开场不能停在人物介绍，必须同时生成一个可调查事件、人物请求、资源机会或迫近风险，并给出清晰行动方向。

## 境界体系

| stable key | 境界 | 子阶段 | 参考寿元上限 | 玩法定位 |
| --- | --- | --- | ---: | --- |
| `mortal` | 凡人 | 无 | 80 年 | 尚未正式踏入修行 |
| `qi-refining` | 练气期 | 1-13 层 | 120 年 | 生存与资源积累 |
| `foundation` | 筑基期 | 初期/中期/后期/大圆满 | 200 年 | 建立功法、丹药和法宝体系 |
| `golden-core` | 结丹期 | 初期/中期/后期/大圆满 | 500 年 | 首次具备仙缘飞升资格 |
| `nascent-soul` | 元婴期 | 初期/中期/后期/大圆满 | 1000 年 | 具备虚空裂缝路线资格 |
| `spirit-transformation` | 化神期 | 初期/中期/后期/大圆满 | 2000 年 | 具备正统渡劫资格 |
| `void-refining` | 炼虚期 | 初期/中期/后期/大圆满 | 5000 年 | 提高正统飞升准备度 |
| `body-integration` | 合体期 | 初期/中期/后期/大圆满 | 10000 年 | 接近此界极限 |
| `great-ascension` | 大乘期 | 初期/中期/后期/大圆满 | 20000 年 | 最高准备度与最高时间成本 |

境界数值是内容平衡基线。任何修改都应同时检查 Prompt、世界书、默认状态和测试用例，不能只改其中一处。

## 修炼与突破

典型突破由四步组成：

1. `profile.cultivation_progress` 达到瓶颈。
2. 玩家准备丹药、灵石、功法或天材地宝。
3. 模型调用当前步骤提供的 `fc_request_roll`，只提交稳定 key、展示标题和行为说明。
4. 后端按 Runtime 中该 key 的 `cultivation` 规则执行判定并原子落地可信状态效果；工具结果进入历史，模型在下一步骤依据真实结果继续叙述。

当前 Runtime 的基础成功率为：

| 突破 | 稳定判定 key | 基础成功率 | 主要风险 |
| --- | --- | ---: | --- |
| 凡人到练气 | `cultivation.breakthrough.qi_refining` | 85% | 引气失败、进度损失 |
| 练气到筑基 | `cultivation.breakthrough.foundation` | 75% | 修为倒退、寿元损失 |
| 筑基到结丹 | `cultivation.breakthrough.core` | 60% | 重伤、资源损失 |
| 结丹到元婴 | `cultivation.breakthrough.nascent` | 40% | 走火入魔、生死危机 |
| 元婴到化神 | `cultivation.breakthrough.deity` | 25% | 高额寿元损失或死亡 |
| 化神到炼虚 | `cultivation.breakthrough.void_refining` | 20% | 法则反噬、修为尽废 |
| 炼虚到合体 | `cultivation.breakthrough.body_integration` | 15% | 法身崩解或死亡 |
| 合体到大乘 | `cultivation.breakthrough.great_ascension` | 10% | 道基崩毁或直接陨落 |

失败后果应和判定等级匹配：普通失败以资源、进度和寿元损失为主；大失败可以造成重伤、境界倒退、走火入魔或死亡。模型不能在判定前预先宣布结果。

## 寿元系统

寿元同时由 `profile.age`、`profile.lifespan` 和 `profile.remaining_lifespan` 表达。模型每次推进明显时间跨度时都必须同步年龄和剩余寿元。

参考消耗：

| 行动 | 寿元消耗 |
| --- | ---: |
| 日常对话、行走、观察 | 0.1-0.3 年 |
| 小规模战斗 | 0.5-1 年 |
| 激烈战斗 | 1-2 年 |
| 短期闭关 | 1-5 年 |
| 长期闭关 | 5-20 年 |
| 炼丹炼器 | 0.5-5 年 |
| 重伤恢复 | 5-20 年 |
| 突破失败 | 10-50 年 |
| 使用禁术 | 10-100 年 |

寿元提醒节奏：

- 剩余 50%：首次温和提醒。
- 剩余 30%：明确提示突破、延寿或调整路线。
- 剩余 10%：至少每三回合提醒一次。
- 剩余 5%：每回合提醒，并要求玩家立即作出选择。
- 降至 0：提交 `lifespan_exhausted` 失败结局“寿元耗尽”。

可用延寿手段包括延寿丹、稀有灵药、特殊秘境、不老泉、妖兽内丹或高阶夺舍等。具体出现与代价由挑战内容控制；稀有资源不能因玩家一句要求就无条件出现。

## 灵石与资源循环

灵石是局内修炼资源，也是成功结算的基础值。典型用途包括：

- 购买突破丹药、功法、法宝和疗伤药。
- 布置聚灵阵或加速闭关。
- 购买延寿资源。
- 准备雷符、护体法宝和渡劫丹药。

模型只能通过当前步骤开放的状态工具更新 `attributes.灵石`，参数仍受 State Schema 校验。成功结局触发时，后端从服务端保存的当前状态读取该值，再按固定挑战版本的奖励规则计算最终平台奖励。模型不能提交最终奖励金额。

## 天命判定

需要不确定结果的战斗、探索、炼制、社交和修炼行为应使用判定工具循环：

1. 模型调用当前步骤提供的 `fc_request_roll`，只提交稳定判定 key、展示标题和行为说明。
2. 后端按基础成功率、属性、天赋、情境、收益上限、历史和管理员难度计算最终成功率。
3. 后端掷骰，在工具事务中原子写入 `roll_history`、展示消息、状态效果、invocation 和 tool result。
4. 下一模型步骤读取权威工具结果，再完成叙事或调用当前快照允许的另一个工具。

最终成功率被限制在 1%-99%。管理员难度修正当前为 easy `+15`、normal `0`、hard `-15`。高收益行为会受到更严格的成功率上限；连续失败、同类重试和高收益连胜也会影响后续判定。

玩家看到的判定卡片默认只显示摘要；展开“判定详情”后，可查看本次判定冻结的公开前置条件、成功率分解、最终成功率和结果区间。境界等 `player` 字段会使用 Presentation 的 `value_labels` 转换为玩法文案；未公开的模型/内部条件只参与服务端门禁，不出现在玩家详情中。详情缺失时历史消息仍保持摘要兼容。

动态特质必须进入 `traits` 数组才能参与判定。例如“重伤”造成全局减益，“顿悟”提升修炼判定；仅在叙事中提到而不写状态不会生效。

## 三条飞升路线

### 正统渡劫

- 最低境界：化神期。
- 触发：玩家主动确认“引动天劫”。
- 流程：九波独立雷劫判定。
- 影响因素：境界、护体法宝、雷符、疗伤丹药、业力和天赋。
- 失败：早期失败可重伤，中期失败可倒退境界，后期失败可灰飞烟灭。
- 成功工具：`fc_complete_challenge`，`outcome_key = orthodox_ascension`。
- 服务端条件：持久化境界至少为化神；必须成功通过 `cultivation.breakthrough.foundation/core/nascent/deity`，以及 `cultivation.tribulation.1` 到 `.9` 十三种稳定判定 key。
- 奖励倍率：`30000` bps，即局内灵石的 3 倍。

### 虚空裂缝

- 最低境界：元婴期。
- 触发：通过探索、任务或高气运机缘发现裂缝，并由玩家主动进入。
- 流程：执行“虚空裂缝穿越判定”。
- 失败：重伤、随机传送，严重时肉身损毁。
- 成功工具：`fc_complete_challenge`，`outcome_key = void_ascension`。
- 服务端条件：持久化境界至少为元婴；必须成功通过三种突破 key 和 `cultivation.void_rift.traverse`。
- 奖励倍率：`20000` bps，即局内灵石的 2 倍。

### 仙缘传送

- 最低境界：结丹期。
- 触发：玩家通过隐藏任务、NPC 馈赠或上古遗迹获得飞升符，并主动使用。
- 获取：剧情条件满足后必须先执行 `cultivation.affinity.acquire`；成功结果进入服务端流水，下一回合才可写入飞升符状态。
- 稀有度：一局通常只应出现 1-2 次获取机会。
- 风险：叙事上成功率极高，适合保守路线或寿元将尽时离场。
- 成功工具：`fc_complete_challenge`，`outcome_key = affinity_ascension`。
- 服务端条件：持久化境界至少为结丹；必须成功通过筑基、结丹和仙缘获取三种稳定 key。
- 奖励倍率：`15000` bps，即局内灵石的 1.5 倍。

## 非成功结局

### 坐化

玩家在剩余寿元不超过五年时可以主动安排后事并坐化，提交 `outcome_key = peaceful_passing`。坐化 category 为 `neutral`，保留叙事尊严，但没有奖励规则。

### 寿元耗尽或陨落

寿元归零提交 `outcome_key = lifespan_exhausted`，category 为 `failure`。渡劫失败或其他致命事件应先按剧情与状态处理；只有已发布 outcome 才能结束挑战。失败结局没有奖励规则，通用统计以后可从 `challenge_runs.category` 聚合。

## 结构化状态

新对话默认状态的主要结构如下：

```json
{
  "profile": {
    "name": "",
    "gender": "",
    "title": "",
    "appearance": "",
    "attire": "",
    "background": "",
    "talent": "",
    "cultivation_realm_key": "mortal",
    "cultivation_level": "凡人",
    "cultivation_stage": "",
    "cultivation_progress": 0,
    "age": 18,
    "lifespan": 80,
    "remaining_lifespan": 80
  },
  "attributes": {"灵石": 1},
  "traits": [],
  "inventory": { "items": [] },
  "relationships": {},
  "location": {},
  "quests": [],
  "risks": [],
  "world_flags": {},
  "cultivation": {
    "breakthrough_attempts": 0,
    "failed_breakthroughs": 0,
    "talent_awakened": false,
    "cultivation_method": ""
  },
  "ascension": {
    "void_rift_discovered": false,
    "ascension_token_owned": false,
    "tribulation_preparation": 0,
    "karma_debt": 0
  }
}
```

模型通过当前步骤提供的类型化状态工具修改普通对话状态。标量更新可调用 `fc_state_set_number` 并提交 `{"updates":[{"path":"profile.remaining_lifespan","value":70}]}`；数组追加可调用 `fc_state_append_json` 并提交 `{"updates":[{"path":"traits","value_json":"\"顿悟\""}]}`。允许路径、操作、类型、默认值、可见性和更新权限全部来自本挑战 `runtime/state-schema.json`，通用后端没有修仙顶层命名空间白名单。

左侧状态栏由 Runtime Presentation 展示人物档案、境界与功法、寿元、动态属性与特质、位置、物品、人物关系、任务、风险和飞升准备。`attributes`、`inventory`、`relationships` 等允许动态子字段的玩家对象会保留可见动态内容，同时递归剔除 Schema 中明确标记为 `model` 或 `internal` 的字段。

## 结局与结算协议

成功结局工具调用示例：

```json
{
  "name": "fc_complete_challenge",
  "call_id": "provider-call-id",
  "arguments": {
    "outcome_key": "void_ascension",
    "summary": "你穿过虚空裂缝，成功超脱此界。"
  }
}
```

该工具只在 Runtime 允许完成挑战且当前版本存在可用 outcome 时提供。后端处理顺序：

1. 根据步骤工具快照确认名称、参数和 `outcome_key`，然后锁定对话。
2. 按对话固定的 `challenge_version_id` 查找 outcome，执行通用 `core.conditions`。每条飞升路线在版本内容中用 `state_in` 明确列出可接受的 `profile.cultivation_realm_key`，并用 `successful_roll` 校验全部稳定判定 key；平台层不内置境界顺序或飞升门槛。
3. 校验成功后执行版本化 calculator，在一个工具事务中原子写入唯一 `challenge_run`、奖励账本、玩家余额、`challenge_run_rewards`、对话 ended 状态、invocation 与 tool 消息。
4. 校验拒绝时回滚领域写入，只保存公开 tool result；模型在下一步骤依据拒绝原因继续。若需要先改变状态，必须先在更早步骤提交状态工具，不能在同一个 Tool Call 中为自身补资格。

`challenge_runs.conversation_id` 唯一，`attempt_id + call_id` 与结算约束共同保证重复调用、重放请求和归档恢复都不能重复领奖。

## 内容维护清单

修改玩法时至少同步检查：

- `pack.json` 的版本号、公开说明、允许 Persona、outcome 和奖励规则。
- `runtime/` 下的状态 Schema、展示分组、动作和玩家能力文件。
- `rewards.json` 的全局奖励稳定语义。
- Prompt 中的稳定 outcome/roll key、玩家输入权限边界、仙缘获取流程和结算安全分段。
- Prompt 的 12 个完整职责分段与主动推进要求，不能压缩成规则摘要。
- 世界书中的至少 12 类完整规则、共享境界与基础术语、寿元、延寿、飞升路径和动作协议。
- `roll_config.yaml` 的类别、词表、难度和收益上限。
- Runtime 默认状态字段与前端公开展示契约。
- 正式文档中的规则表和结算说明。

已被对话使用的发布内容发生变化时必须使用新的版本号，不能覆盖其固定快照。新版本发布后旧版本退役，普通 active 对话自动封存但仍可浏览；详情页可基于当前版本创建新的初始对话。开发期尚未被对话引用的仓库内置版本可以由管理员显式覆盖；后端会删除后重建该版本并重新执行完整发布校验。

## 当前边界与后续增强

以下规则目前仍主要由模型与内容约束，不应在文档中误写成后端硬校验：

- `profile.cultivation_realm_key`、显示境界和寿元上限由服务端突破判定原子维护；阶段、修为进度和剩余寿元仍主要由模型维护，且尚无独立修为事件账本。
- 飞升符是否完成了指定剧情任务链仍由内容控制；后端只硬性要求本对话成功执行过 `仙缘获取判定`。
- 每回合寿元消耗是否严格落在参考范围。
- 局内灵石获取和消耗是否经过独立局内经济事件账本；账号灵石奖励已进入通用奖励账本。

后续若挑战奖励具有真实价值，应把这些条件逐步迁移为结构化规则、服务端事件和不可伪造流水，而不是继续增加 Prompt 文字。
