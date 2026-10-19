# 28. 状态与剧情一致性：动作结果、叙事绑定与事实校验

> 状态：已实现（核心契约、哈希、绑定、确定性校验、兜底与前端同帧刷新）。模型语义复核和归档指标属于后续增强。

实现落点：

- 契约：`backend/internal/gameplay/contract/action_result.go`
- 状态哈希：`backend/internal/storage/campaign_state_hash.go`
- 动作提交与 ActionResult：`backend/internal/storage/campaign.go`
- 未消费动作与绑定：`backend/internal/storage/campaign_narrative.go`、`storage/repo_message.go`
- 上下文注入：`backend/internal/campaign/service.go`、`backend/internal/orchestration/assemble.go`
- 校验与兜底：`backend/internal/orchestration/narrative_validate.go`
- 前端同帧刷新：`frontend/src/stores/campaign.ts`、`frontend/src/stores/socket.ts`

## 1. 要解决的问题

玩家看到的左侧状态栏来自结构化投影，正文来自模型生成。两者一旦分叉，就会出现：

- 正文写“已突破筑基”，左侧仍是“炼气九层”。
- 正文写“气血尽复、伤势痊愈”，左侧气血未满或伤势仍在。
- 正文写“获得三块灵石”，库存和账本没有任何变化。
- 正文写“老邢对你更信任了”，关系数值没有变化，或变化幅度与描述不符。
- 玩家刚完成一次动作，旧请求的慢响应随后落库，正文描述的是上一版状态。

这些不是单纯的文案问题。根因通常有四类：

1. **模型拿到的不是动作后的完整结果。** 只有增量事件或历史摘要，模型只能自行补全“突破后境界”“回血后伤势”等未提供的事实。
2. **叙事没有绑定状态版本。** 生成期间状态可能继续推进，旧叙事仍会写入 `display_messages`。
3. **保存前没有事实校验。** 正文只要非空就会被接受，错误主张直接进入时间线。
4. **前端读取了两个快照。** 正文和左侧投影分别刷新，顺序错乱时会出现短暂或持久的不一致。

目标不是让模型“更小心”，而是让错误叙事**无法进入正式时间线**，并让所有展示都绑定同一个权威快照。

## 2. 当前基线

现有实现已经具备几项关键基础，不需要推倒重来：

- `campaigns.revision` 是每次成功领域动作递增的乐观并发版本，动作提交使用 Campaign 行锁。
- `campaign_action_commits` 以 `(campaign_id, idempotency_key)` 保证动作只结算一次，`result JSONB` 已保存动作结果。
- `campaign_events` 只追加，记录动作产生的领域事件。
- `display_messages` 已有 `turn_id`、`attempt_id`、失效时间和失效原因。
- Runtime 投影与 `field_ownership` 已明确“每个字段只有一个所有权域”。
- `conversation_generation_runs` 已提供生成租约，可承载 stale 判定。

本轮已补齐：动作结果成为持久 `ActionResult`，叙事记录绑定 revision 与 `state_hash`，落库前执行确定性事实校验，前端按 revision/hash 缓冲并对齐消息与侧栏。模型语义复核、失败指标和归档诊断仍属于后续增强。

## 3. 核心不变量

1. 后端 Campaign 状态与结构化动作事件是唯一事实来源；LLM 只生成叙事，不决定状态。
2. 动作只结算一次。叙事生成、校验、重试、归档失败都不得重放动作或回滚已提交状态。
3. Campaign 进入 `completed`、`dead` 或 `abandoned` 时必须同时提交已启用的 `outcome_key`；气血死亡为 `perished`，寿元耗尽为 `lifespan_exhausted`。失败结局同样生成 `CompletionView`、关闭对话并进入“本世经历”。
4. 每条正式叙事必须绑定生成时的 `campaign_revision`、`state_hash` 与 `action_id`。
5. 叙事写入前必须通过分层事实校验；校验失败只能重生成或降级为模板说明。
6. 左侧状态栏、动作按钮和正文必须来自同一快照；旧快照不得覆盖新快照。
7. 自动归档与长期记忆是只读消费者，只能报告冲突，不能写入或纠正 Campaign 状态。
8. 隐藏事实（私密 NPC 目标、未发现地点、内部判定）不得进入玩家可见叙事或投影。

## 4. 目标回合流程

