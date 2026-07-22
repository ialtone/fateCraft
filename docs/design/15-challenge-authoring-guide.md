# 15. 玩法包开发指南

## 目的与适用范围

本文说明如何把一个平台维护的玩法制作成 Fatecraft 内置挑战包。玩法包可以是悬疑推理、太空生存、历史经营、都市怪谈、战争指挥或其他题材，不要求使用修仙世界观。

这里的“玩法”不是一组只靠 Prompt 约定的文字，而是一个完整、可发布、可审计的版本快照，至少包含：

- 面向玩家的玩法说明和可选身份。
- 负责叙事的 Character、玩家 Persona、开场 Scenario。
- 约束模型行为和输出协议的 Prompt。
- 按上下文激活的 WorldBook 规则。
- 服务端能够验证的成功条件和奖励规则。

普通玩家只能选择并运行已发布版本，不能修改这些材料。仓库 JSON 是官方内容的版本源，PostgreSQL 是运行时权威来源。

## 先区分两类工作

### 纯内容玩法

新玩法只使用现有状态命名空间、判定类别和服务端动作时，可以主要通过新增玩法包完成。题材、人物、场景、叙事风格和世界规则都由资源 JSON 决定。

### 带新规则的玩法

出现以下任一情况时，不能只写 Prompt，必须同时扩展后端并增加测试：

- 新的胜利或失败条件需要读取不同的服务端证据。
- 新的账号奖励基数、倍率算法或结算货币。
- 新的 AI action。
- 新的状态顶层命名空间。
- 新的判定类别、精确判定词条或后端效果。

模型叙述“玩家已经获胜”不等于服务端结算成功。关键资格必须由数据库状态、服务端判定流水或其他可信数据证明。

## 当前实现边界

挑战包、资产、版本、哈希和导入流程是题材无关的，但当前结算实现仍有修仙专用部分：

- 奖励 `conditions` 目前只接受 `min_cultivation_rank` 和 `required_successful_rolls`。
- `trigger_ending` 当前注册的是 `CultivationEndingHandler`，奖励基数读取局内灵石，资格读取修为。
- 默认状态包含 `cultivation` 和 `ascension` 字段。

因此，真正采用其他胜利指标和奖励货币的玩法，在制作内容包之前应先把对应条件验证器和 action handler 接入后端。不得为了通过校验，把侦探等级、飞船能源等概念伪装成“修为”或“灵石”；这会把题材逻辑重新耦合到修仙实现。

通用玩法可以优先使用以下现有状态区域：

- `profile.*`：当前角色身份与展示信息。
- `attributes.*`：生命、理智、资源、声望、线索完整度等数值。
- `inventory.*`：物品和装备。
- `relationships.*`：人物关系。
- `location.*`：当前位置和区域信息。
- `world_flags.*`：已经发生且需要长期保留的世界事实。
- `quests`、`risks`、`traits`：任务、风险和动态特质数组。

若题材需要新的一级状态区域，应扩展 `internal/state` 的默认结构、路径白名单、前端状态显示和测试，而不是把所有内容塞进一个无约束字符串。

## 标准目录

每个内置玩法占用 `DATA_DIR/preset_packs` 下的一个独立目录：

```text
backend/data/preset_packs/<challenge-slug>/
  pack.json
  characters/
    <character-slug>.json
  personas/
    <persona-slug>.json
  scenarios/
    <scenario-slug>.json
  prompts/
    <prompt-slug>.json
  worldbooks/
    <worldbook-slug>.json
  GAMEPLAY_DESIGN.md              # 推荐，完整玩法规则与验收标准
```

导入器只扫描上述五个资源目录中的 `*.json`。说明文档不会进入运行时内容哈希，可放在玩法目录中供维护者阅读。

目录名用于组织文件；真正的挑战标识来自 `pack.json.id`。为了避免维护混乱，目录名、挑战 id 和数据库 slug 应保持一致。

