# Fatecraft 设计文档

Fatecraft 是“平台发布挑战、玩家参与挑战”的 AI 文字冒险系统，不是允许玩家自由拼装 Prompt 和世界书的通用聊天工具。本文档目录描述当前实现、维护边界和后续演进方向。

## 阅读顺序

1. `01-overview.md`：产品目标、核心对象、完整运行流程与非目标。
2. `03-product-features.md`：普通用户、管理员和系统能力及验收边界。
3. `15-challenge-authoring-guide.md`：不限题材的玩法包格式、制作流程与后端扩展边界。
4. `14-ascension-trial-gameplay.md`：内置“飞升试炼”的完整玩法规格。
5. `13-challenge-system.md`：挑战内容聚合、版本、发布、哈希与结算。
6. `04-conversation-isolation.md`：对话归属、生命周期、并发与版本隔离。
7. `08-context-management.md`：上下文预算、装配顺序和构建日志。
8. `09-worldbook-memory.md`：版本化世界书扫描与单对话摘要记忆。
9. `12-ai-actions.md`：结构化输出、两阶段判定和动作处理。
10. `05-frontend-vue.md`、`06-backend-gin.md`：前后端模块和数据流。
11. `07-database-postgresql.md`：权威 schema、表关系、约束与初始化。
12. `10-api-websocket.md`：HTTP 和 WebSocket 契约。
13. `11-roadmap.md`：已知边界和后续工作。

`02-sillytavern-reference.md` 记录早期参考来源，不代表 Fatecraft 对普通用户开放相同的模板管理能力。`12-ai-actions-examples.md` 是协议示例集，规则仍以正式专题和已发布挑战版本为准。

## 权威来源

发生冲突时按以下顺序判断：

1. 数据库关系与约束：`backend/scripts/sql/init.sql`。
2. 后端安全和事务行为：`backend/internal`。
3. 模型输出结构：`backend/data/schemas/narrative_response.json`。
4. 判定算法参数：`backend/data/roll_config.yaml`。
5. 官方挑战首次初始化材料：`backend/data/preset_packs/*`。
6. 本目录设计文档。

文档应解释实现和设计意图，但不能覆盖代码与 schema 的真实行为。发现不一致时，要么修改实现并验证，要么修正文档，不能让两套规则长期并存。

## 系统不变量

- PostgreSQL 是运行时挑战内容的唯一权威。
- 空数据库首次启动时，官方 JSON 在事务中自动初始化；数据库已有任意挑战包时完全跳过，绝不自动升级或覆盖。
- 发布版本不可原地修改；对话固定版本、选中资产和内容哈希。
- 保存草稿与发布使用同一版本行锁，发布基于锁定后的内容计算哈希。
- 普通用户不能读取或替换技术 Prompt、世界书、角色、场景和奖励规则。
- 普通用户只能操作自己的对话；归属不符统一按不存在处理。
- 判定结果由服务端生成并写入流水，模型不能自行宣布判定成功。
- 同一对话最多结算一次，奖励由后端事务计算和发放。
- 所有对话运行时数据按 `conversation_id` 和当前用户归属隔离。

## 文档维护规则

修改下列内容时必须同步维护对应专题：

| 改动 | 至少更新 |
| --- | --- |
| 挑战 Manifest、奖励、玩法包格式或发布流程 | 15、13、07，并同步对应题材玩法文档 |
| 境界、寿元、飞升路线或 Prompt 规则 | 14、12 |
| REST/WS 路由或响应格式 | 10、05、06 |
| 数据表、约束或初始化方式 | 07、README |
| 上下文顺序、预算或世界书算法 | 08、09 |
| 权限、登录或对话生命周期 | 03、04、10 |

新增“未来能力”时必须明确标为规划，不得写成已经实现。发布新挑战内容前，应同时检查初始化 JSON、发布校验、正式玩法文档和测试。
