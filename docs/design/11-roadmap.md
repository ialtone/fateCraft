# 11. 路线图与任务拆分

## MVP 目标

做出一个能长期自由对话、能维护状态、能控制上下文的最小完整版本。

MVP 包含：

- Gin 后端。
- PostgreSQL。
- Vue + Vite + TypeScript。
- 对话创建、结束、切换和消息历史。
- 流式 AI 回复。
- 左侧结构化状态面板。
- Prompt 预设基础版。
- 世界书关键词触发。
- 摘要记忆基础版。
- 上下文检查器基础版。

MVP 不包含：

- 登录权限。
- 向量记忆。
- 高级世界书递归。
- 插件系统。
- TTS、图片、图库。

## 阶段 1：工程骨架

- 创建 backend 和 frontend。
- 初始化 Gin。
- 初始化 Vue。
- 配置 PostgreSQL 连接。
- 配置 Vite 代理。
- 添加 health check。

## 阶段 2：数据库和基础 CRUD

- migrations。
- conversations CRUD。
- messages CRUD。
- state CRUD。
- prompt presets CRUD。
- world books CRUD。

## 阶段 3：主聊天界面

- 对话列表。
- 主对话页。
- 结束当前对话并切换到其他对话。
- 消息时间线。
- 输入框。
- WebSocket 连接。
- 流式显示。

## 阶段 4：LLM 和上下文

- LLM client。
- PromptSegment。
- token counter。
- 最近历史窗口。
- 当前状态注入。
- context build log。

## 阶段 5：状态系统

- character_state schema。
- AI state_update 校验。
- 状态 patch。
- StatePanel 展示。
- 手动编辑状态。

## 阶段 6：世界书和摘要

- 世界书关键词扫描。
- 世界书预算。
- 摘要生成。
- 摘要版本。
- ContextInspector 展示。

## 阶段 7：打磨和测试

- 消息编辑。
- 重新生成。
- 继续生成。
- API 测试。
- DAO 测试。
- 上下文预算测试。
- 前端组件测试。
