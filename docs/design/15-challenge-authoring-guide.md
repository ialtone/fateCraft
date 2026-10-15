# 15. 通用玩法包开发指南

## 目的与适用范围

本文说明如何为 fateCraft 新增不限题材的玩法。玩法可以是修仙、悬疑、生存、经营、历史推演或其他文字挑战；不要为了复用现有系统，把线索、能源、声望或金钱伪装成境界与灵石。

完整玩法由四部分组成：

1. 叙事内容：Character、Persona、Scenario、Prompt、WorldBook。
2. 运行契约：State Schema、允许动作、玩家能力和展示布局。
3. 可信规则：已安装玩法模块的动作、定义与 verifier。
4. 账号奖励：稳定 outcome 和固定 `reward_points`，支持的模块还可声明阶段积分；账号资产仅积分与剧情改写次数。

## 先判断是否需要新后端模块

### 纯内容或通用规则玩法

先按[模块目录](24-gameplay-module-architecture.md)选择匹配玩法；七种新增模块的配置见[制作指南](gameplay-modules/README.md)。仅需要受限 State/Roll 条件的轻量玩法可以使用 `core.generic@1`。它支持：

- 已声明数值状态阈值；
- 状态等值；
- 服务端成功判定 key；
- 最少回合；
- 有界 `all/any` 组合；
- 固定结局积分，不从模型可写数值计算倍率收益。

例如既有可信来源能够保证线索完整度时，可验证“线索完整度至少 80 且最终指控判定成功”；需要来源、互斥选择和真实成本的过程规则，应由模块行动维护，不能仅靠模型填写完整度。

### 特殊可信规则玩法

只有以下情况才增加 `backend/internal/gameplay/<module>`：

- 规则需要题材特有的状态语义或跨字段不变量；
- 验证无法由受限条件树安全表达；
- 需要新的后端动作或资源计算方法；
- 结果必须由服务端专用算法产生。

模块必须在 `gameplay/bootstrap` 显式注册，并通过 descriptor 声明动作、定义 Schema/校验、投影及结算能力。七个新模块和 `cultivation.trial@2` 通过 RuntimeModule 接口执行；内容必须保留其完整动作集合，可用条件由模块预览决定。后台切换模块会重建权限与动作骨架，仍须补齐规则/结局/展示后通过发布校验。管理员不能上传 Go、JavaScript、SQL 或任意表达式。

## 标准目录

~~~text
backend/data/preset_packs/<challenge-slug>/
  pack.json
  runtime/
    runtime.json
    campaign.json                 # 仅 Campaign
    state-schema.json
    actions.json
    settlement.json
    presentation.json
  definitions/
    rules.json                    # 一个 JSON 定义数组，可按职责拆分
  GAMEPLAY_DESIGN.md               # 可选，也可链接 docs/design 的正式规格
  characters/
    narrator.json
  personas/
    default-player.json
  scenarios/
    opening.json
  prompts/
    gameplay-v1.json
  worldbooks/
    rules-v1.json
~~~

玩法规格应完整记录目标、循环、状态、判定、结局、奖励、可信边界和平衡参数。可以放在 `GAMEPLAY_DESIGN.md`，也可集中在 `docs/design`（内置修仙使用 14、16、25），避免保留重复且不一致的设计源。Markdown 不被运行时解析。

`characters/`、`personas/`、`scenarios/`、`prompts/` 和 `worldbooks/` 下的每个 `*.json` 都会作为资产加载；`runtime/` 和 `definitions/` 下的文件按各自契约解析。不要保留 Manifest 不再引用的旧 Prompt 或世界书，否则它们仍会进入版本、管理页和内容哈希。

## 标识与版本

- Pack slug：小写字母、数字和连字符，例如 `midnight-case`。
- 版本：稳定 `major.minor.patch`，新包可以从 `1.0.0` 开始，现有包按实际变更升版。
- 资产 slug：版本内按 kind 唯一。
- outcome key：版本内稳定，例如 `case_solved`。
- roll key：服务端证据稳定，例如 `investigation.final-accusation`。
- reward key：全局命名空间，例如 `fatecraft.points`。
- gameplay engine/verifier key：只能使用后端目录返回的已安装值。

标题和中文文案可以调整，安全规则只能引用稳定 key。

## `pack.json`

### 基本信息与入口

