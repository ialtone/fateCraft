# 玩法模块制作与示例

与[玩法模块架构](../24-gameplay-module-architecture.md)配套。下文为已实现规则，不代表示例完成了商业平衡。八个新增示例与两个修仙包一起默认导入并启用；已有库启动时仅补齐缺失包。管理员可在后台“内置内容”停用入口，再编辑和发布新版本；发布不会重新启用已停用的包。

## 制作入口

每个包沿用 `pack.json`、`runtime/`、`definitions/` 和资产目录。先复制同玩法的完整示例，保持 engine 及模块要求的动作/权限，再修改其主规则 definition 的 `content`。后台根据服务端目录生成数值、文本、对象与列表编辑控件；Schema、实际解析和内容验证器共同决定可接受配置。

| 类型 | 示例目录 | 主规则 definition kind |
| --- | --- | --- |
| 生存探索 | `backend/data/preset_packs/island-rescue` | `survival.scenario` |
| 经营建设 | `backend/data/preset_packs/mountain-inn` | `management.business` |
| 潜入逃脱 | `backend/data/preset_packs/night-blockade` | `stealth.operation` |
| 关系叙事 | `backend/data/preset_packs/old-town-promise` | `relationship.story` |
| 恋爱养成（关系叙事） | `backend/data/preset_packs/xingang-lover` | `romance.story` |
| 古墓探险 | `backend/data/preset_packs/lost-chambers` | `tomb.expedition` |
| 地牢远征 | `backend/data/preset_packs/underground-watchtower` | `dungeon.expedition` |
| 修仙飞升 | `backend/data/preset_packs/reincarnation-trials`、`mortal-path` | 原 State/Roll 与修仙领域定义，见 14、16 专题 |
| 宗门经营 | `backend/data/preset_packs/qinglan-sect` | `sect.foundation` |

新模块主规则要求恰好一份对应定义。资源、地点、配方、人物、任务和结局使用稳定 key 引用，中文名称用于展示；不能通过把名称改成“已救援”“已通关”取得资格。时间使用整数游戏分钟，无挂机离线产出。

可调整数值、地图、成本、目标和情节，不可编写脚本或新增未安装的行动。修改成功条件时同步 Runtime Outcome，修改公开结构时同步 Presentation。发布后通过创建新内容版本更新，不覆盖旧存档。

## 提供准确的叙事事实

纯模块实现 `NarrativeFactProvider.NarrativeFactPaths()`，从自己的 model 投影中选取最多 20 条短小核心路径，例如当前位置、生命、当前身份、进度和已达成终局。Campaign Provider 使用 `ProjectionView.FactPaths`；纯模块 Campaign 适配器自动添加 `gameplay.`。不要把完整库存、全部 NPC 或任务列表都声明为必选核心。核心总量最多 4 KiB，零和 false 保留原义，缺失明确为 unavailable。

平台从动作前后真实投影生成有界事实反馈，模块的 `Result.success` 若存在，仅表示该次游戏目标是否成功，不表示是否提交；没有该字段时平台不会自行补成成功。失败动作已经扣除的资源、推进的时间和新增伤势应由真实状态体现。物品名称与身份使用稳定版本定义，不用叙事别名代替物品 key。

Preview/Capability 的拒绝解释应描述真实、公开的前置，不泄露种子、隐藏结局条件或未来判定。不可用说明仅供模型理解，不能通过文案或工具名扩大执行权限。player 与 model 可见性不同的字段不会进入共享动作反馈；原有 model 专属上下文仍通过 model 投影提供。

关键事实在历史之后提供，动作反馈随原有事务和幂等结果保存；无需增加另一套状态存储或每回合审核模型。详细预算、恢复和协议见 [24 架构](../24-gameplay-module-architecture.md)、[08 上下文](../08-context-management.md)及 [19 Agent](../19-agent-tool-loop.md)。

## 生存探索：荒岛求援

`survival` 管理地点有限资源、背包、饥饿、口渴、生命、体力、天气与救援。主要配置为 `resources`、`locations`、`recipes`、`initial_stock`、小时消耗、天气风险、救援地点/装置/窗口和截止时间。

行动包括观察、采集、移动、制作、使用补给、休息、求援及放弃。正常循环是获取食水和材料、制作救援装置、抵达求援点、撑到救援窗口再求援；玩家必须真实持有装置，叙事描述不算物品。地图采空后不会刷新，制作原子扣料。