```text
玩家输入 / UI 动作
        |
        v
[事务 A] 校验参数 -> 行锁 Campaign -> 写入领域状态与事件
         -> 计算 state_hash -> 保存 ActionResult -> revision + 1
        |
        v
构造不可变 NarrativeContext
  { campaign_id, revision, state_hash, action_result, player_projection }
        |
        v
模型生成叙事（禁止自行推算状态）
        |
        v
L0 结构校验 -> L1 硬事实校验 -> L2 语义主张校验
        |                         |
        | 通过                    | 不通过
        v                         v
[事务 B] 校验 revision/hash  针对性重生成一次
 一致后写入 display_messages        |
        |                           | 仍不通过
        v                           v
广播 {message, snapshot}      写入模板兜底叙事
                                    |
                                    v
                              记录 validation 告警
```

关键边界：事务 A 提交后，动作结果已经生效。事务 B 只负责“这段叙事是否允许展示”，不负责修改状态。

## 5. ActionResult 契约

`campaign_action_commits.result` 从“给 UI 看的任意对象”升级为带版本的稳定契约：

```json
{
  "schema_version": 1,
  "action_id": "player-action-0f6c...",
  "action_key": "cultivation.breakthrough",
  "actor_type": "player",
  "campaign_id": "…",
  "revision": 42,
  "state_hash": "…64 hex…",
  "game_time": 1380,
  "goal_status": "completed",
  "summary": "周砚由炼气九层突破至筑基初期，消耗聚气丹一枚。",
  "facts": [
    {
      "path": "character.realm",
      "label": "境界",
      "kind": "enum",
      "before": "qi_refining",
      "after": "foundation",
      "visibility": "player"
    },
    {
      "path": "character.stage",
      "label": "阶段",
      "kind": "enum",
      "before": "9",
      "after": "1",
      "visibility": "player"
    },
    {
      "path": "inventory.pill.qi_gathering",
      "label": "聚气丹",
      "kind": "integer",
      "before": 2,
      "after": 1,
      "delta": -1,
      "unit": "枚",
      "visibility": "player"
    }
  ],
  "added": [],
  "removed": [],
  "preserved": [
    {
      "path": "character.wounds",
      "label": "伤势",
      "reason": "本次突破未治疗伤势"
    }
  ],
  "constraints": {
    "allowed_claims": ["突破至筑基初期", "消耗一枚聚气丹"],
    "forbidden_claims": ["伤势痊愈", "气血回满", "寿元增加"]
  }
}
```

### 5.1 FactChange

每个 `FactChange` 必须满足：

- `path` 是投影中的稳定路径，不使用展示名称作为键。
- `before`、`after` 来自同一事务内的真实值；`delta` 只能由后端计算。
- `kind` 至少覆盖 `enum`、`integer`、`boolean`、`string`、`set`。
- `visibility` 为 `player`、`model`、`internal`。只有 `player` 事实可以进入玩家正文和左侧投影。
- 没有变化的资源不写成“变化”，但可以放进 `preserved` 明确“未改变”，例如 `气血回满不等于伤势清除`。

### 5.2 goal_status

统一描述任务、承诺、遭遇和终局推进：

- `unchanged`：本次动作不改变目标状态。
- `progressed`：进度增加，但目标未完成。
- `completed`：目标完成。
- `failed`：目标失败或过期。
- `blocked`：条件不足，动作未产生目标推进。

`goal_status` 必须由领域动作给出，模型不得根据正文自行推断。

### 5.3 state_hash

`state_hash` 是对“会影响玩家投影与叙事事实的字段”做规范化序列化后的 SHA-256：

- 键按字典序排序，数组保持领域语义顺序。
- 排除 `updated_at`、`created_at` 等非语义时间戳。
- 包含 `challenge_version_id` 与 `challenge_hash`，避免版本混用。
- 包含角色、境界、资源、伤势、位置、时间、任务、关系、势力、遭遇、终局等投影相关表。
- 不包含纯内部随机数、缓存或临时 UI 状态。

哈希必须在**同一事务内、所有领域写入完成之后**计算并写入 `campaigns.state_hash`。

## 6. 数据结构改造

项目当前只有 `00001_init.sql`，且没有历史存档兼容要求，因此直接修改基线，不新增兼容迁移：

### 6.1 `campaigns`

新增：

```sql
state_hash CHAR(64) NOT NULL CHECK (state_hash ~ '^[0-9a-f]{64}$'),
```

创建 Campaign 时由 `CampaignRepository.Create` 在初始化完成后写入首个真实哈希。数据库默认全零哈希只保护测试夹具和直接插入路径，不能作为正式存档的最终值。

### 6.2 `campaign_action_commits`

