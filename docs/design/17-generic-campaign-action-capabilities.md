# 17. 通用 Campaign 动作能力架构

> 本文是后续 Agent 开发、审查和扩展长期玩法的通用入口。它只描述当前已经实现的协议与边界，不定义修仙、推理、经营或其他题材规则。具体题材动作见对应模块文档。

## 1. 目标和边界

Fatecraft 的 Campaign 核心只负责存档生命周期、Runtime 固定、动作路由、事务、revision、幂等、事件、安全错误和有界上下文。核心不理解法力、线索、库存、军队或任何题材概念。

长期玩法分成三层：

| 层 | 职责 | 不允许 |
| --- | --- | --- |
| 平台核心 | Campaign、协议、锁、事务、幂等、预算、错误安全 | 出现题材资源名、动作 key 或计算公式 |
| 可信玩法模块 | 动作定义、动态能力、事务执行、领域投影、题材规则 | 绕过平台事务或信任模型自报结果 |
| 可导入玩法包 | 固定模块、动作白名单、内容定义、Prompt、WorldBook、展示 | 携带代码、SQL、脚本或引用未安装动作 |

纯内容变化只需更新玩法包。全新机制必须由已安装的可信模块实现；当前没有开放任意规则脚本或通用 DSL。

## 2. Runtime 固定

Campaign Runtime 必须声明模块允许的 handler、上下文策略和领域动作白名单：

~~~json
{
  "engine": {"key": "some.campaign", "version": 1},
  "mode": "campaign",
  "campaign": {
    "lifecycle_handler": "some.campaign_lifecycle",
    "projection_handler": "some.campaign_projection",
    "context_policy": "some.campaign_context",
    "action_allowlist": ["some.move", "some.inspect"],
    "field_ownership": [
      {"path": "campaign", "owner": "campaign", "value_type": "object", "visibility": "player", "storage_projection": "campaigns"}
    ],
    "initial_location_key": "world.start",
    "world_clock": {
      "calendar_key": "world.default",
      "max_due_events_per_turn": 16
    }
  }
}
~~~

发布校验同时确认：

- engine 和版本已安装；
- lifecycle、projection、context policy 属于该模块；
- `action_allowlist` 包含 1 到 64 个无重复稳定 key；
- 每个动作都出现在模块 descriptor 的 `campaign_actions` 中；
- 玩法包不能扩大动作的 actor 权限。

完整 Campaign 配置进入版本内容哈希。Campaign 创建后固定 `challenge_version_id + challenge_hash`，不会跟随内置包后来覆盖或发布的新版本。

## 3. 动作契约

每个已安装动作只能通过一个 `RegisteredAction` 注册：

~~~text
RegisteredAction
  Definition  静态 key、说明、actor、封闭参数 Schema
  Capability 当前 revision 的只读动态能力 Provider
  Handler     事务内最终校验、计算和写入
~~~

`ActionDefinition.argument_schema` 必须是 `additionalProperties=false` 的对象 Schema。当前基础参数类型为 string、integer 和 boolean；未知字段、缺少 required、错误类型、enum 外取值和静态越界会在进入事务前拒绝。

`ActionCapability` 是当前 Campaign revision 的派生读模型：

~~~json
{
  "key": "some.action",
  "description": "动作含义",
  "available": true,
  "revision": 12,
  "argument_schema": {
    "type": "object",
    "required": ["target_key", "amount"],
    "properties": {
      "target_key": {"type": "string", "enum": ["target-a"]},
      "amount": {"type": "integer", "minimum": 1, "maximum": 11}
    },
    "additionalProperties": false
  },
  "options": [
    {
      "target_key": "target-a",
      "limits": {"amount": {"minimum": 1, "maximum": 11}},
      "cost_preview": {"resource_key": "resource-x", "per_unit": 1}
    }
  ]
}
~~~

通用字段语义：

