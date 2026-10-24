# 24. 配置驱动与可信玩法模块

实现基线：2026-10-09。本文记录第一阶段的实际架构；八种主类型、九个内容包的本地检查及未验事项见[验收记录](gameplay-modules/validation.md)。两套修仙核心玩法已完成本轮规则更新，正式规格见[单局](14-ascension-trial-gameplay.md)与[长期](16-cultivation-campaign-agent-reference.md)，平衡样本及真实环境交接见[25 验证记录](25-core-gameplay-validation.md)。

## 层次与装配

| 层 | 实际位置 | 职责 |
| --- | --- | --- |
| 中立契约 | `backend/internal/gameplay/contract/` | Runtime、模块目录、定义校验、状态所有权、动作与投影 DTO |
| 执行内核 | `backend/internal/gameplay/kernel/` | 纯状态执行、权限、参数、预算、随机证据和不可变输入检查 |
| 公共规则组件 | `backend/internal/gameplay/components/` | 世界动作/内容/初始化/投影，制作、采集、市场、势力、生产、人员、远征与配置处理 |
| 可信模块 | 同目录下 `cultivation/`、`survival/`、`management/`、`stealth/`、`relationship/`、`tomb/`、`dungeon/`、`sect/` | 各玩法的合法状态、成本、时间、事件、行动和终局 |
| 组合入口 | `backend/internal/gameplay/bootstrap/` | 启动、导入及测试共用的完整模块与 Campaign 处理器安装 |
| 内容包 | `backend/data/preset_packs/<slug>/` | 固定版本的规则数据、角色、出身、场景、Prompt、世界书、结局和展示 |

通用服务、API、协调器和仓储依赖中立契约，不导入具体玩法。组合入口可以导入全部已安装模块；题材存储适配器可以依赖仓储，纯规则不能获得全局数据库连接或账号余额。依赖回归由 `gameplay/bootstrap` 的测试检查。

一个内容包选择一个确切的 `engine.key + engine.version`，主模块在代码中选择公共组件。管理员不能任意混搭多个主模块，也不能上传 Go、JavaScript、SQL、WASM 或表达式。首期“可插拔”是编译期注册：同模块新内容主要通过配置完成，新算法或组件仍需构建部署。

原有 `core.generic@1` 和 `campaign.generic@1` 保留为通用基础引擎，不额外计作产品主类型。通用 Campaign 直接使用 `components/world*`；修仙 Campaign 的出生、功法和境界归 `cultivation`，不存在先产生修仙角色再删除字段的通用实现。

## 八种主类型与运行方式

| 展示顺序 | 类型 | 模块 | 内容包 | 模式 |
| ---: | --- | --- | --- | --- |
| 1 | 生存探索 | `survival.exploration@1` | 荒岛求援 `island-rescue` | Challenge |
| 2 | 经营建设 | `management.settlement@1` | 山间客栈 `mountain-inn` | Campaign |
| 3 | 潜入逃脱 | `stealth.escape@1` | 夜入封锁区 `night-blockade` | Challenge |
| 4 | 关系叙事 | `relationship.story@1` | 小镇旧约 `old-town-promise` | Campaign |
| 5 | 古墓探险 | `expedition.tomb@1` | 失落墓室 `lost-chambers` | Challenge |
| 6 | 地牢远征 | `expedition.dungeon@1` | 地下哨塔 `underground-watchtower` | Challenge |
| 7 | 修仙飞升 | `cultivation.trial@2` / `cultivation.campaign@2` | 百年飞升劫 / 青河修仙志 | Challenge / Campaign |
| 8 | 宗门经营 | `sect.management@1` | 青岚宗重建 `qinglan-sect` | Campaign |

七个新增模块和修仙单局采用下述纯运行接口；修仙和通用世界 Campaign 使用领域表及注册动作。它们共享平台的版本、事务、分支与结算保障，但不强迫使用同一种存储形态。

## 纯运行接口

`RuntimeModule` 提供 `Actions`、`Initialize`、`ValidateState`、`Preview`、`Execute`、`Project`。输入是固定内容和本次权威状态，返回新状态、领域事件及动作结果；接口不接收账号账本、网络客户端或数据库事务。

- `RunState` 保存 Schema 版本、revision、整数游戏时间、active/completed/failed、结局 key、领域数据和本次随机证据。
- 行动声明主体、参数、可写路径与事件。内容必须保留纯模块完整的 domain 动作目录，Campaign 同时保留对应 allowlist。当前没有可选动作依赖声明；可用性由实时 Preview 决定，不能靠删行动裁掉流程。
- Preview 输出可执行性、拒绝原因、资源成本、时间、风险和具体参数方案；执行仍按当前真实状态复核，预览不是授权凭据。方案可携带 `blocked_reason`（如「资源不足：需要 灵石 ×2（持有 ×1）」）：带此字段的方案仅用于展示与缺口提示，执行时由支付复核拒绝；trial 模块的 `options()` 保留全部缺口方案，`Available` 仅在没有任何方案时为 false。
- Execute 不接受调用者指定收益、概率或随机值。平台传入服务端种子生成的随机源，内核记录实际抽样并覆盖模块自报证据。
- Project 按 player、model、inquiry 三种受众执行。询问只解释已提交事实，不调用行动或读取未来随机数。
- 输入经复制后传入模块，校验和预览不能偷偷改存档。状态更新不能超出动作写入声明，终态不能继续产生效果。