新增便于查询和审计的显式列，完整结果仍保存在 `result`：

```sql
state_hash CHAR(64) NOT NULL CHECK (state_hash ~ '^[0-9a-f]{64}$'),
schema_version SMALLINT NOT NULL DEFAULT 1 CHECK (schema_version > 0),
narrative JSONB NOT NULL DEFAULT '{}'::jsonb
    CHECK (jsonb_typeof(narrative) = 'object' AND octet_length(narrative::text) <= 65536),
```

动作提交按 `(campaign_id, idempotency_key)` 主键读取，历史恢复按 `(campaign_id, revision)` 索引读取；`state_hash` 随记录加载后用于校验。当前没有按哈希检索的查询，因此不为 `(campaign_id, state_hash)` 单独建索引。

### 6.3 `display_messages`

新增叙事绑定与校验状态：

```sql
campaign_revision BIGINT CHECK (campaign_revision >= 0),
campaign_state_hash CHAR(64) CHECK (campaign_state_hash ~ '^[0-9a-f]{64}$'),
validation_status VARCHAR(16) NOT NULL DEFAULT 'not_applicable'
    CHECK (validation_status IN (
        'not_applicable', 'passed', 'repaired', 'fallback', 'stale', 'failed'
    )),
validation_detail JSONB NOT NULL DEFAULT '{}'::jsonb
    CHECK (jsonb_typeof(validation_detail) = 'object'),
```

约束与索引：

- 非 Campaign 对话这些列保持 `NULL` / `not_applicable`。
- Campaign 叙事消息要求 `campaign_revision`、`campaign_state_hash` 非空。
- `stale` 与 `failed` 消息不得作为正式助手叙事进入上下文；建议直接不写入，或用 `invalidated_at` 标记。
- 增加 `CREATE INDEX idx_display_messages_campaign_binding ON display_messages(conversation_id, campaign_revision, campaign_state_hash);`

### 6.4 `display_message_campaign_actions`

一个叙事可能绑定同回合的多个动作，使用关联表而不是在消息上放单个 `action_id`：

```sql
CREATE TABLE display_message_campaign_actions (
    display_message_id UUID NOT NULL REFERENCES display_messages(id) ON DELETE CASCADE,
    campaign_id UUID NOT NULL REFERENCES campaigns(id) ON DELETE RESTRICT,
    action_id VARCHAR(160) NOT NULL,
    revision BIGINT NOT NULL CHECK (revision > 0),
    PRIMARY KEY (display_message_id, action_id),
    CONSTRAINT fk_display_message_campaign_action
        FOREIGN KEY (campaign_id, action_id)
        REFERENCES campaign_action_commits(campaign_id, idempotency_key)
        ON DELETE RESTRICT
);
```

关联表只记录已通过校验的助手叙事；系统动作回执不参与“动作已消费”的判定。未消费动作查询按 `campaign_id + revision <= 当前 revision` 且不存在有效助手绑定来筛选。

## 7. 生成与提交协议

### 7.1 生成输入

主叙事 Prompt 中注入一段结构化、不可编辑的 `NarrativeContext`，至少包含：

- 当前 `revision`、`state_hash`、游戏时间、地点。
- 本回合全部未消费 `ActionResult`（最多 4 个）。
- 当前玩家投影（左侧状态栏使用的同一份数据）。
- 明确规则：状态变化只能来自 `changes`；`preserved` 中的字段不得写成“已恢复/已清除”；不得创造未出现于 `changes` 的获得物。

历史正文只用于语气与连续性，不得作为数值来源。若历史正文与当前快照冲突，以快照为准。

### 7.2 保存前校验

校验分三层，逐层短路：

**L0 结构校验**

- 输出非空、长度在上限内、不是工具协议残片。
- 不包含未闭合的状态 JSON、内部字段名或隐藏事实。
- Campaign 已结束时不再生成普通行动叙事。

**L1 硬事实校验**

只检查能够从 `ActionResult` 与快照确定的客观事实：

- 境界、阶段、位置、游戏时间、任务状态。
- 气血、法力、神识、修为、寿元、货币、库存数量。
- 关系数值与势力数值。
- 明确提到的物品、功法、NPC 是否真实存在且可见。

实现方式以“结构化事实 + 受控模式匹配”为主，例如识别“炼气九层”“筑基初期”“三块灵石”“气血回满”等可映射表达，再与对应路径比较。无法映射的文学表达不强行判定。

**L2 语义主张校验**

检查会改变玩家理解的强主张：