- `available` 表示当前至少存在一种合法输入；
- `revision` 必须等于投影所基于的 Campaign revision；
- `argument_schema` 只能在静态 Schema 基础上收紧：不能新增参数、改变类型、取消必填、扩大 enum 或放宽长度和数值边界；
- `options` 表达有关联的候选组合，参数不能跨不同 option 拼接；调用方提交的某个参数只要出现在任一 option 中，匹配的 option 就必须显式包含相同值，条件参数不能附着到不接收它的候选；
- `limits` 表达该 option 下的动态数量范围；
- `violations` 只包含明确标记为公开的不可用原因；
- `metadata` 只能放有界、可进入模型上下文的公开辅助数据。

能力不是权威状态，不持久化为 Campaign 事实，也不能代替事务内复核。

## 4. 生成前链路

1. Campaign Service 读取归属、固定 Runtime、Campaign 头、最近可见事件和领域快照。
2. 根据 Runtime 的 `projection_handler` 调用玩法模块 Projection Provider；平台核心不构建题材字段。
3. 按 `action_allowlist` 读取已注册动作，并过滤 actor 不包含 `model` 的动作。
4. 对每个动作调用 Capability Provider，统一写入当前 revision、静态说明和默认 Schema。
5. Provider 失败会停止本轮生成，不能把“无法判断”当成可执行。
6. 结果先作为内部 `campaign_projection.action_capabilities` 交给主叙事协调器；当前按目录顺序把最多 16 个 `available=true` 的能力转换为独立 `fc_action_*` Tool Definition。
7. 工具定义与 Handler、Campaign revision 一起固定到当前 `agent_steps` 快照；随后从发送给模型的 Projection 正文移除 `action_capabilities`，避免 Schema 重复占用上下文或让模型自行拼装调用协议。
8. 其余 Projection 超过当前模型预算时，Context Builder 按玩法 Provider 声明的主题优先级压缩；工具定义属于必选预算，放不下时构建失败而不是静默移除可用动作。

当前硬限制：Runtime 动作最多 64 个、单步骤暴露的 Campaign 工具最多 16 个、每个动作 options 最多 64 个、能力目录合计最多 96 KiB、单 Provider 上下文超时 200 ms。options 被裁剪时会标记 `truncated=true`；事务执行不依赖被裁剪的候选列表。能力、Projection 顶层主题和事件列表都必须使用稳定 key/id 作为并列项 tie-breaker，不能依赖数据库默认返回顺序，以便上下文哈希、分页和重连恢复可复现。

## 5. 提交与原子事务

玩家 HTTP 动作仍显式提交：

~~~json
{
  "action_id": "稳定幂等键",
  "key": "some.action",
  "expected_revision": 12,
  "arguments": {}
}
~~~

主叙事模型不提交 `action_id`、动作 key 或 `expected_revision` 外壳，只调用快照中的具体工具：

~~~json
{
  "name": "fc_action_some_action",
  "call_id": "provider-call-id",
  "arguments": {}
}
~~~

服务端从工具绑定还原稳定动作 key，从步骤快照读取 expected revision，并以 `attempt_id + call_id` 生成 Campaign 幂等键。稳定 key 与玩家文案分离：Projection 保持规则所需的原值，玩家 Snapshot 通过当前 Runtime Presentation 的 `value_labels` 解析；通用前端不得维护某个玩法的值词典。执行顺序：

1. 核心检查 call ID、工具名、步骤快照、已注册动作、actor、静态参数 Schema 和 Runtime `action_allowlist`。
2. PostgreSQL 锁定 Campaign 行；先查 Campaign 幂等结果，再检查 active 和快照固定 revision。
3. 在同一事务内重建领域快照，并再次调用与生成前相同的 Capability Provider。
4. 核心确认动态 Schema 只收紧静态定义，再验证动态 Schema、option 组合和 option limits；不修改、不缩小、不替换模型参数。
5. Capability 允许后，玩法 Handler 执行最终题材校验、确定性计算和领域写入。
6. 领域事实、事件、时间、位置、Campaign 状态、revision、Campaign 幂等记录、Agent invocation、assistant tool-call 消息、tool-result 消息和 attempt 写工具计数原子提交。
7. 数据库提交后才广播 Campaign、状态、判定或结算事件；下一模型步骤重新读取最新 Projection 和 revision，并构建新的工具快照。