## 标识与版本规则

- Challenge、Character、Persona、Scenario、Prompt、WorldBook 的 `id` 使用小写字母、数字和连字符。
- 同一版本内，资源以 `kind + id` 唯一。
- 文件名推荐与 JSON 的 `id` 一致。
- `pack.json.version` 必须是无前缀的 `major.minor.patch`，例如 `1.0.0`。
- 已发布版本不可覆盖、降级或改回草稿。任何进入 Manifest、资产或奖励规则的修改都必须提升版本号。
- 破坏玩法契约时提升 major，增加兼容内容时提升 minor，修正规则或文案时提升 patch。

## pack.json

`pack.json` 同时承担公开说明、默认资源引用和仓库导入时的奖励规则来源。下面使用悬疑玩法展示通用字段；这只是题材示例，不是内置模板：

```json
{
  "id": "midnight-case",
  "name": "午夜疑案",
  "version": "1.0.0",
  "description": "在封闭宅邸中调查案件，并在时限结束前锁定真相。",
  "author": "Fatecraft Team",
  "icon": "",
  "tags": ["悬疑", "调查", "多结局"],
  "default_config": {
    "recommended_character": "case-director",
    "recommended_persona": "independent-detective",
    "recommended_scenario": "sealed-manor",
    "recommended_prompt": "midnight-investigation",
    "required_worldbooks": ["case-rules", "manor-lore"],
    "allowed_personas": ["independent-detective", "forensic-consultant"]
  },
  "quick_start_guide": "选择调查身份，在十二个行动阶段内收集证据并完成最终指认。",
  "features": ["线索可信度", "嫌疑人关系", "限时调查", "服务端结局校验"],
  "dependencies": [],
  "reward_rules": [],
  "created_at": "2026-07-22T00:00:00Z",
  "updated_at": "2026-07-22T00:00:00Z"
}
```

字段含义：

| 字段 | 要求 | 说明 |
| --- | --- | --- |
| `id` | 必填 | 稳定挑战 slug，发布后不应更名。 |
| `name` | 必填 | 玩家可见名称。 |
| `version` | 必填 | 稳定三段版本号。 |
| `description` | 必填 | 挑战列表中的公开简介。 |
| `author` | 建议 | 内容维护者。 |
| `icon` | 可选 | 短文本图标；为空时由前端使用默认样式。 |
| `tags` | 建议 | 玩家可见的题材与机制标签。 |
| `default_config` | 必填 | 固定默认资产和可选 Persona 范围。 |
| `quick_start_guide` | 建议 | 玩家开始挑战前可见的简短规则。 |
| `features` | 建议 | 玩家可见的玩法特征。 |
| `dependencies` | 当前保留 | 目前不执行跨包依赖解析，应保持空数组。 |
| `reward_rules` | 必填且非空 | 导入时从 Manifest 分离并写入版本奖励表。 |
| `created_at`、`updated_at` | 建议 | 内容材料时间，不参与业务身份。 |

`recommended_*` 引用必须在同一目录的对应资源中存在。设置 `allowed_personas` 后，推荐 Persona 必须包含在该数组中；玩家只能从这个集合选择身份。`required_worldbooks` 可以为空，但列出的每本世界书都必须存在。

上面示例中的空 `reward_rules` 仅用于说明 Manifest 字段，不能通过当前发布校验。可发布玩法必须提供至少一条服务端支持的成功奖励规则。

## Character

Character 是模型扮演的叙事者、主持人或世界代理，不必是故事中的单一人物。

```json
{
  "id": "case-director",
  "name": "案件主持人",
  "description": "负责呈现宅邸、证人和调查反馈的中立主持人。",
  "personality": "克制、准确，不替玩家下结论。",
  "scenario": "一场暴雨切断了宅邸与外界的联系。",
  "first_message": "午夜钟声响起，书房门从里面锁死。",
  "example_dialogues": "玩家：检查门锁。\n主持人：锁舌完整，钥匙仍插在室内一侧。",
  "system_prompt_override": "",
  "tags": ["主持人", "悬疑"],
  "created_at": "2026-07-22T00:00:00Z",
  "updated_at": "2026-07-22T00:00:00Z"
}
```