生命耗尽、截止时间到达或放弃结束本局。行动途中死亡不再发采集/制作产物或继续抽天气；休息同样流逝生存时间，不能免费补满。内容验证检查地点连接、资源引用与有限制作材料来源；更高天气风险和更慢采集仍须额外验证完整通关代价。

## 经营建设：山间客栈

`management` 使用公共生产组件维护库存、设施等级和队列，自行负责现金、订单、违约、日常支出及经营目标。配置包含资源买卖价、设施/升级成本、配方、订单物品/收入/期限/罚款、每日支出与目标。

可以建设、升级、采购、出售、安排生产、接单、交付、推进时间、完成经营和关店。示例路径：建厨房与客房、升级客房、接商队和香客订单，完成三轮餐食与三轮住宿生产，交付后确认完成。方案预览显示具体投入和工时，生产完成前库存不增加。

订单到期与每日支出随时间真实发生；现金不足形成经营风险，期限到达、资金恶化或关店按模块规则失败。完成必须同时达到设施、订单数和营业收入目标，不能靠来回买卖刷收入。买入后立即出售不允许价差套利。

设施/配方校验通过单调依赖闭包寻找资源来源，可检查最高设施等级的配方；不会把“某资源存在来源”当作已证明总资金和工期足够。

## 潜入逃脱：夜入封锁区

`stealth` 独立管理警戒、追捕、巡逻位置、门锁、情报、目标与工具。配置包含有向连接校验后的地点图、上锁/藏身/情报位置、巡逻路线与节拍、风险、撬锁工具、诱饵和期限。

通过观察巡逻、移动、潜入、搜取情报、开锁、藏身或诱导来取得目标，最后在可用出口由玩家确认脱离。到达目标点不自动完成逃脱；情报和门锁开通事实持久保存，已开的门可以返回。

暴露、追捕和时间消耗有实际后果，警戒失控、超时或放弃结束本局。撬锁工具有限，情报必须先真实获得。发布采用有限工具状态搜索验证目标与出口，最多 8192 个组合；超限显式要求简化配置。搜索不替代巡逻时机与风险平衡。

## 关系叙事：小镇旧约

`relationship` 维护三位以上人物、有限倾听事实、互斥事件选择、承诺、信任与结局。可配置每个人的初始信任及信息来源、事件前置和选择、承诺成本/期限/履约与违约影响、结局条件。

倾听得到来源明确的事实，选择事件分支可能创建承诺，投入有限精力履约后选择结局。重复倾听和重复同一选择不能无限刷信任，同一事件不能同时取互斥选项。跨人物承诺可以同时影响多方关系，成功检查事实、承诺和信任组合。

推进时间先处理到期，再处理行动收益，刚好到期不能用一次迟交把违约改成成功。尚有待履行承诺不能直接结束，过多违约、超时或主动离开可失败；不需要生命或战斗数值。示例提供两个不同结局，配置校验拒绝依赖互斥事实的结局。


## 恋爱养成：新港恋人

`romance`（`romance.story@1`）归属关系叙事类目，是数据驱动的恋爱养成引擎：数值、阶段、时段、行动、限时消息、情敌、事件、旅行、告白判定表、场景与结局全部写在主规则 definition 的 `content` 里，Go 只负责解释执行。示例包 `xingang-lover`「新港恋人」以 Challenge 模式运行。

主要行动：`romance.start`（选定角色）、`romance.act`、`romance.reply`、`romance.confess`、`romance.trip`、`romance.rival`、`romance.remember`、`romance.boundary`、`romance.choose`、`romance.abandon`。包未声明对应能力时（例如没有告白表、没有旅行目的地、没有消息种类、没有情敌池、没有可写列表或反应选项），Preview **直接省略**该动作，而不是返回一行灰色的「这个世界没有…」。

制作要点：

- **占位符**：`Note` / `Title` / `Label` / `Template` / `Reason` / `Expired` / `Active` 等面向人的字符串会扫描 `{...}`。合法键与 `fill`/`ref` 一致，包括 `stat.*`、`cond.*`、`list.*`、`flag.*`、`active.*`、`lead.*`、`arg.*`、`chose.*`、`confess.*`、`act` / `act.halves`、`rival*`、`deadline.*`、`stage*`、`text.*`，以及 `name` / `stage_name` / `slot_name` / `label` / `roll` / `rival_name` / `kind` 等。未知键在内容校验阶段报错；若仍漏网，运行时保留 `{key}` 原文，不再静默打印 `0`。
- **投影分组保留键**：`StatDef.Group`（以及列表/文本/揭示的 key）不得占用投影顶层字段，至少包括 `log`、`stage`、`lead`、`day`、`slot`、`weekend`、`outcome_key`、`last_result`、`facts`、`caps`、`messages`、`rivals`、`warning`、`hidden`、`stage_rank`、`stage_key`、`stage_day`、`intimacy_tier`、`choices`、`goal`、`pending_messages`、`known_likes`、`known_dislikes`、`scene`，以及本包其他顶层数值 key。示例包用 `her` / `you` 作为分组名是允许的。
- **玩家话题**：`romance.reply` 对 player 只暴露已解锁的喜好/雷区 key；model 仍可见全部。
- **随机预算**：内核每次 transition 最多 64 次抽样。规则表里大量 `dice` 时注意不要撞「随机用途重复或超出预算」。
- **终局**：告白与抉择场景走配置里的判定表/结局表；主动放弃走声明的 `AbandonAction`。

