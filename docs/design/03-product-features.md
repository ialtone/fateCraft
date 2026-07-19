# 03. 产品功能拆解

## 对话功能

对话是核心容器，包含角色、场景、历史、状态、摘要、世界书和 Prompt 预设。每个对话互相隔离，当前对话结束后只能切换到另一个对话或新建对话。

必须支持：

- 创建对话。
- 重命名对话。
- 结束对话。
- 归档对话。
- 删除对话。
- 搜索对话标题和最近消息。
- 对话内分页加载历史。
- 对话绑定角色卡、Persona、Prompt 预设和世界书。
- 对话之间默认不共享消息、状态、摘要和上下文日志。

## 消息功能

消息分为内部消息和展示消息。

内部消息用于上下文：

- role：system/user/assistant/tool/internal。
- content：给模型或系统使用的内容。
- hidden：是否对前端隐藏。
- token_count：缓存 token。

展示消息用于 UI：

- type：user/assistant/system/state/event。
- markdown：渲染文本。
- metadata：状态更新摘要、模型名、耗时等。

操作：

- 发送用户消息。
- 编辑消息。
- 删除消息。
- 重新生成。
- 继续生成。
- 标记消息为隐藏，不进入上下文。
- 后续支持 swipes/候选回复。

## 状态面板功能

状态面板不是固定字段，而是一个可配置 JSON schema。

默认结构：

- profile。
- attributes。
- inventory。
- relationships。
- location。
- quests。
- risks。
- world_flags。

状态变更来源：

- AI 的 `state_update`。
- 用户手动编辑。
- 系统规则修正。

每次状态变更都记录：

- before。
- patch。
- after。
- source。
- related_message_id。

## 角色卡

角色卡参考 SillyTavern，但不必完全兼容所有格式。

字段：

- name。
- description。
- personality。
- scenario。
- first_message。
- example_dialogues。
- system_prompt_override。
- tags。
- avatar_url，可选。

## Persona

Persona 是用户扮演身份。

字段：

- name。
- description。
- speaking_style。
- constraints。

注入策略：

- 作为高优先级 PromptSegment。
- 可参与世界书扫描。

## Prompt 预设

Prompt 预设由多个 PromptSegment 组成。

功能：

- 创建预设。
- 编辑分段。
- 启用/禁用分段。
- 排序。
- 查看 token。
- 对话绑定预设。

## 世界书

用于设定注入。

条目字段：

- title。
- content。
- keywords。
- constant。
- priority。
- enabled。
- budget_group。
- notes。

## 上下文检查器

用于解释本次请求上下文。

需要展示：

- PromptSegments。
- token 明细。
- 世界书命中。
- 摘要版本。
- 历史窗口。
- 裁剪原因。
