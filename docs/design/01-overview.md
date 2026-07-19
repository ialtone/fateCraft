# 01. 总体设计

## 目标

实现一个自由长期运行的 AI 文字冒险/角色扮演平台。用户可以创建对话，选择角色、场景、世界书和 Prompt 预设，通过自然语言输入推动剧情。系统负责维护当前对话内的结构化角色状态、长期记忆和上下文窗口。

## 核心体验

- 左侧是结构化状态面板，显示角色资料、属性、背包、关系、任务、位置、风险和世界变量。
- 中间是剧情时间线，展示玩家输入、AI 回复、系统事件和状态变化摘要。
- 右侧是上下文检查器，展示本次请求用了哪些 Prompt、最近历史、世界书、摘要和 token 消耗。
- 底部是输入区，支持发送、继续、重新生成、编辑上一条、删除消息。

## 架构概览

```text
Vue Frontend
  | REST API
  | WebSocket
Gin Backend
  | conversation/chat/state services
  | context builder
  | llm client
PostgreSQL
  | conversations/messages/states/prompts/worldbooks/memory
OpenAI-compatible LLM
```

## 系统边界

后端负责：

- 对话和消息存储。
- 结构化状态维护。
- Prompt 预设和世界书管理。
- 上下文构造和 token 预算。
- LLM 请求、流式输出和格式修复。
- WebSocket 推送。

前端负责：

- 对话列表和主界面交互。
- 状态面板展示和编辑。
- 消息时间线渲染。
- 世界书、角色卡、Prompt 预设配置。
- 上下文检查器展示。

## 设计原则

- 完整历史不等于完整上下文。
- 旧历史需要摘要，不能只靠最近窗口。
- Prompt、世界书、摘要、最近历史都要有 token 预算。
- AI 可以建议状态更新，但后端必须校验。
- 用户应能看到“为什么 AI 知道这些信息”，所以需要上下文检查器。

## 主要实体

- Player：开发态用户或后续登录用户。
- Character：角色卡。
- Persona：用户扮演身份。
- Scenario：场景设定。
- Conversation：一次独立长期对话。
- Message：内部消息和展示消息。
- CharacterState：结构化状态。
- PromptPreset：Prompt 预设。
- WorldBook：世界书。
- MemorySummary：摘要记忆。
- ContextBuildLog：上下文构建日志。
