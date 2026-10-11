# 16. 修仙长期 Campaign Agent 参考

> 内容基线：`mortal-path 3.7.0`，可信模块 `cultivation.campaign@2`。本轮实现与本地检查已完成，PostgreSQL、浏览器及真实模型验收见 [25 验证记录](25-core-gameplay-validation.md)。
>
> 本文是后续开发 Agent 理解、维护和扩展修仙长期 Campaign 的稳定入口。它描述当前代码真实提供的边界，不记录实施阶段，也不把设想写成已交付能力。发生冲突时，以 `backend/migrations`、`backend/internal` 和已发布 Runtime Contract 为准。

## 1. 系统定位

架构接入见[24 玩法模块](24-gameplay-module-architecture.md)：初始化与修仙内容校验归 `cultivation/campaigninit`、`cultivation/contentrules`，通用制作/采集/市场/势力/世界组件位于 `gameplay/components`。平台不需要了解境界。功法归一化层级、闭关恢复、真实最终渡劫、出身物资和阶段积分均由修仙模块维护。

修仙长期 Campaign 是 Fatecraft 的单人长期世界模式。它把叙事模型和可信玩法引擎分开：

- 模型理解玩家意图、生成叙事，并在需要改变世界时提交一个受限领域动作；
- 后端校验动作主体、参数、版本定义、资源、位置、条件和并发版本，并计算最终结果；
- PostgreSQL 保存角色、成长、物品、NPC、势力、时间、任务、遭遇和结局等权威事实；
- 前端和模型读取由权威事实重建的有界投影，不能把投影反向当作写入接口；
- 每个 Campaign 固定创建时的 ChallengeVersion 和内容哈希，后续发布不会改变已有存档。

当前模式不包含多人共享世界、玩家交易或对战、按现实时间离线演化、Campaign 跨版本迁移、自由创建永久规则，以及完整战棋或逐帧战斗。它与单局玩法“百年飞升劫”只共享修仙世界设定，不共享运行状态、任务、事件、判定或结局；具体见 `20-cultivation-shared-setting.md`。

平台异步长期记忆已经接入本模式，只消费已提交且有效的消息、状态修订、判定和可供模型使用的已发生事件；它不能直接写入 Campaign 权威事实。左栏角色问答只读取玩家状态投影、Campaign 玩家快照、玩家可见事件和最近已封口消息，不直接暴露可能含 model evidence 的 Memory 正文。

## 2. 核心对象与权威边界

### 当前事实与动作叙事

Provider 声明姓名、境界 key/名称、阶段名称、当前/所需修为、生命与年龄/寿元为叙事关键事实。平台每步读取实时投影，将此小核心放在历史之后；模型不能因旧正文描写了突破就覆盖当前存档。普通状态问答、动作后的叙事与重新生成都遵守这一规则。

领域动作在原事务中生成执行前后事实反馈，随 `campaign_action_commits.result` 保存，幂等重试返回原反馈。闭关、学习或炼制提交成功不等于境界突破；突破或遭遇失败时，已支付的成本与真实反噬继续保留。反馈中的共享事实取 player/model 投影交集，NPC 私密上下文不复制到共享结果。大列表可省略，但未知不能描述为空或已经成功。

model 可见动作的不可用原因通过只读限制说明提供。规则前置不足、目录未提供和过期快照分别解释，仍由原生工具快照和事务决定执行权限。继续叙事保留既有动作，改写依据恢复后的分支事实；终局收尾不开放新动作。完整机制见 [19](19-agent-tool-loop.md) 和 [08](08-context-management.md)。

~~~text
ChallengePack
  `-- ChallengeVersion（已发布静态快照）
        |-- Runtime Contract
        |-- Prompt / WorldBook / Persona / Scenario
        |-- Gameplay Definitions
        `-- Outcome（含固定积分）