内核上限：内容 4 MiB、状态 64 KiB、单次 Preview/Project 32 KiB、动作 64、参数 16、事件 32、随机抽样 64。累计游戏时间最多 1000 亿分钟、单次行动最多 1000 万分钟（`kernel.MaxActionMinutes`，约 6944 天，容纳修仙类深闭关）；随机用途不能重复，资源成本等数值受安全范围限制。Execute 返回前重新运行新状态的双方 Preview 和三类 Project，避免提交一个后续无法读取或操作的状态。预算超限拒绝整次动作，不先扣资源。

## 叙事关键事实与动作反馈

纯模块通过 `NarrativeFactProvider.NarrativeFactPaths()` 声明 model 投影中的关键路径；八种内置主类型均已提供。Campaign Provider 在 `ProjectionView.FactPaths` 声明关键路径，纯模块 Campaign 适配器自动增加 `gameplay.` 前缀，修仙 Campaign 的境界、阶段、修为、生命和寿元由修仙 Provider 声明。通用层仅做投影取值、比较、容量和受众处理，不认识题材字段。

模块核心最多 20 项、事实对象最多 4 KiB。纯模块在 model Project 阶段复核核心容量，因此发布、初始化验收与动作后 Project 检查会使用同一规则；无法支持后续投影的动作在提交前拒绝。平台在历史之后提供最新核心，完整背包、任务和领域详情仍按原有预算压缩。

动作反馈从执行前后真实投影产生，区分可信提交与游戏目标成功；由 player/model 投影交集保证共享结果不泄露独占字段。反馈最多 8 KiB，最多 24 条变化，集合按稳定身份列出变化项；无法完整提供的详情明确标记。Campaign 的反馈提升为持久 `ActionResult`，写入 `campaign_action_commits.narrative`，并带 `revision` 与 `state_hash`；单局模块写入 `module_action_commits.narrative`，不参与 Campaign 哈希。助手叙事通过 `display_message_campaign_actions` 关联全部来源动作，系统动作回执不算叙事消费。

领域规则继续负责动作可用性和公开解释，平台只保留 model 主体当前可见的只读限制说明。未知动作、目录限制、前置不足及过期预览分别处理；可用目录仍是唯一执行权限来源。恢复和回放使用已提交结果，不因补反馈而重复执行动作。

## 两种持久化适配

### Challenge

`challenge/module_*.go` 将模块状态保存在 `conversation_states.module_state` 的受保护信封中，包含引擎、服务端种子和 RunState。`gameplay` 是玩家投影缓存；两者均由 `SourceModule` / `UpdateModule` 独占，普通模型更新不能覆盖根对象或子路径。

模型动作、玩家动作和最终结算使用现有工具/回合事务。纯模块达到终态时，由平台读取固定 Outcome 并结算；不能用模型自由填写的结局替代模块终态。原有 State/Roll 模块维持自己的固定版本校验。

### Campaign

`gameplay/campaignruntime` 适配纯模块到现有 Campaign 生命周期、能力、事务、事件与奖励服务。`campaign_runtime_states` 按 Campaign 保存固定引擎、种子与模块状态，不创建虚假的修仙角色。CampaignService 为 Snapshot 添加平台所有的 `campaign` 头与收益事实，模块不能覆盖。

模块 revision、游戏时间、状态和结局必须与 Campaign 头一致。纯模块终态按 Outcome 分类映射到 Campaign：`success` 使用 `completed`，`failure` 使用 `dead`，`neutral` 使用 `abandoned`；不能把 `completed` 直接当作成功。玩家关闭经营、离开关系故事、解散宗门以及青河修仙志中止本世均经声明的零参数玩家动作执行；模型无权替玩家决定。顶部“结束对话”对有玩法绑定的对话也走同一主动中止事务，不得只更新 conversation 状态。

每次行动的随机证据另以内部 `core.module_random` 事件追加，每组最多 16 条；同一模型回合的后续行动不会抹掉前次证据。内部种子、随机证据和审计数据不出现在玩家/模型/问答投影。

### 幂等与恢复

玩家行动进入 `module_action_commits` 和正常回合日志，包含请求 ID、参数哈希和分支。事务先锁对话，再锁 Campaign，复核用户、active 状态、generation owner、branch epoch 和 revision。同请求返回已保存结果；同请求换参数、旧分支或过期 revision 拒绝，不靠前端防连点保证正确性。

