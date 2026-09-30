# Fatecraft 设计文档

Fatecraft 是“平台发布挑战、玩家参与挑战”的 AI 文字冒险系统，不是允许玩家自由拼装 Prompt 和世界书的通用聊天工具。本文档目录描述当前实现、维护边界和后续演进方向。

## 阅读顺序

1. `01-overview.md`：产品目标、核心对象、完整运行流程与非目标。
2. `03-product-features.md`：普通用户、管理员和系统能力及验收边界。
3. `15-challenge-authoring-guide.md`：不限题材的玩法包格式、制作流程与后端扩展边界。
4. `17-generic-campaign-action-capabilities.md`：不限题材的 Campaign 动作能力、事务复核、模块接入和 Agent 维护规范。
5. `21-campaign-outcome-rewards.md`：不限题材的 Campaign 终局固定积分、事务和幂等规范。
6. `16-cultivation-campaign-agent-reference.md`：已实现的修仙长期 Campaign 权威边界、动作目录和内容扩展参考。
7. `20-cultivation-shared-setting.md`：两个内置修仙玩法共享的世界设定及独立运行边界。
8. `14-ascension-trial-gameplay.md`：内置单局挑战“百年飞升劫”的当前完整玩法规格。
9. `13-challenge-system.md`：挑战内容聚合、版本、发布、哈希与结算。
10. `04-conversation-isolation.md`：对话归属、生命周期、并发与版本隔离。
11. `18-async-memory-character-inquiry.md`：异步长期记忆、完整回合上下文与左栏只读问答。
12. `08-context-management.md`：上下文预算、装配顺序和构建日志。
13. `09-worldbook-memory.md`：版本化世界书扫描及其与长期记忆的边界。
14. `19-agent-tool-loop.md`：主叙事文本与真实工具循环、Provider 适配、事务、恢复和显式诊断。
15. `12-ai-actions.md`：状态、判定、结算与 Campaign 工具处理。
16. `05-frontend-vue.md`、`06-backend-gin.md`：前后端模块和数据流。
17. `07-database-postgresql.md`：权威 schema、表关系、约束与初始化。
18. `10-api-websocket.md`：HTTP 和 WebSocket 契约。
19. `11-roadmap.md`：已知边界和后续工作。
20. `future-gameplay-directions.md`：未来玩法方向、扩展规则与候选方向（长期备忘）。

`02-sillytavern-reference.md` 记录早期参考来源，不代表 Fatecraft 对普通用户开放相同的模板管理能力。`12-ai-actions-examples.md` 是协议示例集，规则仍以正式专题和已发布挑战版本为准。

账号奖励专题：[22-reward-management-and-exchange.md](22-reward-management-and-exchange.md)，覆盖固定通关积分、注册赠送、管理员发放与撤回、积分兑换改写次数。

日志专题：[23-logging-and-diagnostics.md](23-logging-and-diagnostics.md)，覆盖诊断关联、正文预算、队列、配置审计、后台筛选及运行验收边界。

## 权威来源

发生冲突时按以下顺序判断：

1. 数据库关系与约束：按版本顺序执行的 `backend/migrations/*.sql`。
2. 后端安全和事务行为：`backend/internal`。
3. 模型协议、工具目录与结构化契约：`backend/internal/llm`、`backend/internal/orchestration/agent_tools.go`、`backend/internal/memory/contract.go` 和 `backend/internal/inquiry`。
4. 判定算法参数：`backend/data/roll_config.yaml`。
5. 官方挑战首次初始化材料：`backend/data/preset_packs/*`。
6. 本目录设计文档。

文档应解释实现和设计意图，但不能覆盖代码与 schema 的真实行为。发现不一致时，要么修改实现并验证，要么修正文档，不能让两套规则长期并存。

## 系统不变量