Campaign（单个玩家的长期权威存档）
  |-- Character / Techniques / Inventory / Equipment
  |-- Locations / NPC / Relationships / Factions
  |-- Recipes / Markets / Regions / Quests / Encounters
  |-- World Clock / Scheduled Events
  |-- Append-only Domain Events / Action Commits
  `-- Conversation（唯一的持续叙事对话）
~~~

| 数据 | 权威来源 | 说明 |
| --- | --- | --- |
| 世界规则和可引用 key | 已发布 `ChallengeVersion` | 创建 Campaign 后固定版本与哈希 |
| 角色、资源、关系和世界变化 | Campaign 领域表 | 只能由可信领域动作在事务中修改 |
| 领域历史 | `campaign_events` | 只追加；按 `player`、`model`、`internal` 控制可见性 |
| 动作幂等结果 | `campaign_action_commits` | 只追加；相同 action id 重放原结果 |
| 模型上下文 | `campaign_projection` | 有界、可重建、非权威 |
| 前端详情 | Campaign snapshot | 面向玩家过滤私密事实，非写入载体 |
| 对话叙事状态 | `conversation_states` | 当前 `mortal-path` 只开放 `scene`，不能承载领域事实 |
| 账号奖励 | `player_reward_balances` 和 `reward_ledger` | 与局内灵石、丹药、法宝严格分离 |

Campaign 状态只有：

- `active`：允许创建尚未绑定的唯一对话并执行普通动作；
- `completed`：终局任务已由后端核验并写入稳定结局 key；
- `dead`：气血或寿元结算导致角色死亡；
- `abandoned`：玩家显式放弃存档。

## 3. 静态定义与动态事实

### 3.1 静态定义

青河使用以下通用定义及四类修仙专属配置：

| kind | 用途 |
| --- | --- |
| `realm` | 境界顺序、突破成本、寿元和基础属性 |
| `technique` | 功法传承、层级、装配槽和逐层效果 |
| `item` | 堆叠物品、消耗效果和经济属性 |
| `equipment` | 独立装备、耐久、槽位和属性加成 |
| `location` | 界域/区域/地点/场景层级、连接、准入和采集 |
| `faction` | 势力权限、声望阈值和职位规则 |
| `npc_archetype` | 关键 NPC 或受限可实例化原型 |
| `recipe` | 炼丹、炼器、制符的输入、产出、门槛和失败规则 |
| `market` | 货币、有限库存、补货和价格规则 |
| `world_event` | 游戏时间调度与区域、市场、势力、NPC 聚合变化 |
| `quest` | 任务/承诺状态机、目标、奖励、解锁、里程碑和终局 |
| `encounter` | 敌方属性、回合、招式、逃跑和战利品规则 |
| `cultivation.origins` | Persona 与普通开局物资的明确映射 |
| `cultivation.training` | 各境界的闭关、炼体及恢复方案 |
| `cultivation.ascension` | 最高境界、主线分组、引劫材料与真实渡劫门槛 |
| `cultivation.progress_rewards` | 阶段事实、分段积分及完整通关总预算 |

定义的稳定 key 必须在同一个 ChallengeVersion 内唯一。发布校验会检查字段形状、数值范围、跨定义引用、地点连通性、任务可达性、资源来源和市场套利等边界。模型不能创建新定义，也不能引用当前固定版本之外的 key。

`realm.rank` 是当前玩法版本定义的非负顺序值，不是平台内置境界表。发布时要求 rank 唯一，且 `next_realm_key` 必须指向存在并具有更高 rank 的定义。`campaign_characters.realm_key` 是角色境界身份，`realm_rank` 是从同一固定版本派生的查询缓存：创建存档时两者取自最低 rank 定义，突破成功时在同一次事务中取自目标定义并成对更新。突破前若缓存与当前定义不一致，或目标缺少显式递增 rank，动作直接拒绝；禁止用 `当前 rank + 1` 猜测或修复。

功法开局有两种互斥声明：`starting_known=true` 表示确有必要的固定传承；`starting_pool=<稳定 key>` 表示候选传承，同一池按 Campaign 随机种子只选择一个。候选可用 `starting_weight` 配权，并用 `starting_slot` 指定选中后的装配槽。每个池至少需要两个候选；不同池或固定传承不能产生可能同时占用同一槽位的组合。可重复游玩的内容应优先使用候选池，且未选中的功法仍应通过传承物品、任务或市场保留可获得来源，不能把题材常见名称硬编码成所有存档的统一开局。

### 3.2 动态事实

动态事实分别保存在明确的 Campaign 领域表中，包括：

- `campaign_characters`、`campaign_techniques`；
- `campaign_item_stacks`、`campaign_item_instances`；
- `campaign_known_recipes`、`campaign_market_states`、`campaign_market_stocks`；
- `campaign_location_discoveries`、`campaign_npcs`、`campaign_relationships`；
- `campaign_faction_standings`、`campaign_region_states`；
- `campaign_scheduled_events`、`campaign_quests`、`campaign_encounters`。

JSONB 仅保存有界扩展属性；数量、状态、位置、时间、revision、装备槽等关键约束使用显式列、外键、唯一约束或 CHECK 约束。

## 4. 一次领域动作的执行顺序

模型动作使用当前步骤提供的真实工具。以修炼为例：

~~~json
{
  "name": "fc_action_cultivation_practice",
  "call_id": "provider-call-id",
  "arguments": {"technique_key": "已开放的稳定 key", "sessions": 1}
}
~~~

执行顺序如下：

1. Agent 协调器在收到完整归一化结果后按工具调用优先选择 tool lane，拒绝未知工具、非法 call ID、错误 JSON 和不符合步骤快照的参数；同一步伴随的文本只作为中间输出。
2. 工具绑定还原 `cultivation.practice` 动作 key 和快照固定 revision；服务端以 `attempt_id + call_id` 生成 Campaign 幂等键。模型不能自行提交这三项。
3. Campaign 服务确认当前玩家拥有该存档，固定版本和哈希仍可解析，动作已注册、位于 Runtime `action_allowlist`，调用主体在 `ActionDefinition.actors` 中，参数通过封闭静态 Schema。
4. PostgreSQL 锁定 Campaign 行；先查幂等提交，再检查 `active` 状态和快照 revision。
5. 在同一事务中重建领域快照并再次调用生成工具前使用的修仙 Capability Provider，复核动态 Schema、同一 option 参数组合和 limits；不修改模型提交值。
6. Provider 允许后，处理器执行最终题材校验、天命判定、确定性计算和领域写入；会推进游戏时间的动作同时处理到期效果、世界事件和任务截止，单回合上限由 Runtime 配置，当前为 16。
7. 后端校验动作必须产生 1 至 64 条事件、单事件 payload 不超过 8 KiB、总 payload 不超过 64 KiB、公开结果不超过 16 KiB。
8. 领域事实、时间、位置、Campaign 状态、事件、revision、幂等记录、判定流水与判定卡片、Agent invocation 与 tool call/result 消息在同一事务提交。拒绝/失败使用哨兵回滚领域写入后，只保存公开工具结果。

随机结果不能由玩家或模型指定。突破、制作、调查、采集风险、遭遇攻击与逃脱由对应 Campaign 动作在事务内调用共享天命判定核心，综合动作基础率、可信角色属性、伤势与资源状态、对手境界、历史结果和管理员难度，并且只产生一次权威结果。资源种类、数量、品质及世界事件仍使用 Campaign 随机种子、revision、服务端幂等键和用途派生可复现结果。`mortal-path` Runtime 不开放独立 `fc_request_roll`，因此不能在专用领域动作之前再掷一次骰。没有专属结算器的交涉、洞察、潜行、探索和脱险使用受限 `fortune.resolve_uncertain`；它只裁定行动是否成立，任何关系、物品、任务或伤势变化仍需后续领域动作提交。

## 5. 当前动作目录

动作 key、主体、当前可用状态和参数结构由内部 `ActionCapability` 派生。主叙事只看到 `available=true` 动作对应的 `fc_action_*` 工具及已收紧参数 Schema，不直接看到能力目录或 revision；玩家动作 API 仍使用公开能力和 revision。参数有关联时必须来自同一个 option 并服从 limits，不能从本文或静态最大值猜测。主体缩写：P = player，M = model，S = system。题材无关的完整协议见 [17-generic-campaign-action-capabilities.md](17-generic-campaign-action-capabilities.md)。

| 动作 key | 主体 | 作用 |
| --- | --- | --- |
| `character.rename` | P/M | 通过权威角色表更新公开姓名，并追加 `character.renamed` 领域事件；不能用会话状态模拟改名 |
| `cultivation.practice` | P/M | 修炼已掌握且已装配功法；必须从该动作 capability option 读取当前成本和 `sessions.maximum`，不能只使用静态参数上限 |
| `cultivation.retreat` | P/M | 按版本方案消耗材料、法力和游戏时间，增加修为与功法熟练度 |
| `character.recover` | P/M | 按方案恢复气血、法力、神识及普通伤势，不能增加修为 |
| `cultivation.begin_ascension` | P | 核验最高境界圆满及主线，扣除准备材料并开始最终天劫 |
| `cultivation.attempt_breakthrough` | P/M | 按下一境界定义结算突破成本、成功、失败和寿元 |
| `cultivation.learn_technique` | P/M | 消耗合法传承载体学习功法 |
| `cultivation.equip_technique` | P/M | 将功法装入定义允许的唯一能力槽并重算属性 |
| `cultivation.unequip_technique` | P/M | 卸下功法并重算属性 |
| `character.apply_condition` | M/S | 施加受限伤势、中毒、心魔或长期效果 |
| `character.resolve_condition` | M/S | 移除已存在的叙事效果；模型不能清除规则产生的突破反噬 |
| `character.adjust_vitals` | M/S | 模型仅能结算资源损耗，恢复使用休整或药品；致死时结束存档 |
| `inventory.gain_item` | M/S | 授予定义允许的堆叠物；模型还受 `model_grantable` 限制 |
| `inventory.use_item` | P/M | 消耗物品并应用后端白名单效果 |
| `inventory.gain_equipment` | M/S | 创建定义允许的独立装备；模型还受 `model_grantable` 限制 |
| `inventory.equip_item` | P/M | 将可用装备实例装入合法唯一槽位 |
| `inventory.unequip_item` | P/M | 卸下装备实例并重算属性 |
| `craft.learn_recipe` | P/M | 消耗配方载体掌握配方 |
| `craft.execute_recipe` | P/M | 原子消耗材料并结算成功、失败、品质和产物 |
| `trade.buy` | P/M | 从当前位置市场有限库存购买物品或装备 |
| `trade.sell` | P/M | 向当前位置市场出售物品或未装备实例 |
| `market.adjust_price_modifier` | S | 系统调整市场动态价格系数 |
| `travel.move` | P/M | 沿已发现相邻连接旅行，并校验境界与势力准入 |
| `world.investigate` | P/M | 调查当前位置并发现合法相邻地点 |
| `fortune.resolve_uncertain` | M | 对交涉、洞察、潜行、探索或脱险执行受限天命判定；本动作本身不改写其他领域事实 |
| `world.gather` | P/M | 按地点资源表采集、采矿或狩猎 |
| `npc.instantiate_archetype` | M/S | 在当前地点按原型白名单和实例上限创建 NPC |
| `npc.move` | M/S | 沿合法连接移动 NPC |
| `npc.set_status` | M/S | 按状态机改变 NPC 生命周期状态 |
| `relationship.record_interaction` | M/S | 更新当前地点 NPC 的多维关系与已知事实 |
| `faction.adjust_standing` | M/S | 调整势力声望、贡献和通缉并重算权限 |
| `quest.accept` | P/M | 接受已提供且满足位置要求的任务或承诺 |
| `quest.submit` | P/M | 后端核验目标、原子扣除物品、发奖、解锁后续；最终道路必须由玩家选择 |
| `encounter.start` | P/M | 在当前位置开始一个已发布遭遇 |
| `encounter.act` | P/M | 在活动遭遇中攻击或防御，可使用已装配功法 |
| `encounter.flee` | P/M | 按后端规则尝试逃离活动遭遇 |

`campaign.abandon` 是玩家生命周期操作，使用独立 API，不出现在模型动作目录中。活动遭遇存在时，除 `encounter.act` 和 `encounter.flee` 外的所有领域动作统一拒绝。

## 6. 玩法执行规则

### 6.1 角色、功法与物品

- 境界、基础/有效属性、气血、法力、神识、寿元、伤势和长期效果由后端维护；
- 境界 key 是身份，rank 只作为版本定义派生缓存参与门槛和数值比较，不能由玩家、模型或独立兜底逻辑写入；
- 功法具备传承来源、层数、熟练度、装配槽和逐层白名单效果；
- 堆叠物按物品、品质和数量维护，数量不能为负；
- 法宝与装备是独立 UUID 实例，同一实例只有一个状态和一个装备槽；
- 功法或装备变化后立即从定义重算有效属性，模型不能提交最终数值；
- 气血归零或寿元耗尽会原子写入死亡事实和事件，并阻止后续普通动作。

功法熟练度统一为 0～10000，层级为 `1 + floor(mastery × (max_layer - 1) / 10000)`，只有满熟练度才达到最高层。旧 `mastery_per_layer` 不再受支持；学习、修炼、战斗、装卸和投影共同校验持久层级一致。已装配功法和装备的效果统一读取，正法力成本至少扣 1；疗效修正限制在 -90%～+100%，用于药品与休整的正向气血恢复，卸下后失效。

突破先校验当前境界寿元是否足以完成耗时，精确考虑当前分钟在一天内的余量，再掷骰和写入下一境界寿元。恰好触及寿尽边界也不能靠突破自救。失败反噬为 `category=injury`、严重度 1～100 的规则伤势，可由付费休整减轻；模型不能覆盖或直接删除。

### 6.2 地图、NPC 与势力

- 地点采用 realm/region/location/scene 分层，旅行只允许已发现且直接连接的可旅行节点；
- 目标地点 `entry_requirements` 可组合境界下限和势力权限，前端路线提示与后端使用同一规则；
- 关键 NPC 和原型实例都有稳定 key、位置和生命周期状态；原型实例化受地点白名单与数量上限约束；
- 普通玩家 snapshot 永不包含 NPC 私密状态；模型只获得当前位置 NPC 的有界私密上下文；
- 势力分别保存声望、贡献、职位、权限和通缉，权限随当前数值重新计算而非永久赠送。

### 6.3 生产、市场与采集

- 制作在一个事务中检查功法、境界、地点、器具、法力和材料，再结算成功率、失败损耗、品质和伤害；
- 市场拥有有限库存、游戏时间补货、稀缺修正、势力声望修正和动态价格系数；
- 买卖货币、库存和物品在同一事务更新，重试不能复制资产；
- 经济资源只能来自初始资产、采集、任务、生产、交易或系统结算；
- `world.gather` 根据地点表、区域资源存量和服务端随机结算，不能用叙事授予动作绕过。

市场条目可配置 `restock_target_stock`（0～max_stock，要求正的 restock_quantity）。每个周期间隔按 `restock_quantity` 向目标移动：低于目标时补货，高于目标时消化收购物资，保证常规材料持续有收购容量。未设置的条目维持向 max_stock 补货；零补货的有限传承不自动再生。重复读取不推进周转，同一市场买卖仍受无套利校验。前端展示实际买价、卖价、在售数量和可收购数量。

### 6.4 世界时间、事件与任务

- 世界只随游戏动作推进，不随现实离线时间增长；
- 修炼、突破、旅行、调查、采集和制作等动作声明分钟增量；
- 跨过到期时间时，世界事件、周期续排、区域聚合变化、市场影响、NPC 目标和任务截止在当前事务内有界处理；
- 推进前预演全部到期根事件与周期后继，超过预算直接拒绝并返回可行分钟数；通过后按到期顺序处理全部事件，不将过期事件留到下一次操作；
- 任务目标由后端从位置、库存、势力、遭遇胜利等事实核验，隐藏目标和后续任务不提前进入玩家投影；
- 任务可产生局内奖励、解锁后续任务和稳定里程碑，但只有声明 `campaign_outcome_key` 的终局任务能完成 Campaign。

### 6.5 遭遇与结局

- 遭遇由后端保存回合、双方资源、状态和最大回合数；
- 伤害、法力成本、防御、逃跑、敌方行动、死亡和战利品均由规则结算；
- 同请求重放已提交的结果，种子派生的资源选择可复现；不接受模型提交命中、伤害或胜负；
- `encounter_win` 可作为任务目标，只有已持久化的胜利记录满足条件；
- 终局任务提交会在同一事务完成任务、写里程碑事件、设置 `completed`、记录稳定 outcome key 并授予账号奖励；
- 长期 Campaign 结局直接使用 reward_points，通关积分通过不可变账本幂等发放，不进行局内库存折算或额外扣除。

两条终局共享必要条件：大乘圆满、至少一条青河早期主线、星海讨伐、封闭裂隙、高阶问道准备，以及本 Campaign 的真实最终天劫胜利。引劫消耗 3 枚法则残片；普通 `encounter.start` 不能绕过引劫门槛。此处天劫是有限回合遭遇，区别于单局的九波机制。执掌天关还要求抵御天关破袭；携律远行要求交付 6 枚残片。

### 6.6 闭关、恢复与开局差异

九阶各有主修和炼体方案。备齐资源后主修通常四次、炼体五次达到本阶圆满；实际还需获取传承、赚取材料、恢复与承担突破失败。闭关天数依次为 1/7/30/60/90/120/180/180/180，地点、功法、境界、法力、材料、寿元、限时承诺和事件预算均在扣费前核验。高阶资源逐步转向天材与法则残片，不能只在村口重复低阶劳动。

七日调息不耗材料，恢复气血 10%、法力/神识 25%，不治疗普通伤势；三个付费恢复档位耗时一日，恢复气血 75%、法力/神识 100%，每项普通伤势减轻 50。疗效可修正气血恢复，毒、心魔等不因普通疗伤一并清除。

六种出身在基础物资上追加不同普通材料：药师学徒偏药草/疗伤，青岚杂役偏符纸/朱砂，老渡口船工偏泉水/灵木，林氏旁支偏灵石，衙门差役偏丹药，游方散修偏矿石。映射必须覆盖允许的 Persona；只可追加存在且本来就有开局数量的普通堆叠物，单项最多 20、总量最多 30，不能赠送高阶传承或账号积分。开局三类随机传承池继续按真实 seed 选择。

竹林调查、伏流救援与宗门失踪调查要求实际遭遇胜利及各自证物；仅到地点、提高关系或交普通矿材不能完成。NPC 关系与势力反应仍保留受限叙事动作，但这些动作不能代替真实战斗证据、成长或最终渡劫。

## 7. 模型上下文和限制

内部 Campaign Projection 当前包含 Campaign 头、revision、当前位置、游戏时间、最近事件、`action_capabilities` 和当前决策相关的修仙领域投影。协调器把按目录顺序排在前面的最多 16 个可用能力转换为当前步骤 `fc_action_*` 工具，再从模型可见 Projection 正文移除能力目录。主要集合是有界的：Runtime 动作最多 64 个、每个动作 options 最多 64 个且内部能力目录合计最多 96 KiB；最近事件最多 16 条且受 16 KiB 预算约束，功法 16、每类库存 32、配方 12、当前关系 12、显式引用的已知远端关系 4、当前位置 NPC 私密条目 8、任务/承诺各 12、最近遭遇 4、可开始遭遇 8。

Context Provider 同时声明顶层主题优先级；完整投影超出当前模型窗口时，通用 Context Builder 保持 Campaign 核心，并按该优先级整段加入角色、遭遇、区域、任务、关系、库存等主题。工具定义独立计入必选预算，不能通过压缩 Projection 静默删除。`projection_control.partial=true` 表示本轮是有损读模型，省略主题不等于空状态。压缩只影响当前 Prompt，不修改存档、工具快照或事务规则；具体通用协议见 17 号文档。

5000 条库存和 5000 条遭遇的自动测试仍将投影限制在 128 KiB 内。大世界的扩展方式是增加数据库定义和按需投影，不是把所有世界资料一次性塞入 Prompt。

模型可以：

- 根据投影理解当前事实并生成符合世界尺度的叙事；
- 调用当前步骤提供的一个 `fc_action_*` 工具，并严格使用其动态 Schema；
- 在工具成功后依据公开结果继续下一步骤，在没有可信变化时直接返回普通叙事；
- 在没有领域变化时只叙事，或只更新 Runtime 允许的对话状态 `scene`；
- 引入非关键路人时，从投影给出的可实例化原型中选择。

模型禁止：

- 直接写境界、属性、寿元、库存、装备、功法、位置、关系、势力、任务、时间、遭遇或结局；
- 编造未知 key、隐藏地点、隐藏任务条件、NPC 秘密或当前投影不存在的资源；
- 自行决定随机结果、制作品质、交易价格、战斗伤害、胜负、战利品或奖励；
- 用 `inventory.gain_*` 铸造采集、任务、制作或市场经济资源；
- 在活动遭遇中叙述其他动作已经完成；
- 主动宣告 Campaign 完成或账号奖励已发放；
- 把 rejected/failed、未完整接收或未出现在工具快照中的调用描述成已提交事实。

工具参数、状态或领域校验失败时，本次工具事务不会留下领域写入，公开拒绝结果进入工具历史供下一步骤处理。已经 committed 的更早工具保持有效；若后续模型调用失败，attempt 标记为 `partial`，临时文本不持久化，用户可从权威事实继续叙事。

## 8. API 与前端

受保护的 Campaign API 包括：

- `POST /campaigns`、`GET /campaigns`、`GET /campaigns/:id`；
- `GET /campaigns/:id/snapshot`、`GET /campaigns/:id/events`；
- `GET /campaigns/:id/conversations`、`POST /campaigns/:id/conversations`；
- `POST /campaigns/:id/actions`、`POST /campaigns/:id/abandon`。

所有读取和写入都校验当前玩家归属，归属不符按不存在处理。事件接口仅返回 `player` 可见事件并使用 sequence 游标分页。

前端 `CampaignViewRenderer` 根据固定 Runtime 的 `presentation.campaign_view` 提供栏目、字段和动作槽，包括闭关休整、阶段目标和飞升准备。动作提交期间禁止重复操作；结果不明时按 Campaign 持久保留原请求重试，不能新建请求猜测结果。已提交但刷新失败与动作失败区分显示；离开后重开同一对话会忽略旧响应。成功同步快照、消息、完成状态及账号余额。领域动作的判定卡片来自服务端持久化结果。

## 9. 内置内容基线：mortal-path 3.7.0

合并加载 270 条版本化定义：9 境界、28 功法、71 物品、12 装备、35 地点、11 势力、18 NPC、12 配方、5 市场、12 世界事件、22 任务、10 遭遇，另有 1 出身、1 飞升、1 阶段奖励与 22 闭关/恢复方案。

青河郡 → 天星海 → 界墟 → 上灵界的连接随境界开放。炼虚的虚空标定、合体的法身试炼、大乘的问道准备连接到最终飞升门槛；高阶功法均有任务或交易来源。12 类宏观世界事件每 180 天续排，市场独立按各自游戏时钟周转。

阶段积分依次为：筑基并完成一条早期主线 100 分；结丹并完成星海讨伐与真实胜利 100 分；化神并完成封闭裂隙与真实胜利 100 分。阶段按顺序检查，已领取记录不能替代回档后当前分支所缺少的任务。

两条终局 world_warden（执掌天关）和 law_seeker（携律远行）分别再发 700 分，各自完整通关共 1000 分。阶段 receipt 和终局账本保留已发事实，回档不会收回也不能重复领取；0 分与直连有明确实际发放记录。详见 [21 积分协议](21-campaign-outcome-rewards.md)。

## 终局与玩家中止

青河修仙志的可信终局包括两个成功终局、`perished`（身亡）、`lifespan_exhausted`（寿尽）和 `abandoned`（玩家中止本世）。身亡与寿尽由领域状态和时间支付顺序自动判定；中止由玩家确认后执行 `campaign.abandon`，不要求境界或寿元条件，也不发终局积分。模型不能代替玩家调用该动作，叙事也不能覆盖已经提交的终局。

Campaign 快照由平台追加 `outcome_category`、`outcome_title` 等保留字段。失败和中立不再被 `completed` 的绿色成功文案掩盖；对话刷新、断线重连和顶部结束入口均读取同一持久化终局事实。

## 10. 内容包扩展规则

`backend/data/preset_packs/mortal-path` 的关键文件为：

- `pack.json`：版本、入口资产、结局与奖励规则；
- `runtime/runtime.json`：Runtime 入口和分层文件引用；
- `runtime/campaign.json`：Campaign handler、动作白名单、初始地点和时钟上限；
- `runtime/state-schema.json`、`runtime/actions.json`、`runtime/settlement.json`、`runtime/presentation.json`：状态、模型动作、结算和玩家展示契约；
- `definitions/*.json`：按职责拆分并按文件名加载；00/01 成长功法、05 出身、10～13 物品装备制作市场、15 早期证据、20～25 地图势力人物事件任务遭遇、40 飞升、45 阶段积分、50 闭关恢复；根定义及旧高阶混合文件已移除，无重复加载；
- `characters/`、`personas/`、`scenarios/`、`prompts/`、`worldbooks/`：叙事资产。

扩展内容时遵循以下顺序：

1. 使用稳定、小写 key，先补定义，再补引用它的地点、市场、事件、任务或遭遇。
2. 大块内容放入带排序前缀的 `definitions/*.json`，不要复制基础定义。
3. 为新资源建立可信来源，为新地点建立双向可达连接，为新任务建立从初始任务可达的解锁路径。
4. 新规则能力先实现后端解析、校验、动作和测试，再在内容包中启用；不能先用自由 JSON 或 Prompt 模拟权威规则。
5. 改变已发布内容时提升稳定版本号。管理员的“覆盖内置内容”只允许替换未被 Conversation 或 Campaign 使用的同版本快照；发布新版本会退役旧版本并封存其普通对话，旧对话保持只读，可从详情页基于当前版本开启新对话。Campaign 存档仍固定原版本并按独立生命周期运行。
6. 先通过加载与发布校验，再在空数据库或隔离 schema 执行真实导入；不要只做 JSON 解析检查。

本项目仍为新项目，本轮表结构只修改 `00001_init.sql`，未新增 `00002`。已经执行过旧 00001 的开发库不会因再次 migrate 自动获得修改；验证使用新库或测试创建的隔离 schema，不自动重置已有数据。正式保存数据后应冻结基线并使用增量迁移。

## 11. 安全不变量

- Campaign 固定 `challenge_version_id + challenge_hash`；
- 同一 Campaign 的写动作通过行锁串行化；
- 幂等检查发生在 active/revision 检查之前，过期重试仍可重放原结果；
- 动作必须位于 Runtime 白名单，主体必须出现在 `ActionDefinition` 声明中，处理器还可执行更细权限检查；
- 所有资源和规则 key 必须来自固定版本定义；
- 角色 `realm_key + realm_rank` 必须对应同一版本定义，并在境界变化时原子更新；
- 库存扣除、产出、时间、事件、结局和奖励必须事务一致；
- 玩家和模型都不能指定随机结果；
- 事件与动作提交记录只追加；
- 玩家 snapshot 不泄漏 NPC 私密状态、远端区域私密信息或隐藏任务条件；
- 失败动作不能留下领域事实，也不能让叙事声称结果已提交；
- 账号奖励与 Campaign 局内资产不能直接互相替代；结局固定积分与存档完成同事务提交，局内库存不折算为积分。

## 12. 验证与发布基线

每次修改 Campaign 引擎、schema 或 `mortal-path` 内容至少执行：

~~~powershell
cd backend
go test ./...
go vet ./...

cd ../frontend
npm run build
~~~

PostgreSQL 集成测试读取 `FATECRAFT_TEST_DATABASE_URL`，为每次测试创建随机临时 schema、执行完整 Goose migration，结束后删除 schema。它当前验证：

- revision 并发只有一个提交成功；
- action id 幂等重放；
- 数据库拒绝负库存；
- Campaign 固定积分、0 分、直连不发放、事务回滚和账本幂等；
- `mortal-path 3.7.0` 的 270 条定义、九阶境界、出身初始化、阶段与终局积分、客户端直连和当前分支事实；剧情改写/重新生成使用平台级统一额度。

本机没有执行真实 PostgreSQL，相关测试已编写且编译通过，未配置测试库时跳过。24 组完整经济/主线离线流程、6 组生产筹资和 144 组供料阶段模拟各自覆盖不同范围；不能把内存规则模拟当成数据库事务或真实模型验收。分布与可复现命令见 [25 验证记录](25-core-gameplay-validation.md)。

`.github/workflows/verify.yml` 在 PostgreSQL 17 服务上运行后端测试和 vet，并在 Node 22 上运行前端构建。当前前端没有 Playwright/Cypress 浏览器 E2E；自动化 UI 基线是 `vue-tsc --noEmit + vite build`，交互和多视口仍需手工验收或后续单独补充浏览器测试套件，不能在交付说明中声称已有 E2E。

## 13. 关键代码入口

| 关注点 | 入口 |
| --- | --- |
| PostgreSQL 权威 schema | `backend/migrations/*.sql`，由 `cmd/migrate` 执行 |
| Campaign 生命周期与动作调度 | `backend/internal/campaign/service.go` |
| Campaign 天命判定适配 | `backend/internal/campaign/roll.go` |
| 通用动作能力、Schema 和 Violation | `backend/internal/campaign/capability.go` |
| 事务、revision、事件和幂等 | `backend/internal/storage/campaign.go` |
| 领域实体和存储操作 | `backend/internal/storage/campaign_domain.go` |
| 修仙动作目录 | `backend/internal/gameplay/cultivation/campaignactions/actions.go` |
| 修仙判定状态映射 | `backend/internal/gameplay/cultivation/campaignactions/rolls.go` |
| 生产和经济动作 | `backend/internal/gameplay/cultivation/campaignactions/economy_actions.go` |
| 遭遇动作 | `backend/internal/gameplay/cultivation/campaignactions/encounter_actions.go` |
| 世界事件、任务和终局 | `backend/internal/gameplay/cultivation/campaignactions/world_actions.go` |
| 修仙能力和静态参数 Schema | `backend/internal/gameplay/cultivation/campaignactions/capabilities.go`、`schema.go` |
| 修炼成本与上限单一规则源 | `backend/internal/gameplay/cultivation/practicerules/rules.go` |
| 修仙投影与信息边界 | `backend/internal/gameplay/cultivation/campaignview` |
| 模型工具目录与领域动作提交 | `backend/internal/orchestration/agent_tools.go`、`agent_transaction.go` |
| 内容加载和发布校验 | `backend/internal/challengeimport`、`backend/internal/challenge` |
| 内置修仙内容 | `backend/data/preset_packs/mortal-path` |
| Campaign HTTP API | `backend/internal/api/campaigns.go` |
| 前端 Campaign 通用渲染器 | `frontend/src/components/CampaignViewRenderer.vue` |

后续 Agent 改动时应先确定变化属于“静态定义”“动态事实”“领域动作”“投影”还是“叙事”。任何影响资源、成长、位置、关系、时间、任务、遭遇或结局的能力都必须进入领域动作和 PostgreSQL 事实层，不能只修改 Prompt 或前端。


## 中止与真实完成（2026-10-11）

`campaign.abandon` 为玩家终局，活动遭遇不阻止中止。模型可提议，由玩家在界面确认。Campaign 终态在原动作事务关闭对话并保存完整结果；保留已到账阶段积分，不额外发终局积分。 完整协议见 [26 玩法结局生命周期](26-gameplay-ending-lifecycle.md)。