~~~json
{
  "id": "midnight-case",
  "name": "午夜疑案",
  "version": "1.0.0",
  "runtime_entry": "runtime/runtime.json",
  "description": "在钟楼敲响十二下前保存证据并指出真凶。",
  "author": "fateCraft",
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
  "quick_start_guide": "检查现场、保护线索并在时间耗尽前提出最终指控。",
  "features": ["证据完整度", "倒计时", "服务端判定", "通关积分"],
  "dependencies": []
}
~~~

Manifest 的 ID、名称和说明必须与 Challenge Pack 一致。所有推荐资产、必需世界书和允许 Persona 都必须存在于同一版本。

### 入口奖励前置条件

要求账号至少持有 1 积分（仅检查余额，不是局内调查凭证）：

~~~json
{
  "entry_requirement": {
    "type": "reward_balance",
    "reward_key": "fatecraft.points",
    "operator": "gte",
    "quantity": 1
  }
}
~~~

组合条件：

~~~json
{
  "entry_requirement": {
    "type": "any",
    "conditions": [
      {"type": "reward_balance", "reward_key": "fatecraft.message-rewrite-credit", "quantity": 1},
      {"type": "reward_balance", "reward_key": "fatecraft.points", "quantity": 3}
    ]
  }
}
~~~

条件树最多 4 层、32 个节点。当前 entry requirement 只检查持有，不消费；需要消费型入口时必须先增加后端原子“检查 + 扣除 + 创建对话”流程，不能只在前端扣数。

### Outcome

`pack.json` 的 `outcomes` 在导入时转换为版本化数据库规则：

~~~json
{
  "outcomes": [
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
  ]
}
~~~

`category` 只能是 `success/failure/neutral`；`tone` 只能是 `success/error/warning/info`。Prompt 应解释合法结局及当前模块的确认方式；`core.generic` 可提出候选，纯模块由真实行动形成终态，player 专属选择必须在玩家界面确认。

通用条件支持 `state_equals`、`state_in`、`state_number`、`successful_roll`、`minimum_turns` 以及嵌套的 `all/any`。其中 `state_in` 适合枚举式门槛：

~~~json
{"type":"state_in","path":"progress.tier","values":["master","legend"]}
~~~

`values` 必须包含 1 到 64 个不重复的 JSON 标量，`path` 必须在本版本 State Schema 中声明。等级顺序及哪些值满足门槛必须由玩法包明确列出，不要依赖平台代码解释题材名称。

### 通关积分

在上述 outcome 中填写 `reward_points`（例如 100）。非负安全整数，省略或 0 均不发积分；0 积分结局仍正常完成。单局与 Campaign 使用同一字段，发布后随版本固定。关系表修仙 Campaign 的正积分结局必须由本版本真实终局任务引用；纯 Campaign 模块使用自己的可信完成状态，不能硬塞修仙任务。

不创建通用奖励类型目录或收益倍率计算器。青河的 `cultivation.progress_rewards` 定义阶段、任务/胜利前置与总预算，阶段资格在合法动作后自动核验；后台结局编辑器显示“阶段 + 终局 = 总预算”，发布时必须一致。不要在 Prompt、action 或 narrative 中计算平台奖励，也不要把局内灵石、碎片折算或额外扣除来发账号奖励。事务、幂等与直连边界见 [21 Campaign 阶段与终局积分](21-campaign-outcome-rewards.md)。

## `runtime/`

Runtime 使用 `runtime/runtime.json` 作为入口。它必须声明 `runtime_contract_version: 2`、`$schema: fatecraft.runtime.schema.json`、引擎、模式和分层文件引用：

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

所有 `includes` 路径必须是当前 `runtime/` 目录内的相对 JSON 文件，并符合 `backend/data/schemas/fatecraft.runtime.schema.json` 的入口 Schema。导入器会先解析这些文件，再编译为数据库中的单一 Runtime；运行时不会动态读取玩法包源文件。Campaign 模式必须额外声明 `campaign: "campaign.json"`。

### Engine

~~~json
{"engine": {"key": "core.generic", "version": 1}}
~~~

可用模块从管理员“玩法能力”或 `GET /api/admin/gameplay-modules` 获取。不能写一个尚未安装的 key。当前修仙分别是 `cultivation.trial@2` 和 `cultivation.campaign@2`；engine version、内容 version、runtime_contract_version 各有独立职责，不能互相代替。