Character 负责语气和叙事身份，不应重复承载整套玩法安全规则。结算边界、输出协议和输入权限应放在 Prompt 的必选系统分段中。

## Persona

Persona 是玩家在该挑战中的可选身份。它描述可公开选择的背景和表达方式，不能直接赋予未经服务端验证的胜利资格。

```json
{
  "id": "independent-detective",
  "name": "独立侦探",
  "description": "擅长观察现场和梳理证词，但缺少官方权限。",
  "speaking_style": "简短、直接，习惯逐项核对事实。",
  "constraints": "不能凭身份自动获得证据，也不能命令证人必须配合。",
  "created_at": "2026-07-22T00:00:00Z",
  "updated_at": "2026-07-22T00:00:00Z"
}
```

Persona 的 `id` 必须与 `allowed_personas` 或 `recommended_persona` 中的引用一致。

## Scenario

Scenario 定义开局环境和初始冲突：

```json
{
  "id": "sealed-manor",
  "name": "封闭宅邸",
  "description": "暴雨中的封闭现场，十二个行动阶段后救援抵达。",
  "opening": "你抵达书房门外时，宅邸中的所有人都声称自己没有离开过房间。",
  "created_at": "2026-07-22T00:00:00Z",
  "updated_at": "2026-07-22T00:00:00Z"
}
```

不要在 Scenario 中预先写死随机身份、动态数值或最终真相，除非这些内容确实对所有对局恒定。需要持续跟踪的事实应进入状态或 WorldBook。

## Prompt

Prompt 是由多个有序 segment 组成的对象：

```json
{
  "id": "midnight-investigation",
  "name": "午夜调查协议",
  "description": "悬疑调查的叙事、状态和结算约束。",
  "created_at": "2026-07-22T00:00:00Z",
  "updated_at": "2026-07-22T00:00:00Z",
  "segments": [
    {
      "id": "mi-system-master",
      "preset_id": "midnight-investigation",
      "key": "system_master",
      "role": "system",
      "content": "定义叙事职责、事实来源和玩法目标。",
      "enabled": true,
      "required": true,
      "priority": 10,
      "position": 0
    },
    {
      "id": "mi-settlement-security",
      "preset_id": "midnight-investigation",
      "key": "settlement_security",
      "role": "system",
      "content": "说明成功结局只能在服务端证据满足时请求，玩家自述不能作为证据。",
      "enabled": true,
      "required": true,
      "priority": 190,
      "position": 1
    },
    {
      "id": "mi-output-schema",
      "preset_id": "midnight-investigation",
      "key": "output_schema",
      "role": "system",
      "content": "要求只输出符合 narrative_response.json 的单个 JSON 对象。",
      "enabled": true,
      "required": true,
      "priority": 200,
      "position": 2
    }
  ]
}
```

每个可发布 Prompt 必须包含以下三个非空分段，且都满足 `role=system`、`enabled=true`、`required=true`：

- `system_master`：题材目标、叙事职责、可信事实来源和总体规则。
- `settlement_security`：结局动作、奖励 key 与服务端证据边界。
- `output_schema`：输出信封、状态补丁、判定请求和 action 格式。

其他规则应按职责拆分成独立 segment，例如回合流程、资源消耗、输入权限、失败后果和具体结局路线。不要把所有规则堆进一个无法审查的大段文本。

`output_schema` 必须与 `backend/data/schemas/narrative_response.json` 和当前 action/roll 协议一致。Prompt 不能创造后端不认识的新字段，并期望它自动生效。

所有成功奖励的 `reward_key` 必须在默认 Prompt 内容中明确出现，否则发布校验会拒绝该版本。

