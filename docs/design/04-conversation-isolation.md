# 04. 对话隔离与生命周期

## 核心要求

系统必须按“对话”组织内容。一次对话结束后，用户可以切到下一个已有对话，也可以直接新开对话。不同对话之间默认互不联系。

## 术语

- Conversation：产品层面的对话，是用户看到和管理的最小剧情单位。
- Message：某个对话中的单条用户、AI 或系统消息。
- Conversation State：某个对话独立维护的结构化状态。
- Conversation Memory：某个对话独立维护的摘要记忆。
- Context Build：某次请求 AI 时，基于当前对话构造出来的上下文记录。

## 隔离原则

- 每个对话有独立消息历史。
- 每个对话有独立结构化状态。
- 每个对话有独立摘要记忆。
- 每个对话有独立上下文构建日志。
- 每个对话有独立生成锁。
- 当前对话构造上下文时，只读取当前 conversation_id 下的数据。
- 角色卡、Prompt 预设、世界书可以作为模板被多个对话选择，但它们不是运行时记忆。
- 默认不把 A 对话的历史、状态、摘要或世界书激活结果带入 B 对话。

## 生命周期

对话状态建议：

- active：正在进行。
- ended：已结束，不再继续生成，但可查看和复制。
- archived：归档隐藏。
- deleted：软删除或硬删除，MVP 可先软删除。

流程：

1. 用户新建对话。
2. 用户选择角色、Persona、世界书和 Prompt 预设。
3. 系统初始化该对话的独立状态。
4. 用户在该对话内持续输入和生成。
5. 用户点击“结束对话”。
6. 后端将 conversation.status 改为 ended。
7. 前端返回对话列表，用户选择下一个对话或新建对话。

## 新开对话

新开对话时可以复用模板：

- 同一个角色卡。
- 同一个 Persona。
- 同一个 Prompt 预设。
- 同一个世界书集合。

新开对话时不复用运行时数据：

- 不复用旧消息。
- 不复用旧 character_state。
- 不复用旧 memory_summary。
- 不复用旧 context_build_logs。
- 不复用旧世界书命中状态。

## 从旧对话派生

可以后续做“从此处另开分支”，但必须显式操作。

派生对话规则：

- 用户明确点击“从当前状态创建新对话”。
- 后端复制选定消息之前的历史或复制当前状态。
- 新对话拥有新的 conversation_id。
- 新对话之后的消息、摘要、上下文日志仍独立保存。
- UI 必须标记来源，避免用户误以为自动共享。

MVP 暂不做派生，只做新建、结束、切换。

## 前端交互

对话列表需要展示：

- 标题。
- 角色名。
- 更新时间。
- 状态：active/ended/archived。
- 最近一条展示消息预览。

主对话页需要提供：

- 结束对话按钮。
- 返回对话列表按钮。
- 新开对话按钮。
- 当前对话状态提示。

当对话 ended：

- 输入框禁用。
- 继续生成、重试、编辑状态默认禁用。
- 可以复制为新对话，后续版本支持。

## 后端约束

- 所有消息 API 必须校验 conversation_id。
- 所有状态 API 必须校验 conversation_id。
- ContextBuilder 必须只接收一个 conversation_id。
- MemoryService 必须只读取当前 conversation_id 的摘要。
- WorldBookService 可以读取模板世界书，但激活日志必须写当前 conversation_id。
- WebSocket 路径绑定 conversation_id，不能在一个连接里混用多个对话。

## 数据约束

所有运行时表必须带 `conversation_id`：

- chat_messages。
- display_messages。
- conversation_states。
- state_revisions。
- memory_summaries。
- context_build_logs。

模板表不需要绑定单一对话：

- characters。
- personas。
- prompt_presets。
- world_books。
