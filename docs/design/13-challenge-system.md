# 13. 挑战内容、版本、玩法模块与结算

## 设计目标

挑战系统负责把一套可维护内容冻结成可验证、可复现的运行版本。它必须同时满足：

- 不同题材共享版本、对话、状态、判定、结算和奖励基础设施；
- 特殊规则仍运行在可信后端，而不是 Prompt 或前端；
- 已开始的对话不受后来发布影响；
- 普通用户只能看到公开投影，不能读取 Prompt、隐藏状态和验证条件；
- 模型不能决定结局是否成立或奖励发多少；
- 奖励可被其他玩法或平台功能作为前置条件。

## 聚合边界

~~~text
ChallengePack（稳定入口）
  `-- ChallengeVersion（不可变快照）
        |-- Manifest
        |-- Runtime Contract
        |-- Assets
        |     |-- Character
        |     |-- Persona
        |     |-- Scenario
        |     |-- Prompt
        |     `-- WorldBook
        `-- Outcome Rules（包含 reward_points）
~~~

Pack 只表达稳定入口和当前发布版本。所有会影响生成、状态、展示、验证和奖励的内容必须属于 Version 并进入内容哈希。

## Manifest

Manifest 是公开元数据与入口声明，不承载技术运行细节：

~~~json
{
  "id": "midnight-case",
  "name": "午夜疑案",
  "version": "1.0.0",
  "runtime_entry": "runtime/runtime.json",
  "description": "在午夜前保存证据并指出真凶。",
  "author": "Fatecraft",
  "icon": "search",
  "tags": ["悬疑", "调查"],
  "default_config": {
    "recommended_character": "case-narrator",
    "recommended_persona": "private-investigator",
    "recommended_scenario": "midnight-manor",
    "recommended_prompt": "midnight-case-v1",
    "required_worldbooks": ["midnight-case-rules"],
    "allowed_personas": ["private-investigator"]
  },
  "quick_start_guide": "检查现场并在倒计时结束前提出指控。",
  "features": ["证据完整度", "服务端指控判定", "通关积分"],
  "entry_requirement": {
    "type": "reward_balance",
    "reward_key": "fatecraft.points",
    "operator": "gte",
    "quantity": 1
  }
}
~~~

`entry_requirement` 可选，首批支持 `reward_balance` 与有界 `all/any` 组合。挑战列表只做体验提示；真正创建对话时后端重新检查当前用户余额。

## Runtime Contract

Runtime 与 Manifest 分离。玩法包通过 `pack.json.runtime_entry` 指向 Runtime 入口；入口只保存契约版本、引擎、模式和分层文件引用，导入器会严格解析并编译成数据库中的单一 Runtime 快照：

~~~json
{
  "$schema": "fatecraft.runtime.schema.json",
  "runtime_contract_version": 2,
  "engine": {"key": "core.generic", "version": 1},
  "mode": "challenge",
  "includes": {
    "state_schema": "state-schema.json",
    "actions": "actions.json",
    "settlement": "settlement.json",
    "presentation": "presentation.json"
  }
}
~~~

Runtime 子文件分别保存 `state_schema`、`actions`、`settlement` 和 `presentation`。Campaign 模式额外通过 `includes.campaign` 引用 Campaign 配置。入口和所有被引用文件必须位于当前玩法包目录内，不能使用绝对路径、脚本或任意表达式。入口格式的 JSON Schema 位于 `backend/data/schemas/fatecraft.runtime.schema.json`；后端还会执行跨文件和模块级语义校验。`value_labels` 的命名空间和值必须是稳定 key，标签不能为空且长度受限；未知 engine、版本、action、verifier、settlement handler、状态类型或前端控件都会阻止发布。稳定值不因本地化改变。

Campaign View 是玩法包提供的纯展示契约。`renderer` 当前只能为 `generic`；字段路径只能引用公开 Campaign Snapshot，集合通过 `item_fields` 声明可读标签，`visible_when` 仅支持存在、相等和有限数值比较。动作槽的 key 必须同时存在于当前 Runtime 的 `campaign.action_allowlist` 和玩法模块注册表，参数绑定只能读取公开快照或有限的 item/option/input 上下文。View 的数量、大小和嵌套深度受校验限制，不能声明权限、写库、随机或结算。

普通对话可在同一 Presentation 下声明 `guidance.groups`。每个项目必须是 `message` 或 `action`：前者进入普通用户输入和生成流水，后者只能引用当前 Campaign 白名单动作。后端按公开 state/snapshot 投影可见项，Campaign guidance 还必须通过 `field_ownership` 的 player 可见校验；前端通过通用 `GuidanceRenderer` 展示；不从模型正文猜测或提取选项。

Runtime 的 `actions.rolls[*].requires` 同时是判定门禁和详情数据的唯一来源。判定提交成功后，服务端把门禁时使用的可信状态冻结为 `conditions` 快照，并把 `roll_outcome.breakdown`、成功区间和 `details_version` 写入 `display_messages.metadata`。Presentation 中没有公开标签或 State Schema 可见性不是 `player` 的条件，不得把技术 key、原始期望值或实际值下发给玩家；前端只在支持的详情版本下渲染折叠面板，历史缺失详情的消息保持可读。该快照不参与结算和模型提示，也不改变 Runtime 内容哈希或数据库结构。

最小 Campaign View 示例：

~~~json
{
  "campaign_view": {
    "renderer": "generic",
    "header": {"title_path": "world.current.name", "time_path": "campaign.game_time", "time_format": "game_time"},
    "sections": [
      {"key": "character", "title": "角色", "order": 10,
        "fields": [{"path": "character.name", "label": "姓名", "widget": "title", "order": 10}]}
    ],
    "actions": [
      {"key": "some.inspect", "label": "查看", "placement": "character"}
    ],
    "empty_states": {"character": "暂无角色资料"}
  }
}
~~~

长期内容改用 `mode=campaign` 并声明 `campaign_config`。该模式不强制单局 Outcome 与 settlement，但必须声明模块允许的生命周期、投影、上下文策略、1 至 64 个 `action_allowlist` 动作、初始地点和世界时钟上限；完整配置进入内容哈希。

## 玩法注册表

通用挑战核心依赖 `gameplay.Registry`，不导入具体题材包。组合根显式安装：

- `core.generic@1`：状态相等、状态集合、数值条件、成功判定、最少回合、all/any、可信结局验证；百年飞升劫由独立 `cultivation.trial@2` 以受保护模块状态和可信动作维护完整单局；
- `cultivation.campaign@2`：修仙 Campaign 生命周期、投影、上下文和完整领域动作集合；Runtime 只能从模块声明的 `campaign_actions` 中启用白名单，不允许用自由状态 JSON 冒充领域事实；
- `campaign.generic@1`：直接使用公共世界组件；
- 生存、经营、潜入、关系、古墓、地牢、宗门七个纯运行模块，完整目录见[24 玩法架构](24-gameplay-module-architecture.md)。

模块 descriptor 列出允许的 action、Campaign action、verifier 和 settlement handler。管理员界面通过 `/api/admin/gameplay-modules` 读取目录并生成选项，不在前端硬编码修仙能力。Campaign 动作的 Definition、Capability 和事务 Handler 契约见 [17-generic-campaign-action-capabilities.md](17-generic-campaign-action-capabilities.md)。

纯内容玩法若能使用 `core.generic`，只需新增数据，不需要修改 Go。只有无法由受限通用条件安全表达的规则，才新增后端模块。

定义类型、Schema 和内容校验归当前主模块；不再使用全局修仙类型枚举。发布会检查所有可选 Persona 的初始化、双方行动预览、三类投影和展示路径/控件，拒绝缺失必要行动与未知渲染器。初始空列表、时间和随机可行性仍需流程测试。默认初始化使用 `backend/data/bootstrap.json`；当前清单包含全部八类九个内置包；启动只补齐缺失包，不覆盖已有包或启用状态。

## 结局规则

每个版本的 `outcome_key` 稳定且唯一：

~~~json
{
  "outcome_key": "case_solved",
  "category": "success",
  "title": "真相大白",
  "tone": "success",
  "reward_points": 100,
  "verifier_key": "core.conditions",
  "verifier_config": {
    "conditions": [
      {"type": "state_number", "path": "investigation.clue_integrity", "operator": "gte", "value": 80},
      {"type": "successful_roll", "key": "investigation.final-accusation"}
    ]
  }
}
~~~

标题与色调只用于展示，不参与安全匹配。模型只提交 outcome_key；后端在 Prepare 和最终事务内各验证一次。

`core.conditions` 的 `state_in` 用于由玩法内容声明离散状态集合，例如 `{"type":"state_in","path":"case.status","values":["solved","closed"]}`。`values` 只能包含 1 到 64 个不重复的字符串、布尔值或数字，状态路径必须由该版本的 State Schema 声明。平台只做集合成员判断，不解释值的题材含义。

## 结局积分

每个 outcome 直接保存 `reward_points`，范围为 0–9007199254740991；省略视为 0。正整数在可信通关后自动进入 `fatecraft.points`，0 正常完成且不创建零金额流水。积分与局内财富、库存无换算关系。

青河还通过版本定义 `cultivation.progress_rewards` 保存阶段规则和总预算，阶段积分随合法动作自动核验；后台明确区分终局积分与“阶段 + 终局”总额。阶段事实不随剧情回档，账号事务和实际收益字段见 [21](21-campaign-outcome-rewards.md)。单局与长期的当前规则及本地证据见 [14](14-ascension-trial-gameplay.md)、[16](16-cultivation-campaign-agent-reference.md)、[25](25-core-gameplay-validation.md)。

账号只保留积分与剧情改写次数。玩家用积分兑换改写套餐，后台运营规则见 [22 奖励管理](22-reward-management-and-exchange.md)。Campaign 正积分结局必须由当前版本真实终局任务引用，任务核验、积分和存档完成同事务；详见 [21 终局积分](21-campaign-outcome-rewards.md)。

## 生命周期

### Challenge Pack

- `active`：有当前发布版本，可供普通用户选择；
- `hidden`：不在普通列表展示；
- `retired`：停止新建，但历史对话仍保留固定内容。

### Challenge Version

- `draft`：管理员可修改；
- `published`：不可原地修改，可创建新对话；
- `retired`：不再作为新入口，历史仍可读取。

## 新建与复制草稿

新 Pack 的首个草稿生成安全的 `core.generic` Runtime 骨架。管理员需要补齐 Manifest、入口资产、展示分组和至少一个 outcome。

已有当前版本时，“复制新版本”会：

1. 生成新的 version ID；
2. 更新 Manifest 版本号；
3. 深复制 Runtime 与全部资产；
4. 为 outcome 生成新 ID，保留固定积分；
5. 不改变已有对话或旧发布版本。

## 管理员编辑器

管理页按区域维护：

1. 基本信息和入口资产选择；
2. 角色、Persona、场景、Prompt 与世界书资产；
3. engine、允许动作和状态 Schema；
4. 玩家状态分组、标签、控件和术语；
5. outcome、verifier 与通关积分；
6. 高级 JSON 导入/导出。

保存草稿不等于发布。点击发布会先保存当前表单，再执行完整发布校验，避免误发布数据库中的旧草稿。

## 发布校验

发布至少检查：

- Manifest ID、名称、说明与 Pack 一致，版本一致；
- 推荐资产和允许 Persona 都属于当前版本；
- Prompt 包含启用且必需的 `system_master`、`settlement_security`、`agent_tool_loop`；
- 必需世界书条目 ID 唯一、内容非空、触发配置合法；
- Runtime engine 与版本已安装；
- 模型动作和 settlement handler 被模块允许；
- State Schema 路径唯一，类型、默认值、约束、可见性和更新权限合法；
- presentation 只引用 player 可见路径和平台固定控件；
- outcome key 唯一，category/tone 合法，verifier 已安装且配置有效；
- 通用数值条件引用已声明的数值字段；
- Prompt 明确引用每个 outcome key；
- 每个结局积分是非负安全整数；
- 能力成本只引用积分或剧情改写次数，数量不越界；
- 入口余额条件只引用两项固定 key，条件树不过深。

校验、哈希、版本状态切换和 Pack 当前版本切换在版本行与 Pack 行锁事务内完成；同一 Pack 的并发发布会串行化，最终只有一个 current published version。

## 内容哈希

哈希输入包含规范化排序后的：

- Manifest；
- Runtime 的 engine、State Schema、actions、settlement、presentation；
- 所有资产的 kind、slug、name、content 与排序；
- outcome key、展示、verifier 与配置；
- reward 绑定、配置和公开快照。

数据库行 ID、创建时间等非内容字段不进入哈希。相同语义内容必须得到相同 SHA-256；任一运行规则变化必须改变哈希。

## 内置玩法导入

仓库包目录包含：

~~~text
data/preset_packs/<slug>/
  pack.json
  runtime/
    runtime.json
    campaign.json                 # 仅 Campaign
    state-schema.json
    actions.json
    settlement.json
    presentation.json
  characters/
  personas/
  scenarios/
  prompts/
  worldbooks/
  GAMEPLAY_DESIGN.md（推荐）
~~~

空数据库首次启动时，在 PostgreSQL advisory lock 下导入并发布全部内置包。导入事务创建 Pack、版本、Runtime、资产及包含积分的 outcome，最后执行与管理端相同的发布校验。

数据库已有任何 Pack 时，启动不自动升级。管理员在“内置内容”查看仓库版本和哈希；新版本可显式“导入并发布”，仓库内容与数据库当前快照不一致时显示“覆盖现有内容”。导入前展示结局积分及前后规则，确认时提交 preview_token 和覆盖标记；服务端在配置锁内核对预览，并重新执行完整发布校验。

同一 `slug + version` 内容变化时，覆盖事务先锁定目标版本并检查引用。无人使用时可以删除旧草稿/快照并以新 ID 重建、校验、计算哈希和发布；已被对话或 Campaign 引用时仍返回 409，要求提升版本号。仓库版本低于或等于当前版本时也不能通过覆盖回退。发布更高版本时，旧版本标记为 `retired`，其普通 active 对话自动封存为只读；旧快照不会被删除，用户可从历史对话分叉出绑定当前版本的新对话。

## 公开投影

普通挑战 API 只返回：

- 名称、说明、版本、图标、标签、特色和快速指南；
- 允许选择的 Persona 摘要；
- outcome 的 key/category/title/tone/reward_points；
- 每个结局的固定积分概要；
- 编译后的公开 Runtime Presentation 与玩家 capabilities；
- 当前用户是否满足入口要求及不可用原因。

不返回 Prompt、WorldBook、完整 State Schema、隐藏状态、verifier_config_config 和奖励内部条件。

## 对话固定与运行 Bundle

创建对话时后端：

1. 按当前用户检查已发布挑战和 entry requirement；
2. 校验 Persona 属于允许列表；
3. 固定 version ID、content hash 和资产 ID；
4. 按 State Schema 初始化状态；
5. 以后始终用该固定版本组装 Bundle。

管理员发布新版本不会改变已有对话绑定的 version ID、hash 或内容；旧版本普通 active 对话会被标记为 archived，只读浏览，不会跟随 Pack `current_version_id` 漂移。

发布版本不可原地修改，因此后端可以按 `version ID + content hash` 长期缓存完整文档及其编译结果。Bundle 不作为整份共享对象缓存：Character、Persona 和 Scenario 仍按 conversation 固定的 asset ID 从版本索引选择并复制，避免不同对话互相污染；Prompt 和世界书条目也以私有 slice 交给当前 attempt。所有对话使用缓存前都会重新核对自己的固定 hash。

公开挑战列表复用版本级展示投影，但 Pack 当前入口、用户奖励余额、入口条件和可用性仍实时读取。发布新版本通过新 version ID 自然建立新缓存项；草稿和后台编辑态永不进入长期缓存。

## 结算事务

模型提出 `complete_challenge` 后：

1. 保存前用持久化状态、成功 roll key 和回合数预校验；
2. 校验通过才保存候选成功叙事；
3. 事务内锁 conversation 并确认 active；
4. 检查该对话没有 `challenge_run`；
5. 重建可信 evidence 并再次执行固定 outcome verifier；
6. 读取固定结局积分；client 路由实际所得为 0；
7. 写 run，通过 GrantInCurrentTx 写实际账本、余额和积分快照；
8. 将对话置为 ended；
9. 提交后发送通用 `challenge_completed`。

唯一约束和来源幂等键保证并发重试不会重复结算。0 积分保存在结算详情中，不造授予流水；负数或越界失败回滚。积分与对话完成同事务。验证不满足是业务拒绝，不是系统错误。

奖励管理从 /admin/rewards 集中定位玩法版本，规则仍通过草稿/发布编辑；积分积累与兑换见 [22-reward-management-and-exchange.md](22-reward-management-and-exchange.md)。

## 终局与主动退出

Challenge 的失败、成功和中立退出都必须由可信 Runtime 状态产生，模型正文不能填写结局或阻止已提交的终局。终局展示读取固定 Outcome 的 `category/title/reward_points`，不能把所有已结束状态渲染成成功。已有玩法的玩家专属退出动作继续复用原动作目录；长期玩法顶部结束入口走 Campaign 的主动中止事务。

青河修仙志固定版本包含 `perished`、`lifespan_exhausted` 和 `abandoned`，分别表示身亡、寿尽和玩家主动中止；后两者不产生终局积分。服务端在保存状态、事件、结局、奖励和对话结束时保持事务一致，重复请求按原 action ID 幂等返回。

## 内容演进规则

- 已发布版本永不原地修改；管理员覆盖未使用的内置版本时采用删除后重建，而非修改原快照。
- 稳定 outcome key 的语义变化应提升玩法版本。
- 两项账号余额身份由平台固定，玩法只能配置结局积分。
- 删除后端 verifier 前，必须确认没有仍需运行的已发布版本引用。
- 前端新增可选控件时仍要保留未知控件安全降级。
- 新题材不得把自身字段塞回通用 challenge、事件或 App 顶栏。


## 管理员启用开关

玩法包复用现有 `status`：`active` 为启用，`hidden` 为停用；开关只允许对当前有已发布版本的包操作。`PUT /api/admin/challenges/:id/enabled` 接受必填布尔 `enabled`，仅管理员可调用，在包行锁内更新状态；未发布包不能启用。公开列表、详情与开局选择检查启用状态；已有对话的当前版本校验允许停用包继续，已退役版本仍按原规则只读。开关不修改固定内容哈希、奖励规则或存档，也不需要新迁移。内置目录响应携带 `pack_id`、`enabled`，导入状态与启用状态分别显示。首次发布默认启用，更新已有版本保留停用状态。