`campaign_checkpoints` 在回合前保存全部可变领域表，包含模块状态、角色、功法、物品、市场、NPC、关系、事件队列、任务和遭遇。改写/重新生成恢复目标检查点，并递增 revision 和 branch epoch；按外键逆序删除、正序恢复，任何表失败则整事务回滚。缺少完整检查点明确拒绝。

账号余额、账本、奖励事实、种子和追加历史不回档。旧分支事件与动作提交按来源 turn 的有效性过滤；已到账奖励不会因为重走历史再次发放。状态恢复的存储层检查归属、固定内容哈希、格式、头部一致性和容量；模块领域不变量在后续投影和动作前重新核验，不宣称存储恢复函数已经执行全部模块规则。

Campaign 最后一项模型行动将对话结束时，只有同 attempt、同 turn、当前终局 revision 的提交能继续完成收尾叙事。仅持有 run、旧 attempt 或旧 revision 不再拥有写入权。

## 制作、发布与版本

模块注册自己的 definition kind、中文标签、Schema、形状和内容验证器。通用内容服务不维护题材枚举。发布同时检查通用资产/引用、精确模块版本、动作与字段权限、结局、所有可选出身初始化、Preview 与各受众 Project，以及通用展示路径与类型。

七个新模块的主规则定义各为一个对象，具体字段见[模块制作指南](gameplay-modules/README.md)。严格解码拒绝未知字段；Singleton 解析限制嵌套 8 层、集合 64 项、key 128 字节、字符串 256 字符，模块还可施加更小限额。草稿解码本身不等于已通过完整发布校验。

结构可达性验证包括资源来源、设施/配方依赖、有限开锁工具、事实和机关依赖等。它不能证明任意复杂配置的完整时间、数量、风险可行性，也不能代替平衡模拟。初始空列表的条目展示路径无法靠初始投影完全验证；内容作者仍须走通后续状态。宗门额外用后期菜单探针估计展示预算，探针不是可执行存档或通关模拟。

内容版本、精确模块版本和状态格式分别管理。已发布内容不可改，旧局继续原版；缺失模块或不兼容契约时只读并说明原因，不自动归档、升级或降级。部署前使用 gameplay-check 审核全部活跃/可恢复依赖。

backend/data/bootstrap.json 列出八类九个默认包。启动补齐缺失包并发布启用，已有包不升级、覆盖或重新启用。手动内置/上传导入只产生草稿；停用只阻止新开局，运行暂停才阻止原版执行。

## 玩家、管理员与上下文

玩家侧栏的“行动”页使用 `ModuleActionPanel.vue` 展示实时菜单与方案，并通过统一 GET/POST `/api/conversations/:id/actions` 请求。状态冲突刷新方案；网络结果不明时保留同一请求重试。切换对话后迟到响应不能污染当前页，旧请求不能清除新请求。异步提交成功后重新读取状态、Campaign 与账号余额，不乐观伪造结果。

管理员版本编辑器按分类选择已安装模块，并根据该模块的 Schema 编辑规则。切换模块初始化对应权限、动作及展示骨架，但仍须补齐规则和结局才能发布；完整示例导入是推荐起点。未知渲染器和不支持的动作明确报错。

纯模块 Challenge 在模型上下文中去掉缓存的玩家 gameplay，另行构造 model 投影；世界书扫描使用同受众数据。问答使用 inquiry 投影，Challenge 基于调用者已固定的状态快照构造，不再次读取变化中的实时状态。超预算投影优先保留可容纳的标量，再尝试集合，并列出 `projection_control.omitted_fields`；没有显示的列表不等于事实为空。

## 数据库与维护

本次表和约束写入 `backend/migrations/00001_init.sql`，不新开 00002。使用显式 Goose；已执行过旧 00001 的数据库不会自动升级，应用启动不执行 DDL，也不改 Goose 历史。

新增模块的维护顺序：明确规则与失败路径 → 选择公共组件 → 实现并注册模块 → 配置示例与变体 → 校验发布/恢复/奖励 → 维护玩家展示和管理员 Schema → 执行本地与隔离数据库验收。真实数据库与模型试玩结果必须单独记录，不能把跳过的集成测试当作通过。


## 终局声明与退出

运行时动作通过 `Ending` 标记玩家终局，模块通过 `AbandonAction` 声明统一提前退出。退出须为零参数的 player 动作，发布探针检查零时间、启用的 neutral 零积分 Outcome。Campaign 终态同步关闭关联对话，完整结果沿用动作记录保存。 完整协议见 [26 玩法结局生命周期](26-gameplay-ending-lifecycle.md)。

## 玩法包交换与版本管理

Portable 编译器统一目录/JSON/ZIP、严格 JSON 与发布验证。运行依赖精确 engine key/version；旧局无需跟随当前入口。模块语义变化必须提升模块版本，移除旧模块前运行只读部署检查。

完整协议见 [27 玩法包交换与版本生命周期](27-gameplay-package-exchange-and-versions.md)。
