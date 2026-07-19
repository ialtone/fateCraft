# 05. Vue 前端设计

## 技术栈

- Vue 3。
- Vite。
- TypeScript。
- Pinia。
- Vue Router。
- marked。
- DOMPurify。

## 页面结构

### 对话列表页 `/`

功能：

- 显示对话列表。
- 新建对话。
- 搜索对话。
- 打开对话。
- 结束对话。
- 归档和删除。
- 按 active、ended、archived 过滤。

### 主对话页 `/conversations/:id`

布局：

```text
┌──────────────────────────────────────────────┐
│ 顶部：对话标题 / 模型 / 保存状态 / 对话状态   │
├──────────────┬─────────────────┬─────────────┤
│ 状态面板      │ 剧情时间线       │ 上下文检查器 │
│ StatePanel   │ MessageTimeline │ Context     │
├──────────────┴─────────────────┴─────────────┤
│ 底部输入 ActionInput                          │
└──────────────────────────────────────────────┘
```

当对话状态为 ended：

- 输入框禁用。
- 继续、重试、编辑状态默认禁用。
- 显示“此对话已结束，可返回列表或新建对话”。

### 角色卡页 `/characters`

- 创建角色。
- 编辑描述、性格、开场白、示例对话。
- 绑定标签。

### 世界书页 `/worldbooks`

- 世界书列表。
- 条目编辑。
- 关键词和优先级配置。
- token 估算。

### Prompt 页 `/prompts`

- Prompt 预设列表。
- 分段编辑。
- 启用/禁用。
- 排序。
- token 预览。

### 设置页 `/settings`

- Base URL。
- API Key，由后端保存或环境变量管理。
- 模型名。
- 最大上下文。
- 最大回复 token。
- temperature/top_p。

## 组件拆分

- `ConversationLayout.vue`：主对话布局。
- `ConversationList.vue`：对话列表。
- `ConversationStatusBadge.vue`：active/ended/archived 状态。
- `StatePanel.vue`：左侧状态面板。
- `StateTree.vue`：递归渲染 JSON 状态。
- `MessageTimeline.vue`：消息列表。
- `MessageItem.vue`：单条消息，含编辑/删除/复制。
- `ActionInput.vue`：输入框和发送操作。
- `ContextInspector.vue`：上下文明细。
- `PromptSegmentList.vue`：Prompt 分段列表。
- `WorldBookEntryEditor.vue`：世界书条目编辑。
- `CharacterEditor.vue`：角色卡编辑。

## Store 设计

### useConversationStore

- currentConversation。
- conversationList。
- publicState。
- loadConversation。
- createConversation。
- updateConversation。
- endConversation。
- archiveConversation。
- switchConversation。

### useChatStore

- messages。
- isGenerating。
- sendMessage。
- editMessage。
- deleteMessage。
- regenerate。
- continueMessage。

### useSocketStore

- connect(conversationId)。
- disconnect。
- reconnect。
- handleMessage。
- sendUserInput。
- switchConversation 时关闭旧连接并打开新连接。

### useContextStore

- lastContextBuild。
- loadLastContext。
- selectedSegment。

### useWorldBookStore

- worldBooks。
- entries。
- saveEntry。
- testActivation。

## 交互细节

- 发送消息后立即把用户消息插入当前对话时间线。
- AI 流式返回时显示正在生成的 assistant 消息。
- 状态更新到达后刷新当前对话左侧面板。
- 编辑历史消息后提示用户“后续上下文会受影响”。
- 重新生成会删除或隐藏当前对话上一条 AI 消息，然后重新请求。
- 上下文检查器默认折叠，调试时展开。
- 切换对话时清空当前消息缓存、状态缓存和上下文检查器缓存。
- 新建对话时可选择相同角色和世界书模板，但不继承旧对话运行时数据。