修改成功条件时同步 Runtime Outcome；阶段进度与积分仍走平台 `challenge.progress_rewards`。

## 古墓探险：失落墓室

`tomb` 与地牢共用 `components/expedition` 的地图、补给、负重、暂存战利品和返程基础，独立管理线索链、机关、照明与工具。配置由 `world`、`clues`、`traps`、初始照明和工具组成。

探索分支墓室、发现两段相关线索、处理机关、拾取目标遗物，再返回出口确认带出。解机关必须满足真实线索/工具条件；冒险尝试可能受伤或消耗照明，不依赖模型宣布谜底正确。

遗物进入携带区且占重量，在出口成功撤离才算完成；只在墓中看见或拿到目标不够。死亡、超时和放弃失败，撤退与完成有别。修改地图时需同时核验线索先后、机关所在节点、目标可达、有限工具、照明及携带重量。

## 地牢远征：地下哨塔

`dungeon` 在共用远征基础上维护持续敌人生命、攻击、护甲、补给、药品、休整与遭遇。配置包括 `world`、敌人及其节点、终点敌人、玩家攻防与治疗资源。

选择两条路线之一，交战消耗双方真实生命，合理使用有限药品和休整，击败目标后携物撤离。重进节点不重置敌人，重复击败不重新掉落。战斗、休息和治疗都有资源或时间成本，不能靠聊天回复恢复。

逃跑与提前撤退损失携带物的一半，数量向上取整；必须使用合法返程条件，不能任意传送。死亡或放弃按失败结局处理，提前撤离不冒充完成主目标。玩家当前伤势、物资、敌人存活状态和是否值得继续，是规则真正支持的取舍。

## 宗门经营：青岚宗重建

`sect` 复用生产与人员组件，独立维护弟子特长、资质、培养、传承、岗位、派遣、供养、凝聚力与危机。配置包含初始/可招募弟子、设施和配方、训练/传承成本、派遣任务的准备与产出、危机期限/防御需求、供养规则及重建目标。

主要行动：招募、训练、授业、建设/升级、生产、派遣、驻守、召回、推进时间、完成重建、解散。每名弟子同时只能占一个岗位，训练、生产、驻守和外派不能叠加；未归队的弟子不能参与防御。派遣按实际能力和服务端随机数结算，失败不发成功产出。

示例初始三名特长不同的弟子，需要招募第四名，建成两类设施并升级大殿，培养至少一名二级且获得传承的继承者，完成派遣并守住危机。玩家要在生产、培养、外出取回资源与留人防御间安排时间。

供养、任务归队与危机按时间顺序处理；毁灭后不再收获队列产出。内容验证检查招募/设施/训练/传承/供养的资源来源和防御能力上界，另对后期配方与弟子组合做菜单容量探测。资源来源和防御上界通过不代表每种排班都能在期限内成功。

## 修仙飞升

百年飞升劫使用 `cultivation.trial@2`，青河使用 `cultivation.campaign@2`；两者归同一主类型。单局以模块动作维护出生、时间、资源、三条飞升路线和死亡。青河采用归一化功法层级、闭关恢复、真实任务证据、最终渡劫及阶段积分；仍保留长期世界的领域表。

规则现状参考[单局](../14-ascension-trial-gameplay.md)、[长期](../16-cultivation-campaign-agent-reference.md)和[共同设定](../20-cultivation-shared-setting.md)。两包离线平衡样本与真实环境待验项见[25 验证记录](../25-core-gameplay-validation.md)。后续调整以真实实现与固定版本为准。


## 内容作者的退出声明

共用运行时必须声明 `AbandonAction` 对应 player-only、零参数 `Ending` 动作，内容保留完整动作目录和启用的 neutral 零积分退出 Outcome。平台发布时运行初始退出探针。其他玩家终局使用 Ending 元数据及公开参数方案；不得以 Prompt 绕过领域门槛。 完整协议见 [26 玩法结局生命周期](../26-gameplay-ending-lifecycle.md)。