### 版本化定义

根目录可选 `definitions.json` 与 `definitions/*.json` 会合并为定义列表，同一 kind/key 不得重复。青河现在只使用 definitions 目录，按境界、功法、出身、物品、装备、配方、市场、证物、地点、势力、NPC、事件、任务、遭遇、飞升、阶段与训练拆分。后台“规则定义”根据模块 Schema 编辑，并做发布引用校验。

百年飞升劫的 `cultivation.trial_rules:life` 控制成长、时间、概率、准备和死因；青河的 `cultivation.origins`、`cultivation.training`、`cultivation.ascension`、`cultivation.progress_rewards` 分别负责出身、闭关恢复、飞升与阶段积分，具体字段以模块 Schema 和 [16](16-cultivation-campaign-agent-reference.md) 为准。`market.items[].restock_target_stock` 可配置周转目标：每周期按 restock_quantity 补足缺货或消耗超额库存；不填时只补到 max_stock，配置目标时必须有正的 restock_quantity。

Prompt 和世界书说明设定与动作意图，不再复制整套价格/概率表。任何新定义字段必须先有后端解析、语义校验和执行，不能只在 JSON 或模型文本中声明。

### State Schema

~~~json
{
  "state_schema": {
    "schema_version": 1,
    "fields": [
      {
        "path": "investigation",
        "type": "object",
        "default": {},
        "visibility": "player",
        "update_policy": "model"
      },
      {
        "path": "investigation.clue_integrity",
        "type": "integer",
        "default": 0,
        "visibility": "player",
        "update_policy": "system",
        "constraints": {"min": 0, "max": 100}
      },
      {
        "path": "investigation.solution",
        "type": "string",
        "default": "管家",
        "visibility": "internal",
        "update_policy": "system"
      }
    ]
  }
}
~~~

支持类型：`string/integer/number/boolean/enum/object/list`。一个发布版本最多声明 256 个状态字段；该上限同时约束状态工具快照及服务端校验规模。

可见性：

- `player`：普通用户 API 可见；
- `model`：模型上下文可见但普通用户不可见；
- `internal`：仅后端可信逻辑可见。

更新权限：

- `model`：结构化响应可更新；
- `system`：仅后端服务更新；
- `immutable`：初始化后不可修改；
- `module`：仅已安装纯模块维护，普通状态工具（包括系统状态 PATCH）不能覆盖。

上述调查字段是契约片段，仍须配套可信动作或服务端判定产出线索完整度；不能为方便通关而改回 model。纯 Challenge 模块使用隐藏 `module_state` 和公开 `gameplay`；模块动作集合与 actor 权限必须完整保留。

数值可用 `min/max`，字符串和 list 可用 `min_length/max_length`，enum 必须提供仅由非空字符串组成的非空 `values`。默认值必须满足声明类型和约束。

只有 object 可声明 `allow_children=true`。动态子路径适合非 Campaign 玩法的纯展示扩展，不适合数值 verifier；需要验证的路径应显式声明类型。Campaign Runtime 禁止无边界动态对象，长期事实必须放入 `campaign.field_ownership` 并由领域 Action 写入。

### Campaign

长期玩法使用 `mode=campaign`，并固定可信模块提供的生命周期、投影、上下文策略和动作白名单：

~~~json
{
  "engine": {"key": "some.campaign", "version": 1},
  "mode": "campaign",
  "campaign": {
    "lifecycle_handler": "some.campaign_lifecycle",
    "projection_handler": "some.campaign_projection",
    "context_policy": "some.campaign_context",
    "action_allowlist": ["some.move", "some.inspect"],
    "initial_location_key": "world.start",
    "world_clock": {"calendar_key": "world.default", "max_due_events_per_turn": 16}
  }
}
~~~

`action_allowlist` 必须包含 1 至 64 个无重复稳定 key，且每个 key 都属于当前 engine descriptor 的 `campaign_actions`。导入包只能选择已安装动作，不能定义处理器、扩大 actor 权限或通过 Prompt 创造新副作用。运行时由内部 `ActionCapability` 产生当前 revision 的动态 Schema、候选组合和数值上限，协调器只把可用能力转换为 `fc_action_*` 工具；具体协议见 [17-generic-campaign-action-capabilities.md](17-generic-campaign-action-capabilities.md)。

