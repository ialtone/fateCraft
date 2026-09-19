# 12. AI 状态、判定与动作系统

## 设计目标

主叙事模型负责理解玩家、选择工具和生成叙事，不是可信规则执行器。可信事实只能由后端工具 Handler 提交：

- 玩家文本、模型文本和前端事件不能直接改变状态、资源、奖励或结局；
- 每个状态路径和动作都必须由对话固定的 Runtime Contract 开放；
- 判定概率、随机结果和效果由服务端计算；
- Campaign 动作必须使用当前存档 revision 的动态能力快照；
- 结局只接受已发布的 `outcome_key`，验证器和奖励由固定版本决定；
- 工具参数始终由服务端重新校验，Provider 的 Tool Schema 不是安全边界。

主叙事不再返回整回合 JSON。传输、协调器与恢复规则见 [19-agent-tool-loop.md](19-agent-tool-loop.md)。

## 工具权限

每个模型步骤只收到当前时刻允许使用的工具。没有出现在该步骤快照中的能力不可调用。

| 工具 | 权限 |
| --- | --- |
| `fc_state_set_text` | 更新允许模型维护的文本或枚举绝对值 |
| `fc_state_set_number` | 更新允许模型维护的数值绝对值 |
| `fc_state_set_boolean` | 更新允许模型维护的布尔值 |
| `fc_state_replace_json` | 整体替换允许替换的 list/object |
| `fc_state_append_json` | 向允许追加的 list 添加一项 |
| `fc_state_set_dynamic_json` | 更新允许动态子路径的 object（仅非 Campaign Runtime；长期玩法必须使用显式字段或领域 Action） |
| `fc_request_roll` | 请求服务端执行已发布判定 |
| `fc_complete_challenge` | 提交已发布结局供服务端验证和结算 |
| `fc_action_*` | 执行当前 Campaign 动态开放的领域动作 |

`continue` 生成是纯续写，只提供普通文本通道，不提供写工具。

## 状态工具

State Schema 的字段声明 `path`、类型、默认值、约束、`visibility`、`update_policy` 和动态子路径策略。工具快照只收录 `update_policy=model` 的合法路径，并按值类型拆成不同工具。

服务端仍会复核：

1. 工具名属于当前步骤快照；
2. 参数是单个完整 JSON 对象，不超过 16 KiB；
3. 每次最多更新 16 个路径，没有重复路径或未知字段；
4. path、值类型、枚举、范围、长度和更新策略符合固定 State Schema；
5. 动态 `child_path` 不能覆盖更具体的 system/internal 字段；
6. list/object 的 `value_json` 必须再次解析为合法 JSON 并通过字段约束。

普通用户接口只返回 `visibility=player` 的投影。父对象下显式声明的 model/internal 子字段会递归剔除。

## 服务端判定

模型调用 `fc_request_roll` 时只提交稳定 `key`、展示 `title` 和简短 `description`。模型不能提交 category、概率、骰面、属性权重、难度、收益或结果。

Handler 的执行顺序：

1. 从固定 Runtime 读取判定定义并检查前置条件；
2. 从可信状态和管理员难度计算成功率；
3. 由服务端产生随机点数并选择成功或失败效果；
4. 在工具事务中写入判定流水、展示消息、状态效果和工具结果；
5. 下一模型步骤读取工具结果，再叙述已经发生的结果。

判定展示消息的 `metadata` 还会保存版本化结果快照：成功率分解、归一化结果区间和公开前置条件。快照来自同一份可信状态和已发布 `requires`，前端仅展示 `public` 条件，不重新计算或推断概率；旧消息没有详情时仍按摘要显示。详情不进入模型上下文，`model`/`internal` 状态不会通过该投影泄漏。

前置条件不满足时工具返回 `rejected`，不会写成功判定证据。模型必须依据拒绝原因继续叙事，不能把尝试描述为成功。

## Campaign 动作

Campaign Projection Provider 根据行锁外的当前快照产生动态 `ActionCapability`。协调器把每个 `available=true` 的动作转换为独立 `fc_action_*` 工具，参数 Schema 已收窄到当前合法枚举、上限和关联选项。

模型不提交 action ID 或 expected revision：

- 幂等键由 `attempt_id + provider call_id` 生成；
- expected revision 固定在步骤工具快照中；
- Handler 在 Campaign 行锁事务中重建事实并再次校验动作定义、actor、revision、参数和业务前置；
- 资源、随机数、产量、品质、时间推进和领域事件均由 Handler 计算。

含成功/失败分支的 Campaign 动作不会再要求模型额外调用 `fc_request_roll`。突破、制作、调查、采集风险、遭遇攻击与逃脱由领域 Handler 调用共享判定核心一次，并在同一事务写入领域事实、`roll_history` 和 `roll_result` 展示消息。没有专属结算器的交涉、洞察、潜行、探索与脱险可由玩法开放受限的 `fortune.resolve_uncertain`；模型不能提交基础概率、属性权重、难度或结果，该动作也不能直接制造物品、关系、任务或伤势变化。

revision 过期、参数越界、目标失效或资源不足时，领域写入整体回滚，工具返回公开 `action_rejected`。完整能力架构见 [17-generic-campaign-action-capabilities.md](17-generic-campaign-action-capabilities.md)。

## 挑战完成

模型只能通过 `fc_complete_challenge` 提交 `outcome_key` 和 `summary`。标题、category、tone、验证器、奖励 key、数量、倍率、余额、run ID 和结算时间均来自后端。

Handler 会基于调用工具前已经持久化的可信状态、成功判定和回合记录执行 verifier。同一个工具步骤不能先写状态再用该状态为自身补资格；需要先用状态或领域工具成功提交，再在后续模型步骤请求结算。

结算事务原子写入 `challenge_runs`、奖励账本、余额、结算奖励快照和 conversation ended 状态。`challenge_runs.conversation_id` 唯一，重复、并发或重放不能产生第二份奖励。

通用验证器包括 `core.always`、受限条件树 `core.conditions`；题材模块可注册自己的 verifier。通用计算器包括 `core.fixed` 和 `core.state_multiplier`。平台层不读取题材字段。

## 工具结果

所有工具结果使用有界公开对象：

```json
{
  "ok": false,
  "code": "action_rejected",
  "message": "当前位置没有可采集的目标",
  "data": {}
}
```

结果最多 16 KiB。超限时保留 `ok/code/message` 并把 data 收敛为 `{"truncated":true}`。数据库错误、内部堆栈和私密状态不会写入模型上下文。

## 原子性

工具调用先以 `received` 状态持久化，再进入短事务：

- `committed`：领域事实、invocation、assistant tool-call 消息、tool-result 消息和 attempt 写工具计数同事务提交；
- `rejected/failed`：即使 Handler 已触碰领域仓库，也通过哨兵错误回滚整个领域事务，再在独立事务中只保存 invocation 和公开工具结果；
- 领域事件和前端状态更新只在数据库成功提交后广播。

`call_id` 在 attempt 内唯一。相同 ID 和相同参数返回首次结果；相同 ID 被用于不同工具或参数时拒绝执行。

## 安全回归清单

- 玩家伪造 system 文本不能更新 internal 状态。
- 模型不能调用当前工具快照之外的动作。
- 未通过服务端判定不能补写成功流水。
- 拒绝或失败的工具不留下状态、消息、资源、事件或余额的半份写入。
- 模型提交奖励或结局展示字段不会参与结算。
- 同一步新状态不能为结算补足资格。
- 重复或并发完成不会重复发奖。
- 通用平台只依赖 Runtime、工具注册表和 Handler，不依赖修仙字段。