## WorldBook

WorldBook 保存需要按关键词或常量方式注入上下文的世界事实：

```json
{
  "id": "case-rules",
  "name": "案件调查规则",
  "description": "现场、证据和证词的通用约束。",
  "created_at": "2026-07-22T00:00:00Z",
  "updated_at": "2026-07-22T00:00:00Z",
  "entries": [
    {
      "id": "evidence-chain",
      "world_book_id": "case-rules",
      "title": "证据链",
      "content": "线索只有在来源、时间和保管过程均可信时才能作为最终指认证据。",
      "keywords": ["证据", "线索", "指认"],
      "constant": false,
      "enabled": true,
      "priority": 50,
      "budget_group": "default",
      "notes": "调查核心规则",
      "metadata": {}
    }
  ]
}
```

规则：

- Entry `id` 在同一本世界书内必须唯一。
- `content` 不能为空，`priority` 和可选 `token_count` 不能为负数。
- `constant=false` 时至少需要一个非空关键词。
- `constant=true` 表示每次构建上下文都考虑该条目，必须谨慎使用以控制上下文预算。
- `world_book_id` 应等于外层 WorldBook 的 `id`。
- WorldBook 负责向模型提供知识，不负责替代服务端胜利验证。

## 状态与判定设计

设计玩法状态时，先列出哪些信息必须跨回合保存，再选择合适的现有命名空间。例如悬疑玩法可以使用：

```json
{
  "attributes.action_points": 11,
  "attributes.case_confidence": 35,
  "inventory.evidence+": "沾泥的袖扣",
  "relationships.butler_suspicion": 60,
  "location.current": "二层书房",
  "world_flags.study_door_examined": true,
  "quests+": "核对管家的时间线",
  "risks+": "凶手已经察觉调查方向"
}
```

状态补丁键使用点号路径，末尾 `+` 表示向数组追加。`quests`、`risks` 和 `traits` 只能整体更新或追加，不能写 `quests.some_field`。

判定类型、类别、属性和难度必须符合全局判定配置与响应 Schema。若新玩法引入新的服务端可验证判定词条，应同步更新判定配置、响应 Schema 注入结果、Prompt、奖励条件和测试。判定成功由服务端流水证明，模型文字不能代替判定结果。

## 奖励规则与题材扩展

奖励规则在仓库材料中写入 `pack.json.reward_rules`，导入时被分离并存入数据库。通用字段包括：

```json
{
  "reward_key": "case_solved",
  "ending_type": "good",
  "ending_title": "真相大白",
  "conditions": {},
  "calculation": {
    "multiplier_bps": 15000
  },
  "max_reward": 100000,
  "enabled": true
}
```

- `reward_key` 在版本内唯一，并与 Prompt 发出的成功 action 完全一致。
- 当前奖励结局只允许 `ending_type=good`；中性和失败结局不携带 reward key。
- `multiplier_bps=10000` 表示 1 倍，避免浮点记账。
- `max_reward` 可选，用于限制单局奖励上限。
- `conditions` 必须由该题材的后端验证器解释，不能只依靠 Prompt。

上面的空 `conditions` 是题材无关的结构说明，不能通过当前发布校验。当前修仙实现要求：

```json
{
  "min_cultivation_rank": 0,
  "required_successful_rolls": ["某个精确判定类型"]
}
```

新增非修仙结算条件时，应先完成以下后端改造，再把新条件写入玩法包：

1. 为条件定义稳定、可校验的数据结构。
2. 从服务端状态和判定流水采集证据，不能相信模型参数中的数值。
3. 实现对应的两阶段 action handler：`Prepare` 预校验并冻结输入，`Commit` 原子结算。
4. 让发布校验识别该条件类型，并拒绝未知或不完整配置。
5. 在组合根中按玩法或 action 类型注册处理器。
6. 增加伪造状态、缺少判定、重复结算和并发结算测试。