### Actions 与平台能力

~~~json
{
  "actions": {
    "model": ["update_state", "roll", "complete_challenge"],
    "player": [],
    "capabilities": {}
  }
}
~~~

玩法 Runtime 只声明题材相关的模型/玩家动作。剧情改写（`message_rewrite`）是平台级能力，统一通过 Runtime API 的 `platform_capabilities` 对外描述；挑战包不能声明、改名、禁用或修改其成本。后端执行时仍会重新验证对话归属、平台策略、消息类型和奖励余额；奖励消费与分支修改同一事务。

### Settlement

当前通用值：

~~~json
{"settlement": {"handler": "core.challenge_completion"}}
~~~

### Presentation

~~~json
{
  "presentation": {
    "state_panel_title": "调查进展",
    "state_sections": [
      {
        "key": "case-progress",
        "title": "案件",
        "order": 10,
        "fields": [
          {
            "path": "investigation.clue_integrity",
            "label": "线索完整度",
            "widget": "progress",
            "unit": "%",
            "order": 10
          },
          {
            "path": "investigation.suspect",
            "label": "当前嫌疑人",
            "widget": "text",
            "order": 20
          }
        ]
      }
    ],
    "completion_view": {
      "replay_label": "重新调查",
      "back_label": "返回案件",
      "empty_reward_text": "本次没有获得调查奖励"
    },
    "terminology": {
      "challenge": "案件",
      "outcome": "结案结果",
      "reward": "调查奖励"
    }
  }
}
~~~

控件只允许 `text/number/progress/badge/boolean/list/item_list`。展示只能引用 player 可见的显式状态字段。未声明字段不会自动用英文 key 展示。

Campaign Runtime 还可以声明 `presentation.campaign_view`，由通用 `CampaignViewRenderer` 消费：

~~~json
{
  "campaign_view": {
    "renderer": "generic",
    "title": "长期世界",
    "header": {"title_path": "world.current.name", "time_path": "campaign.game_time", "time_format": "game_time"},
    "sections": [
      {"key": "character", "title": "角色", "order": 10,
        "fields": [
          {"path": "character.name", "label": "姓名", "widget": "title", "order": 10},
          {"path": "character.realm_name", "label": "境界", "widget": "badge", "order": 20}
        ]}
    ],
    "actions": [
      {"key": "some.inspect", "label": "查看", "placement": "character"}
    ],
    "empty_states": {"character": "暂无角色资料"}
  }
}
~~~

字段 `path` 只能指向后端公开 Campaign Snapshot；集合字段使用 `item_fields` 声明条目路径、标签、单位和 `value_namespace`。`visible_when` 仅支持 `exists`、`equals`、`not_equals` 或有限数值比较，不能写脚本。动作槽可选 `source_path` 遍历公开集合，也可用 `item_path` 展开父条目中的子集合；参数使用 `from: item.*`、`from: parent.*`、`from: snapshot.*` 或固定 `value` 绑定。动作 key 必须同时在 `campaign.action_allowlist` 和模块注册表中，视图条件只改变按钮显示，不能替代服务端事务校验。

`list` 用于特质、任务和风险等数组；`item_list` 可以展示物品数组，也可以把 player 可见对象渲染为紧凑键值列表，适合动态属性、行囊和人物关系。player 对象允许动态子字段时，普通状态接口会保留这些动态内容，但仍会递归剔除 Schema 中更具体的 `model/internal` 子字段。玩法必须为包含技术英文键的固定字段单独配置中文标签，不要依赖前端猜测翻译。

### `value_labels`

玩法状态、投影和结局中的稳定枚举值通过 `presentation.value_labels` 提供玩家文案，映射随 Runtime 版本进入内容哈希。例如 `campaign.relationship.public_stance.neutral` 可以映射为“中立”。命名空间应描述语义路径，值 key 保持规则中的稳定值；不要把中文写入动作参数、数据库字段或事件。未知技术值在玩家界面显示为安全占位或被省略，不得回退为英文 key。动态 `item_list` 子字段必须提供标签或可读名称，不能依赖前端 `Object.entries` 猜测。

## 账号奖励边界