`GameplayDefinition` 属于固定 ChallengeVersion，可以通过 `challenge.Service` 的版本编译缓存按 `kind:key` 读取。Campaign Repository 在使用缓存前传入并校验存档固定的 `challenge_hash`，为本次 DomainState 或 ActionStore 复制外层索引及嵌套 JSON，调用方不能修改共享缓存。角色、功法实例、库存、市场、NPC、任务、事件、revision 和其他 Campaign 事实仍必须从当前事务连接读取；定义缓存不能替代 Campaign 行锁或事务内能力复核。

Handler 返回 rejected/failed 时，内部哨兵回滚该工具事务内的所有领域写入，再单独保存 invocation 与有界公开 tool result。此前已经 committed 的其他工具不回滚；如果后续模型步骤失败，attempt 为 `partial`。不存在内部模型重试或把失败调用当成成功事实。

## 6. Violation 与用户提示

动态拒绝使用结构化 `ActionViolation`：

~~~json
{
  "code": "resource.insufficient",
  "path": "arguments.amount",
  "message": "资源不足，本次最多可执行 11 次",
  "public": true
}
~~~

- `code` 是稳定机器 key；
- 只有 `public=true` 且 message 非空时，工具结果才向模型提供具体原因；
- details 不进入用户提示；
- 普通错误、数据库错误和未结构化 rejection 只显示通用失败文案；
- revision 冲突使用独立安全提示，并要求模型依据最新投影重新选择；
- rejected/failed 调用不留下任何领域事实，但其公开结果会进入工具历史，供下一步骤自然回应。

当前产品决定是不做内部模型重试。用户可以显式重新生成或继续发送消息。

## 7. Projection Provider

`ProjectionProvider.Snapshot` 生成玩家详情页读模型，`ProjectionProvider.Context` 返回 `ProjectionView{Data, Priority}` 作为有界模型读模型。两者由 Runtime 的稳定 key 路由并由玩法模块注册。玩家详情页的稳定值由 Runtime Presentation 的 `value_labels` 解析；Campaign 栏目、字段和动作槽由 `presentation.campaign_view` 描述并由前端通用 Renderer 渲染，玩法模块只需提供公开投影，不再要求前端组件实现题材栏目。

### Campaign View 展示契约

`campaign_view` 是 Runtime 的只读 UI 元数据，包含 `renderer`、header、sections、fields、有限条件、empty states 和 action slots。平台校验 renderer/widget 白名单、公开路径、数量上限、条件表达式和动作 allowlist；参数绑定只能读取公开快照或 item/option/input 上下文。它不包含权限、消耗、随机、奖励或数据库写入规则。前端未知 renderer、字段和技术值安全隐藏，后端仍在每次动作事务中重校验权限、参数和 revision。

同一套 `GuidanceRenderer` 也承载普通对话快捷指引。`presentation.guidance` 的 `message` 项只会提交普通玩家消息，`action` 项复用上述能力/事务入口；指引投影是派生 UI 数据，不是事实或授权。Campaign guidance 的条件和绑定必须通过 `OwnershipRegistry` 的公开字段校验。Campaign 的旧 `campaign_view.actions` 在前端适配为 guidance item，保持旧玩法包兼容。

`Priority` 是 Data 顶层 key 的高到低顺序，必须由玩法模块根据当前决策需要声明；平台不能出现题材 key。未列出的主题仍是低优先级候选，不会丢失兼容性。

平台保留 `id`、`status`、`current_location_key`、`game_time`、`revision`、`recent_events`、内部 `action_capabilities`、玩家 Snapshot 的 `outcome_rewards` 和压缩时注入的 `projection_control`，玩法投影不能覆盖。`outcome_rewards` 由平台根据不可变账本恢复，不进入玩法 Provider 自定义数据。玩家 snapshot 不能泄漏 internal 或仅供模型使用的事实；模型上下文也必须先按地点、查询和集合上限裁剪，再接受统一 Token 压缩。内部 `action_capabilities` 在生成工具快照后会从模型可见 Projection 移除。