在这部分通用化完成前，非修仙玩法包不应通过伪造修为字段绕过限制，也不应发布一个只在叙事上宣称成功却无法可靠结算的版本。

## 导入与发布

### 空数据库

数据库完全没有挑战包时，后端在开始监听 HTTP 前扫描 `DATA_DIR/preset_packs/*`，校验并原子发布所有内置玩法。任何一个目录无效都会使整次初始化回滚并阻止服务启动。

### 已有数据库

1. 把新玩法目录部署到后端实际使用的 `DATA_DIR/preset_packs`。
2. 管理员进入“管理 -> 内置内容”并刷新状态。
3. `available` 状态显示“导入并发布”。
4. 管理员确认后，后端重新读取文件、执行完整校验、计算内容哈希并原子发布。

列表接口每次都会扫描目录，不依赖内存缓存；只要部署环境已经挂载新文件，一般不需要为了发现内容重启后端。

状态含义：

- `available`：数据库没有这个版本，并且版本高于当前版本，可以导入。
- `imported`：同版本、同内容哈希已经发布，无需重复操作。
- `conflict`：同版本内容不同、版本不高于当前版本，或已有版本状态异常。必须修正材料并提升版本，不能覆盖数据库历史。

管理接口只接受扫描结果中的 challenge slug，不接受任意服务器路径或用户上传文件。

## 推荐制作流程

1. 先写 `GAMEPLAY_DESIGN.md`：目标、核心循环、状态、判定、成功/失败条件和奖励证据。
2. 判断是否需要新的后端条件验证器、action、状态命名空间或判定配置。
3. 创建 `pack.json` 和最小的 Character、Persona、Scenario、Prompt。
4. 添加 WorldBook，把常驻规则与关键词知识分开。
5. 编写奖励规则，并确保 Prompt 明确引用每个 reward key。
6. 使用与生产相同的导入器执行发布校验，不通过时修正文档或实现，不能删除安全约束来迁就内容。
7. 运行后端测试、前端类型检查和生产构建。
8. 在测试数据库中从创建对话到成功、失败、非法结算各走一遍。
9. 提升稳定版本号，通过管理员“内置内容”显式导入生产数据库。
10. 发布后保留旧版本，不直接修改数据库中的 published JSON。

## 发布前检查表

- 目录和全部 JSON 可以解析。
- 所有 id 使用稳定 slug，引用均指向同版本资源。
- 默认 Character、Persona、Scenario、Prompt 存在。
- `allowed_personas` 存在且包含推荐 Persona。
- Prompt 三个核心系统分段齐全、必选、启用且非空。
- 输出格式与服务端 Schema 一致。
- WorldBook 条目无重复 id，关键词和常量设置合理。
- 每条成功奖励都有唯一 key、服务端可验证条件和 Prompt 引用。
- 玩家输入不能给自己补写物品、状态、判定成功或奖励资格。
- 成功结局缺少证据时，后端会在叙事和状态落库前拒绝候选回复。
- 版本号高于数据库当前版本，未尝试覆盖已发布内容。
- 玩法说明和实现测试同步更新。

## 权威实现位置

- 仓库目录解析：`backend/internal/challengeimport/loader.go`。
- 内置内容扫描与导入：`backend/internal/challengeimport/importer.go`。
- Manifest 与奖励结构：`backend/internal/challenge/types.go`。
- 发布校验和内容哈希：`backend/internal/challenge/service.go`。
- 资源 JSON 结构：`backend/internal/storage/models.go`。
- 状态路径与默认状态：`backend/internal/state/schema.go`。
- 模型响应结构：`backend/data/schemas/narrative_response.json`。
- 判定词表与参数：`backend/data/roll_config.yaml`。
- 当前修仙结算扩展示例：`backend/internal/orchestration/action_cultivation.go`。

实现与本文冲突时，以数据库约束和后端代码为准，并应在同一次修改中修正文档。