玩法包声明固定结局积分；支持阶段机制的模块通过自身定义声明阶段积分与预算。平台固定 `fatecraft.points` 与 `fatecraft.message-rewrite-credit` 两项余额；导入器拒绝旧的 `rewards.json` 文件，Manifest 和版本保存拒绝旧 `reward_rules` 等未知字段。注册赠送和兑换套餐独立由后台管理，不写入玩法包。

## Character、Persona 与 Scenario

Character 是叙事角色或主持者，至少包含 `id/name/system_prompt/first_message`。`first_message="__AUTO_GENERATE__"` 表示用隐藏触发消息自动生成开场；固定文本则直接作为第一条 assistant 消息。

Persona 是玩家进入玩法的身份模板，至少包含 `id/name/description/prompt`。普通用户只能从 Manifest 允许的 Persona 选择，不能修改原始内容。

Scenario 至少包含 `id/name/description/opening`，描述地点、初始冲突和可观察事实。不要在 Scenario 泄露 internal 答案。

## Prompt

Prompt 资产必须包含三个启用、必需、system 角色核心分段：

1. `system_master`：叙事身份、玩法循环和权限边界；
2. `settlement_security`：outcome key、可信证据和禁止伪造奖励；
3. `agent_tool_loop`：说明普通文本与真实工具调用的边界、何时必须先调用工具，以及工具结果的权威性。

建议再拆分时间、资源、判定和文风分段，避免一个超长文本难以维护。Prompt 应做到：

- 使用稳定 roll key 和 outcome key；
- 明确玩家声明不是已发生事实；
- 判定成功后才写入相应结果；
- 无可信事实变化时直接返回面向玩家的普通文本，不输出 JSON 信封、状态清单或工具参数；
- 需要状态、判定、结算或 Campaign 领域变化时，只调用当前步骤提供的一个工具并等待结果；
- 通用挑战按 Runtime 使用 `fc_complete_challenge`，参数仅包含 outcome_key 和 summary；纯模块通过当前开放的 `fc_action_*` 达成终态，不另造自由结算；
- Campaign 与纯 Challenge 的领域动作只调用当前步骤实际提供的 `fc_action_*`，不提交 action ID 或 revision；
- 工具拒绝后依据公开 tool result 继续普通叙事，不声称动作已成功；
- 不提交奖励 key、数量、倍率和余额。

挑战 Prompt 可以解释业务语义，但不能发明平台工具、改变 Tool Schema、State Schema 权限或动作白名单。平台会在所有历史之后注入最终 Agent 契约，并通过 Provider 原生字段发送当前步骤工具定义；玩法包不应教授任何 JSON 回复信封，也不应要求模型用代码围栏模拟 Tool Call。

## WorldBook

每本世界书包含稳定 `id/name/description/entries`。每个 entry 至少有：

- 唯一 ID、标题和非空内容；
- `constant` 或非空关键词；
- enabled、priority、budget_group；
- notes 和 metadata。

高频核心规则可 constant 注入，路线和地点细节用关键词按需激活。关键词不要带模板残片或无意义后缀。世界书是模型知识，不是后端胜利证明；真正结局条件必须由可信模块状态与 outcome verifier 共同验证。

## 判定设计

每个安全相关判定需要：

- 稳定英文 key；
- 玩家可读 title；
- 清晰 category；
- 合理属性和权重；
- 与风险相称的难度；
- 成败后果。

Prompt 和世界书引用同一个 key。Verifier 的 `successful_roll` 也使用 key，不用中文标题。

## 奖励设计

为每个结局设置容易理解的固定积分，局内资源继续服务于原玩法。账号奖励自动到账，积分可兑换剧情改写次数。注册、管理发放、套餐和收支见 [22 奖励管理](22-reward-management-and-exchange.md)。

通关积分进入玩法版本；注册赠送和兑换价格不进入玩法哈希。修改后两者不应改变旧存档的通关所得。

## 管理端工作流

不修改仓库也可以在“管理 -> 挑战内容”创建草稿：

1. 新建 Pack 和版本。
2. 在“基本信息”填写 Manifest 与入口。
3. 在“叙事资产”新增并填写各类资产。
4. 在“玩法运行”选择已安装模块并声明 State Schema。
5. 在“玩家界面”配置状态分组。
6. 在“结局验证”选择 verifier 并填写受限配置。
7. 在同一结局表单填写终局积分，0 表示不发放；阶段积分在“规则定义”维护，并核对每条终局的总预算。后台“奖励管理”可集中定位结局规则。
8. 保存草稿，再校验发布。

