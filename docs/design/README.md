# Fatecraft 设计文档

这组文档把项目拆成多个小设计单元，便于后续逐步实现和调整。

## 阅读顺序

1. `01-overview.md`：先看项目整体目标和边界。
2. `02-sillytavern-reference.md`：确认从 SillyTavern 借鉴哪些能力。
3. `03-product-features.md`：确认最终要做哪些产品功能。
4. `04-conversation-isolation.md`：确认对话之间如何隔离。
5. `08-context-management.md`：重点阅读，决定长对话质量。
6. `05-frontend-vue.md` 和 `06-backend-gin.md`：分别看前后端实现。
7. `07-database-postgresql.md`：看数据表和存储边界。
8. `09-worldbook-memory.md`：看世界书和记忆系统。
9. `10-api-websocket.md`：看接口草案。
10. `12-ai-actions.md`：看 AI 动作系统（自动结局等）。
11. `11-roadmap.md`：看任务拆分和 MVP。

## 当前产品定位

Fatecraft 不是简单复刻 `TenCyclesofFate`，也不是完整克隆 `SillyTavern`。它要做的是：

- 用 `TenCyclesofFate` 的沉浸式叙事和结构化角色状态作为体验基础。
- 用 `SillyTavern` 的上下文管理、Prompt 管理、世界书、角色卡、消息编辑等成熟思路作为工程基础。
- 用 Go + Vue 做一个更清晰、前后端分离、可扩展的系统。
- 每个对话独立运行，当前对话结束后切换到下一个对话或新开对话，默认互不关联。

## 明确不做

- 不做每日次数限制。
- 不做兑换码奖励。
- 不做观摩频道。
- 不做图片生成模块。
- 不在第一阶段做登录权限。
- 不做跨对话自动共享记忆。