- “突破成功/失败”“伤势痊愈/加重”“击杀/逃脱”“获得/失去”“结盟/背叛”。
- 主张必须有同方向、同主体的 `FactChange` 或 `goal_status` 支撑。
- 否定句、假设句、回忆句不按既成事实处理。
- 不得把 `preserved` 写成变化，不得把 `blocked` 写成 `completed`。

当前 L2 采用确定性规则：只识别突破、伤势、气血、寿元、库存等能映射到路径的强主张。后续可增加一次轻量模型校验，但校验模型只能输出结构化违规列表，不能改写状态，也不能直接生成替代正文。

### 7.3 失败处理

1. 第一次失败：删除包含违规主张的句子并重新校验；仍使用同一个 `ActionResult` 与 `state_hash`。成功时标记 `validation_status='repaired'`。
2. 修复后仍失败：使用后端模板生成简短结果说明，例如“你尝试突破，但灵气不足，境界仍为炼气九层。”，标记 `validation_status='fallback'`。
3. 任何情况下都不重新执行动作，不修改 `campaign_action_commits.result`。
4. 校验器自身异常按失败处理，记录 `validation_detail.error`，不得放行未校验正文。

### 7.4 过期生成

写入 `display_messages` 的事务中重新读取 Campaign 并比较：

- `campaign.revision == context.revision`
- `campaign.state_hash == context.state_hash`

不一致时：

- 不写入正式叙事。
- 将生成标记为 `stale`，记录实际 revision/hash。
- 通过 WebSocket 发送可恢复提示，前端重新拉取快照。
- 不自动用新状态重写旧叙事，避免把玩家没做过的动作写进正文。

## 8. 前端一致性

### 8.1 原子快照

Campaign 快照接口返回 `campaign`（含 `revision`、`state_hash`）与完整玩家 `view`；WebSocket 的 `message_created` / `message_updated` 在助手消息上携带 `campaign_revision`、`campaign_state_hash`、`validation_status` 与 `validation_detail`。

```json
{
  "campaign_revision": 42,
  "campaign_state_hash": "…",
  "validation_status": "passed",
  "message": { "…": "本次叙事" }
}
```

前端以 `revision` 单调递增为准则：

- 同一对话只接受不小于当前 revision 的快照。
- 收到消息时先与当前快照比较：同 revision/hash 才写入 `chat`。
- 消息版本高于当前快照时先缓冲，拉取快照后在同一个同步块应用侧栏和消息。
- 消息版本低于当前快照时丢弃，避免旧正文配新侧栏。
- revision 跳号或同 revision 不同 hash 时丢弃增量并重新拉取完整快照。
- `ending_capabilities` 中未被 Presentation 渲染的可用终局动作进入通用“结局”区；带 `plans` 的终结任务按方案展开，`abandon_action` 不重复显示。

### 8.2 展示来源

- 左侧状态栏、动作按钮、结局弹窗只读结构化投影。
- 正文永不参与状态反推。
- 动作提交期间禁用同一动作按钮；提交成功后使用响应中的快照刷新，不等待叙事生成。
- 结局弹窗的“本世经历”同样来自终局快照，成功、失败、中止、寿元耗尽统一生成。

## 9. 自动归档与长期记忆

自动归档作为旁路服务：

- 输入只包括已提交事件、通过校验的叙事和绑定快照。
- 输出 Memory 保留来源回合/事件 evidence；额外写入 `campaign_revision` 属于后续增强，不改变只读边界。
- 可以报告“摘要与快照不一致”，但不能写 Campaign 状态、覆盖动作结果或触发动作重放。
- 超时、失败、模型不可用都不阻塞主回合，也不改变存档。
- 角色问答引用记忆时，必须同时读取当前快照；冲突时明确以当前快照为准。

## 10. 已落地的前置修复

以下问题会直接制造“叙事与状态栏不一致”的观感，已先行修复，后续改造需保留回归测试：

- 多槽功法装配在 UI 未传 `slot` 时，由后端选择空闲合法槽位，全部占用时才替换第一个合法槽位。
- 玩家与 NPC 的“交谈”动作允许玩家执行，每次固定 `+1 信任 / +1 亲近`，消耗 15 分钟游戏时间，且不能由客户端传入任意增量。
- 承诺与普通任务共用接受/提交动作时，投影同时合并 `quests` 与 `obligations`，避免按钮消失或语义错位。
- 所有结局路径统一生成“本世经历”，包括成功、失败、中止与寿元耗尽。

相关实现与测试：