整份 JSON 位于“高级 JSON”，适合导入/导出，不是主要维护入口。

## 仓库内置包工作流

适合随代码维护、可重复导入的官方内容：

1. 在 `data/preset_packs/<slug>` 创建标准目录。
2. 编写包内规格或在 `docs/design` 更新对应正式专题。
3. 完成 pack/runtime 和资产 JSON。
4. 运行后端 importer 测试，确认内容哈希稳定。
5. 启动按 data/bootstrap.json 补齐八类九个默认包，已有包和停用状态保持。管理员内置更新与上传都先预览并导入草稿，再发布、选择入口和启用。

不需要手工创建奖励表或执行发奖 SQL。草稿导入和完整内容同事务；已发布同号不同内容一律不可覆盖，必须提升版本号。

## 发布前检查表

- 版本号为稳定三段式，Manifest 与 Pack 一致。
- 没有未引用的旧 JSON 资产。
- Runtime 使用已安装模块，动作和 handler 均允许。
- 每个状态路径唯一，默认值、类型、约束和权限匹配。
- Prompt 实际使用的枚举值与 State Schema 完全一致，例如“练气期”不能只声明成“练气”。
- State Schema 不超过 256 个字段，所有 enum values 都是非空字符串。
- Prompt 的 `agent_tool_loop` 只说明普通文本与当前步骤真实工具的边界，不要求 JSON 回复信封或模拟 Tool Call。
- internal/model 字段没有进入 presentation。
- 每个 outcome key 唯一，Prompt 明确引用。
- verifier 的状态路径和 roll key 正确。
- reward_points 为非负安全整数，入口余额条件只能引用两个固定 key。
- 修仙 Campaign 正积分结局关联真实终局任务，纯 Campaign 完成依赖本模块事实；阶段预算与所有配置终局一致。
- 成功、失败、中性结局都有清晰叙事行为。
- 玩家伪造过去事件、物品、判定或奖励不能绕过服务端。
- 可重复玩法不存在无业务必要的统一初始功法或物品；Campaign 的互斥开局传承使用 `starting_pool`，未选内容仍有可信取得路径。
- 桌面和移动端能完成选择、对话、状态、结算和奖励查看。
- `go test ./...`、`go vet ./...`、`vue-tsc` 和生产构建通过。

### Campaign 字段所有权（必须）

Campaign Runtime 必须提供非空 `campaign.field_ownership`。`state_schema` 只允许 conversation 字段，推荐统一使用 `narrative.*` 命名空间；禁止 `allow_children=true` 的无边界对象。每个长期字段声明 `path`、`owner: "campaign"`、权威 `storage_projection`，可写字段还要声明 `write_capability` 和 `event_types`。写入能力必须同时存在于模块 descriptor 和 `action_allowlist`，Action 的 `writes`/`events` 必须与声明一致。

Campaign View 的字段路径只能落在已注册的 Campaign 投影上。不要在 Prompt、Vue 组件或普通状态里复制角色名、位置、资源等长期事实；需要改动时新增领域 Action 和事件。导入器、发布校验和 Agent 工具执行使用同一 Ownership Registry，错误配置应在发布前指出冲突完整路径。

## 可参考实现

- 通用非修仙测试包：`backend/internal/challengeimport/testdata/midnight-case`。
- 官方修仙包：`backend/data/preset_packs/reincarnation-trials`。
- 契约与注册表：`backend/internal/gameplay/contract`，统一装配：`backend/internal/gameplay/bootstrap`。
- 题材模块示例：`backend/internal/gameplay/cultivation`。
- 导入器：`backend/internal/challengeimport`。
- 发布与结算：`backend/internal/challenge`。
- 状态验证与玩家投影：`backend/internal/state`、`backend/internal/gameplay/contract/contract.go`。
- 结构化管理员编辑器：`frontend/src/components/ChallengeVersionEditor.vue`。

## 玩法包交换与版本管理

完整 JSON 示例和结构 Schema 已提供。导出读取数据库固定版本，保留身份/排序/哈希，剔除本地数据库身份和存档。JSON/ZIP 统一编译；首期不接受脚本或跨包依赖。作者修改任何发布内容须提升版本。

完整协议见 [27 玩法包交换与版本生命周期](27-gameplay-package-exchange-and-versions.md)。