玩法包交换、管理员版本控制、旧局继续、运行暂停和奖励实际来源：[27 玩法包交换与版本生命周期](../27-gameplay-package-exchange-and-versions.md)。该专题包含 JSON/ZIP 完整示例、结构 Schema、资源上限与部署检查。


## 恋爱养成：Effect 字段表与 Hooks 时机表

面向下一个 `romance.story` 包作者。字段以 `backend/internal/gameplay/romance` 的 `Effect` / `Hooks` 与 `apply` / 运行时调用为准；未列出的键不会被解析。

### Effect 字段表

规则表 `Rule.do`、行动 `effects` / 分类 `bonus`、消息过期效果、联系效果等处均可写 `Effect`。执行顺序与 `apply` 一致：先写入选择/结果与数值旗帜，再揭示与收集，最后阶段跳转、嵌套规则表、打开场景与终局。

| 字段 | 类型 | 含义 |
| --- | --- | --- |
| `stats` | `map[string]int64` | 对已声明的可写数值做增量；会乘以当前成功率百分数 |
| `set` | `map[string]int64` | 把已声明可写数值设为绝对值（仍受上下限/阶段上限约束） |
| `flags` | `map[string]int64` | 写入 `Flags[key]`；键支持占位符 |
| `until` | `map[string]int64` | 把旗帜写成 `当前 Day + N`（限时标记常用） |
| `add` | `map[string]int64` | 在现有旗帜值上累加；键支持占位符 |
| `rivals` | `int64` | 所有未出局情敌的 `progress` 增量（0–100），可选再被 `rival_cap` 截断 |
| `rival_cap` | `int64` | 与 `rivals` 联用：把全体活跃情敌进度上限压到该值 |
| `rival` | `int64` | 仅当前上下文情敌的 `progress` 增量 |
| `rival_out` | `bool` | 将当前上下文情敌标记为出局 |
| `reveal` | `int64` | 再揭开若干条喜好/雷区；会乘以成功率百分数 |
| `expire` | `bool` | 为 true 时把当前所有待处理消息的 `Day` 置 0，下一轮过期清理会吃掉它们 |
| `collect` | `string` | 把当前行动标签推入指定记忆列表 |
| `choice` | `string` | 记下本次选择 key，供后续 `{chose.*}` 引用 |
| `result` | `int64` | 覆盖告白/判定上下文的结果码（示例：`1` 成功、`2` 未到时机、`-1` 拒绝） |
| `confess` | `string` | 触发告白流程；值为 `final` 时走最终告白，其它非空值走普通告白 |
| `stage` | `string` | 进入指定阶段 key（须严格前进） |
| `table` | `string` | 立刻再跑一张规则表（有深度上限） |
| `scene` | `string` | 打开抉择场景；与 `force` 联用可在已有场景时强行替换 |
| `force` | `bool` | 配合 `scene`：已有未选场景时是否强制打开新场景 |
| `outcome` | `string` | 以该结局 key 结束本局；`note` 会作为结束原因填入 |
| `note` | `string` | 追加一条玩家可见日志；若同时有 `outcome` 则作为终局原因，否则只记日志。支持占位符 |

空 `Effect`（全零值）会被跳过。数值类增量在行动成功路径里常按掷骰得到的百分数缩放；旗帜、场景、终局等多数字段不吃这个百分数。

### Hooks 触发时机表

`hooks` 里每一项是一张规则表的 key；空字符串表示不挂表。引擎在对应时机调用 `table(hooks.X)`。

| 字段 | 触发时机 |
| --- | --- |
| `start` | `romance.start` 选定角色并应用 `contact` 之后、首轮晨间消息之前 |
| `night` | 跨日结算开头：日终衰减/日常增减之前 |
| `morning` | 新的一天开始、消息过期清理之后；若本局已结束则不再跑 |
| `schedule` | 同日晨间流程末尾：情敌日结、晨间消息与随机事件之后；若本局已结束则不再跑 |
| `after_action` | 任意成功落地的玩家动作末尾（已有角色时），写回日志与清理过期 slot 旗帜之前 |
| `success` | 单次行动判定**成功**并应用行动 `effects` 之后、分类 `bonus` 与行动 `after` 表之前 |

告白配置里的 `confess.hook` 不是这张总表的字段：它是告白结算写入记忆列表时用的文案模板，不要和 `hooks.*` 混用。