- PostgreSQL 是运行时挑战内容的唯一权威。
- 用户角色只有 `owner`、`admin`、`user`；普通注册创建 `user`，OAuth 只允许登录已绑定的本地账号，不会自动创建或提权。
- 空数据库必须通过网页首次安装向导明确设置唯一 `owner`；初始化完成后安装入口关闭，后端事务保护 owner 转交和失权操作。
- 空数据库首次启动时，官方 JSON 在事务中自动初始化；数据库已有任意挑战包时完全跳过，绝不自动升级或覆盖。
- 发布版本不可原地修改；对话固定版本、选中资产和内容哈希。
- 已发布/退役版本的进程缓存只保存不可变内容；所有玩家状态、历史、Memory 和 Campaign 事实保持按用户/对话实时隔离。
- 保存草稿与发布使用同一版本行锁，发布基于锁定后的内容计算哈希。
- 普通用户不能读取或替换技术 Prompt、世界书、角色、场景和奖励规则。
- 普通用户只能操作自己的对话；归属不符统一按不存在处理。
- 判定结果由服务端生成并写入流水，模型不能自行宣布判定成功。
- 主叙事只返回普通文本或调用当前步骤快照中的一个真实工具；单个模型步骤不因解析或领域结果失败而重放，传输层临时错误按有限策略处理，不换协议、不降低约束。
- Memory 与角色问答只使用原生 JSON Schema，不降级到固定提交 Tool、`json_object` 或 Prompt-only JSON。
- 所有工具参数和结构化对象都由后端严格校验；非法工具不产生领域写入，已 committed 的工具不会因后续叙事失败而回滚。
- 账号只保留积分和剧情改写次数，实际余额由不可变账本驱动；独立奖励管理与账号积分兑换共用账本，扣分/发奖/订单同事务。
- 同一对话最多结算一次；Campaign 终局也由后端在领域事务中发放固定积分。
- `client` 路由（客户端直连）的对话与 Campaign 不产生通关积分：结算与终局照常展示结局，但跳过全部授予，改写/重新生成不扣费；路由创建时固化、不可修改。
- 每个运行时字段只有一个所有权域；Campaign 长期事实只能经 `field_ownership`、可信 Action 和领域事件写入，conversation state 不得作为权威事实的兜底。
- 所有对话运行时数据按 `conversation_id` 和当前用户归属隔离。
- 每次可写生成由 `conversation_generation_runs` durable owner 负责；`conversations.is_processing` 仅为兼容性忙碌投影，不能作为写入授权或释放条件。
- 模型、HTTP body、SSE、审计、WebSocket、工具参数和列表分页均有明确字节/数量上限；超限请求失败或断开慢连接，不静默丢弃可信事实。
- 匿名分享只能读取专用、不可变、允许列表投影的公开快照，不能把原对话 API 或 WebSocket 变成公开数据源。

## 文档维护规则

修改下列内容时必须同步维护对应专题：

| 改动 | 至少更新 |
| --- | --- |
| 挑战 Manifest、奖励、玩法包格式或发布流程 | 15、13、07，并同步对应题材玩法文档 |
| 共享境界、寿元与修仙基础语义 | 20，并同步 14、16 和两个内置玩法包 |
| 单局飞升路线或 Prompt 规则 | 14、12 |
| REST/WS 路由或响应格式 | 10、05、06 |
| 数据表、约束或初始化方式 | 07、README |
| 上下文顺序、预算、Memory Worker、只读问答或世界书算法 | 18、08、09 |
| 已发布内容缓存、容量、指标、世界书编译或玩法定义读取边界 | 06、08、09、13、17、18，并同步 README 配置 |
| 权限、登录或对话生命周期 | 03、04、05、06、07、10 |
| LLM 请求路由、Relay 传输、客户端直连放权 | 03、04、05、06、07、10、18、19、21 |
| 对话分享、公开快照、分享 token、举报或治理 | 03、04、05、06、07、10，并同步 README 与 docker/README |
| 通用 Campaign 动作、能力投影、Runtime 白名单或事务协议 | 17，并同步 06、08、12、13、15 |
| Runtime 字段所有权、Campaign/会话边界、状态工具或 Snapshot 来源 | 17、19，并同步 05、07、15 |
| 账号奖励、发放、注册赠送、撤回或兑换 | 22，并同步 03、05、06、07、10、13、15、21 |
| Campaign 终局积分、账号收益快照或账本语义 | 21，并同步 05、07、10、13、15 和对应玩法文档 |
| 主叙事 Tool Loop、Provider 传输、工具事务、JSON Schema 或显式诊断 | 19，并同步 06、07、10、12、18 |
| 生成租约、分页游标、请求/队列边界或连接池参数 | 04、06、07、08、10、19 |
| 功法、库存、NPC、地图、世界事件、遭遇或长期结局 | 16，并同步 03、05、06、07、08、10、12、13 |

新增“未来能力”时必须明确标为规划，不得写成已经实现。发布新挑战内容前，应同时检查初始化 JSON、发布校验、正式玩法文档和测试。