终局带出不是普通模型动作。可信终局 Handler 在 Campaign 行锁事务内调用平台奖励结算，来源扣除、账号余额、不可变账本、完成状态和事件原子提交；计算器和发布规则见 `21-campaign-outcome-rewards.md`。

低窗口压缩只改变本次模型读模型，不改变 Campaign 事实、revision、API snapshot、工具快照或事务内能力复核。`projection_control.partial=true` 时，未列主题表示本轮未提供，不表示集合为空。模型只能调用当前步骤实际提供的 `fc_action_*` 工具；上下文日志记录压缩前后 Token 和省略 key。

## 8. 新玩法模块接入

1. 在独立叶子契约包声明稳定动作 key，避免模块 descriptor 依赖执行包。
2. 在 `gameplay.ModuleDescriptor` 声明 Campaign handlers、policies、`CampaignActions` 以及每个可写 Action 的 `CampaignActionWrites/Events` 元数据。
3. 为每个动作提供封闭静态 Schema、只会收紧该 Schema 的 Capability Provider 和事务 Handler。
4. 把领域投影实现放在玩法模块目录，通过组合根注册 Projection Provider，并为 Context Data 声明与题材相符的顶层 Priority。
5. 数值成本和上限应抽成纯规则函数；能力投影与 Handler 共同调用。无论如何，事务内都会重新调用同一个 Capability Provider。
6. Runtime 只白名单启用当前玩法需要的动作；导入校验拒绝跨模块 key。
7. 增加能力边界、事务回滚、revision、幂等、公开错误和真实 PostgreSQL 测试。

只有完全不依赖资源、位置、对象、权限、时间或其他当前事实的动作才能使用 `StaticCapabilityProvider`。

## 9. 关键代码入口

| 关注点 | 入口 |
| --- | --- |
| 通用能力协议和安全校验 | `backend/internal/campaign/capability.go` |
| 注册、上下文、动作路由 | `backend/internal/campaign/service.go` |
| Projection Token 压缩与审计 | `backend/internal/contextbuilder/campaign_projection.go` |
| Runtime Campaign 契约 | `backend/internal/gameplay/contract.go` |
| 模块白名单校验 | `backend/internal/gameplay/registry.go` |
| 事务、revision、幂等和事件 | `backend/internal/storage/campaign.go` |
| 事务内领域快照 | `backend/internal/storage/campaign_domain.go` |
| 修仙动作定义与能力 | `backend/internal/gameplay/cultivation/campaignactions` |
| 修仙投影 Provider | `backend/internal/gameplay/cultivation/campaignview` |
| 模型工具构建与路由 | `backend/internal/orchestration/agent_tools.go` |
| 模型工具原子提交 | `backend/internal/orchestration/agent_transaction.go`、`storage/repo_agent.go` |

后续 Agent 修改 Campaign 动作时，必须同时检查 Definition、Capability、Handler、Runtime allowlist、模块 descriptor、Prompt 和测试。不能只修改 Prompt 或静态描述来宣称服务端获得了新能力。

## 10. 字段所有权与权威投影

Runtime 的 `state_schema` 只声明当前会话的 conversation 状态；Campaign 的长期事实必须在 `campaign.field_ownership` 声明。后端启动时将两者编译为不可变 `OwnershipRegistry`，对路径做唯一归属、父子跨域冲突、写入能力和事件校验，并生成稳定 SHA-256 hash。Campaign 状态字段必须位于 `narrative.*` 命名空间，不能使用无边界 `allow_children`。

每个可写 Campaign 字段都必须绑定当前模块已注册且已加入 `action_allowlist` 的 Action。模块 descriptor 同时声明该 Action 的 `writes`/`events`，例如 `character.rename` 写入 `character.name` 并产生 `character.renamed`。Action Definition、Capability 和 Handler 的元数据不能由 Prompt 或前端替代；Handler 仍在事务内做最终业务校验。

`campaign_view` 的 header、字段和静态条件只能引用 Ownership Registry 中的公开 Campaign 投影。前端只读取服务端 Campaign Snapshot，不得把 conversation state 作为长期字段 fallback。角色姓名、位置、库存、关系等修改必须更新权威领域表并追加事件，提交成功后才广播 `campaign_updated`。