- `backend/internal/gameplay/cultivation/campaignactions/actions.go`
- `backend/internal/gameplay/components/worldactions/actions.go`
- `backend/internal/storage/completion.go`
- `frontend/src/components/CampaignViewRenderer.vue`
- `frontend/src/views/ConversationView.vue`

## 11. 分阶段实施

### P0：契约与绑定（已完成）

- 定义 `ActionResult`、`FactChange`、`goal_status` 的 Go 类型与 JSON Schema。
- 在 `00001_init.sql` 增加 `state_hash`、动作结果版本与 `display_messages` 绑定列。
- 动作事务内计算并保存哈希；幂等重放返回同一结果与哈希。
- 叙事写入前校验 revision/hash，拒绝 stale。

验收：同一动作重放不改变状态；旧 revision 的叙事无法写入；任一 Campaign 消息都能追溯到动作与快照。

### P1：L0/L1 校验与兜底（已完成）

- 实现结构校验与硬事实验证。
- 失败先做一次确定性删除修复；仍失败写模板兜底。
- 记录 `validation_detail`，管理端可查询。

验收：境界、资源、库存、位置、任务状态的错误主张不会进入正式时间线。

### P2：L2 与前端原子刷新（已完成确定性部分）

- 实现可映射语义主张校验与否定/假设句处理。
- HTTP/WS 返回统一快照；前端按 revision 单调应用。
- 增加 stale 提示与自动重拉。

验收：突破失败、伤势未愈、关系仅 +1 等场景中，正文与侧栏完全一致。

### P3：归档、记忆与观测（后续增强）

- 归档与 Memory 绑定来源事件和 revision，保持只读。
- 增加校验失败、兜底、stale、重生成指标。
- 管理端提供按对话查看绑定与违规详情的诊断入口。

验收：归档失败不影响主回合；摘要冲突只产生告警；诊断能定位到具体动作与违规字段。

## 12. 测试矩阵

单元测试：

- `state_hash` 对键顺序不敏感，对语义变化敏感，对时间戳不敏感。
- `FactChange` 的 before/after/delta 由同一事务值生成。
- L1 数值、枚举、集合比较；否定句与假设句不误判。
- 隐藏事实不会进入玩家可见校验上下文。

对应文件：`backend/internal/storage/campaign_state_hash_test.go`、`backend/internal/gameplay/contract/action_result_test.go`、`backend/internal/orchestration/narrative_validate_test.go`。

集成测试：

- 动作提交后 revision 与 hash 同步递增；重放返回同一结果。
- 生成期间状态推进，旧叙事写入被拒绝。
- 校验失败重生成一次；动作没有第二次结算。
- 两次失败写入模板兜底并标记 `fallback`。
- 消息与快照在同一响应中返回，前端拒绝旧 revision。

对应文件：`backend/internal/storage/campaign_narrative_postgres_test.go`，以及现有 `campaign_postgres_integration_test.go`。

场景测试：

- 突破失败时正文不得写“突破成功”。
- “气血回满”不得推导出“伤势痊愈”。
- 交谈一次只增加 1 点信任与 1 点亲近。
- 装备多槽功法时侧栏槽位与后端一致。
- 成功、失败、中止结局弹窗均包含本世经历。

## 13. 观测与告警（后续增强）

建议指标：

- `narrative_validation_total{layer,result}`
- `narrative_fallback_total{action_key}`
- `narrative_stale_total`
- `narrative_regeneration_total{reason}`
- `narrative_validation_latency_ms`

日志只记录 `campaign_id`、`action_id`、`revision`、`state_hash` 前缀与违规 `path`，不记录完整状态或隐藏事实。

## 14. 非目标

- 不通过解析正文来修改角色状态。
- 不因叙事失败回滚已结算动作。
- 不追求对全部文学表达做通用事实核查，只校验能映射到权威路径的主张。
- 不用自动归档或长期记忆替代 Campaign 权威状态。
- 不为旧存档做兼容迁移；本项目当前以新建存档为准。


### 活动快照与业务资源恢复

activity_version 只表示活动稳定投影，不能作为消息、状态或 Campaign 的统一版本。完整活动快照可跳号直接应用；分支切换、回合变化、生成解锁和已知事件缺口仍恢复业务资源。首次订阅和重连等待 ready，REST 读取期间缓冲消息并按 ID/attempt 合并；当前 attempt 的持久消息不能因为终态快照先到而丢弃。无可比较版本的缓冲 state_update 触发补读，Campaign 继续验证 revision/hash。跨 endpoint 的并行 REST 并不构成原子快照。
