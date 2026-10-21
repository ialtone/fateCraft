# 日志、诊断与配置审计

本文描述日志系统的当前代码契约。2026-09-29 已完成本地实现与构建检查；2026-09-30 由项目维护者接手真实环境测试，代码与文档收尾完成。真实 PostgreSQL、Nginx/容器、浏览器联调和 race 检查仍待维护者记录结果，不视为已经通过。

## 日志分类

| 类别 | 保存位置 | 默认行为 |
| --- | --- | --- |
| HTTP、SQL、故障、任务摘要 | Zap → stdout | 生产 `LOG_LEVEL=info`、`LOG_FORMAT=json` |
| 模型逻辑调用摘要 | `llm_request_logs` | 始终记录；异步写入，容量不足允许丢弃 |
| 模型请求、原始响应、合并文本 | 同一条模型日志 | 详细正文默认关闭，开启后有界采集 |
| 上下文预算、分段、引用和裁剪 | `context_build_logs` | 保留诊断元数据，不持久化完整 Prompt |
| 奖励账本、订单、管理员及分享治理审计 | 各业务表 | 随业务事务保存，不进入可丢弃队列 |

诊断缺失不能证明业务未执行。日志写入失败不重试模型、不重放工具、不再次发奖。详细正文可能包含玩家输入、历史、模型输出及内部玩法内容，仅管理员可读；凭据过滤不等于识别任意自然语言中的所有隐私。

## 脱敏与故障响应

HTTP 只记录路由模板；未匹配请求使用 `[unmatched]`，不记录查询串、认证头、Cookie 或请求正文。分享路径覆盖 `/s/`、`/shared/` 和 `/api/public/conversation-shares/`。URL 移除 userinfo、query、fragment；错误日志不输出 panic 值，保留类型与调用栈。PostgreSQL 错误只保留 SQLSTATE 和约束名，参数化 SQL 不展开绑定值。

未知故障返回 HTTP 500：

```json
{"error":"服务暂时不可用","code":"internal_error","trace_id":"<UUID>"}
```

模型列表代理失败返回安全的 `502/upstream_error`。已定义的业务拒绝继续使用原有状态与可公开提示；配置竞争返回 `409/settings_conflict`。内部原因附到请求错误上下文，由访问日志输出。前端错误提示附带服务端提供的关联 ID，便于报障。

WebSocket Agent 失败也不直接透传内部错误或上游正文，终止事件的 payload 带 trace_id，玩家提示包含关联 ID；服务端保留安全的失败摘要。

Nginx access log 使用安全路径和后端 `X-Request-ID`；未到达后端的请求使用代理 ID，并以 `source=proxy` 区分。省略原始 URI、查询串、用户名、Referer 和 User-Agent。站点级原生 error log 被抑制，防止匹配 location 前的错误再次带出原始请求；运行时失败保留在安全访问摘要中，包括状态、upstream 状态和耗时。镜像主进程的配置/启动错误仍使用其默认 stderr。真实失败请求与启动错误输出需要通过容器验收。

## 关联与级别

`trace_id` 是服务端生成的执行关联 UUID：每个 HTTP 请求、WebSocket 业务命令及 Memory 任务执行分别生成，不采信客户端的同名请求头。HTTP 响应通过 `X-Request-ID` 返回，跨域响应显式暴露该头。排队的 WebSocket 命令保留各自的 trace。

`request_id` 继续表示业务幂等键。`conversation_id`、`turn_id`、`attempt_id`、`job_id` 和 `operation_id` 按现有业务身份关联；这些值通过 context 传给 GORM 与相关日志。进程重启后的执行有新 trace，通过持久化业务 ID 串联。

一个逻辑模型调用在第一次网络尝试前分配 `llm_call_id`，也用作模型日志行 `id`。重试告警携带同一 ID 与递增尝试序号，最终摘要记录 `transport_attempts`。真实工具循环的不同模型步骤各有一条摘要，不能把网络重试当成新的业务 attempt。

| 事件 | 级别 |
| --- | --- |
| 成功 `/api/health` 的后端访问 | 不输出常规事件 |
| 普通访问、参数拒绝、未登录、业务冲突 | Info |
| 403、429、SQL 约束、模型重试 | Warn |
| 未预期 5xx、数据库故障、panic | Error |
| 正常模型步骤、工具耗时、普通 SQL/未命中 | Debug |
| 模型步骤达到 60 秒、工具达到 1 秒、SQL 超过 200 ms | Warn |
| Agent 回合完成/失败 | 保留 Info/Error 摘要 |

慢阈值只改变日志级别，不改变业务超时。

Campaign 叙事校验的诊断写入 `display_messages.validation_detail`，包含 layer、违规 code/path/claim、期望值与实际值、原始校验状态和修复/兜底标记。日志只记录 `campaign_id`、`action_id`、`revision`、`state_hash` 前缀和违规 path，不记录完整领域状态、隐藏事实或整段正文；模型正文仍遵循详细正文采集开关。

## 模型摘要和正文预算

摘要包含模型、协议、用途、设置来源/修订、开始时间、总耗时、HTTP 状态、网络尝试次数、成功状态、稳定错误码，以及供应商提供时的 token 使用量。未知 usage 保持空值。传输成功与结构化交付成功分别保存；非结构化请求的结构状态在界面显示“不适用”。

关闭正文或不匹配采集范围时 `request_body={}`，原始响应和合并文本为空；流式解析仅计数，不再为诊断保留 SSE 副本。开启且匹配范围后先过滤已知 API Key、认证字段和 URL 凭据，再按编码后的实际字节限制：

| 字段 | 上限 |
| --- | --- |
| 请求 JSON / 诊断预览对象 | 512 KiB |
| 原始响应前缀 | 512 KiB |
| 合并文本前缀 | 256 KiB |
| 单条估算载荷（含元数据） | 2 MiB |

合法小请求保存 JSON 对象。超大请求保存明确的 `_diagnostic` 预览对象，计入 JSON 转义膨胀；无效或空 JSON 用省略标记表示，不保留无法可靠解析和过滤的原始片段。正文处理替换无效 UTF-8/NUL，按完整字符截断；JSON 和 SSE 中的凭据先解码再过滤。所有处理只影响诊断副本，真实响应仍使用 32 MiB 完整响应和 1 MiB 单条 SSE 行的独立业务上限。

`body_status` 为 `disabled/captured/truncated/invalid`。`body_metadata` 按 `request/raw_response/response_text` 保存 `original_bytes`、`stored_bytes`、`truncated` 和 `invalid`；`audit_truncated` 汇总字段截断状态。预览不可用作完整请求重放。

## 详细正文采集范围

在后台「诊断日志 → 日志策略」中开启详细模型日志后，才显示以下采集范围；点击「保存日志策略」后生效。不开自动关闭计时器，关闭开关会隐藏并保留已配置范围。判定规则与模型服务在「全局设置」的独立小 Tab 保存，日志策略在诊断日志区域单独保存，二者不会互相提交未保存修改。

| 范围 | 采集对象 |
| --- | --- |
| `all`（默认） | 全部模型调用 |
| `user` | 搜索选择的一名用户，包含叙事、后台 Memory、角色询问及该用户发起的模型诊断 |
| `conversation` | 指定对话 ID 的叙事、后台 Memory 和角色询问；无对话归属的模型诊断只留摘要 |

`detailed_llm_logging_enabled` 默认关闭；`detailed_llm_logging_scope` 默认 `all`；`detailed_llm_logging_target_id` 在全部范围为 `null`，指定范围为非零 UUID。切换范围类型会清除旧目标，必须重新选择或填写；新目标及重新开启时校验用户存在或对话存在且未删除。目标被删除不会扩大范围，也不妨碍关闭开关。

服务端在逻辑调用开始时读取设置快照，按可信调用参数中的玩家/对话归属判断，先判断后采集。Memory 使用持久化任务来源的玩家归属，不依赖 HTTP 登录上下文；玩家归属只用于内部判断，不随模型请求发给供应商或 Relay。服务端与客户端直连共用规则，指定范围下缺少相应归属时不采集正文；配置读取失败也不采集。设置变化不回溯在途调用或已有日志；同一实例保存后更新缓存，其他实例最多受 5 秒缓存有效期影响，后续工具步骤重新判断。

范围只控制模型正文，不影响摘要、上下文元数据、配置审计和日志清理。范围外的 `body_status=disabled` 表示未采集正文。修改范围和目标 ID 进入现有事务审计，只记录 ID，不追加用户名称或邮箱；无自动关闭字段或任务。

## 队列与关闭

每实例最多 128 条、32 MiB 待处理估算载荷，包含正在落库的记录。接受时先预留预算，再复制字段；完成或失败都释放预算。超单条/条数/字节预算立即拒绝诊断，计入 `record_limit/queue_limit/byte_limit`；关闭后的提交计入 `closed`，不阻塞游戏流程。

每条写入有 5 秒 context 超时。状态包括 pending、bytes、written、failed、按原因统计的 dropped、last_failure_at、closed 和 unflushed。丢弃及失败告警每分钟有界汇总，writer 的故障不会递归写回队列。实现方必须响应 context；超时不能强制终止忽略取消的 Go 函数。

正常关闭先禁止新的 Hub 生成、取消并等待活动任务、断开 Relay、停止 HTTP 与 Memory，再关闭 writer 并最多等待 10 秒排空。生产者等待也有时限，超时记录告警；writer 关闭超时保存未排空数量。进程崩溃或强制退出不保证保留诊断。

## 检索 API 和后台

`/admin/logs` 内包含模型调用、上下文构建、管理审计和诊断状态。

| API | 专用筛选 |
| --- | --- |
| `GET /api/admin/llm-request-logs` | `conversation_id/attempt_id/trace_id/purpose/model/success` |
| `GET /api/admin/context-build-logs` | `conversation_id/attempt_id` |
| `GET /api/admin/audit-logs` | `actor_id/action` |

三个列表都支持 `from`（含）、`until`（不含）、`limit`（默认 50、1–200）和 `cursor`。时间是 RFC3339，可带小数秒；模型/动作等字符串使用精确匹配。列表返回：

```json
{"logs":[],"next_cursor":"","has_more":false}
```

按 `(created_at DESC,id DESC)` 翻页，游标保存原始时间精度，并绑定日志种类和筛选条件；更改筛选重新查询第一页。非法游标返回 400。不在每页全表 COUNT，不用深 OFFSET。模型列表不加载正文，`/:id` 详情才返回内容。读取失败、空列表、未采集正文、截断和详情 404 分别提示；诊断详情 404 可能是已按策略清理。

`GET /api/admin/diagnostic-status` 返回 `{instance_id,writer,cleanup}`。只读取进程内状态，不扫描日志表。多实例请求可能落在不同实例，累计计数重启归零。清理状态有最近执行时间、耗时、部分删除数量、limit_reached、has_more、backlog_known 和安全错误；尚未执行与确认无积压不同。

## 保留、索引与配置事务

`diagnostic_log_cleanup_enabled` 默认开启，`diagnostic_log_retention_days` 默认 30，范围 1–3650。每轮在约 45 秒内最多执行 20 个批次，每批分别从模型和上下文表删除至多 500 条。按最旧 `(created_at,id)` 选行并 `SKIP LOCKED`，通过 `EXISTS` 判断积压；仍有积压或出错时至少冷却 1 分钟，无积压时每小时调度。

只清理模型/上下文诊断，不删除奖励账本、兑换订单、管理员审计、分享治理和 Campaign 事件。普通 DELETE 与 autovacuum 使空间可复用，不保证数据库文件立即缩小；不在线执行 VACUUM FULL。

`llm_request_logs` 增加 trace、protocol、started_at、transport_attempts、usage、error_code、body_status 和 body_metadata；`admin_audit_logs` 增加独立的 trace_id。补充模型 trace/attempt、上下文全局时间/attempt、管理审计全局时间/actor 索引。全局游标与清理使用 `(created_at,id)`。

管理员修改全局玩法/日志/平台模型或认证/SMTP 设置时，配置与 `system_settings_change/auth_settings_change` 审计在同一事务保存。先锁定当前配置行，再按字段允许列表计算差异；API Key、SMTP 凭据和私有端点仅记录 set/replace/clear，不记录原文、密文或片段。

仓储比较调用方读取的 UpdatedAt 与锁定行，拒绝过时的并发写入，返回 `409/settings_conflict`，不自动合并。系统设置缓存只在提交后更新，冲突时清除本实例旧缓存；Relay 关闭在成功提交后执行。无变化不生成审计。内部初始化不伪造管理员身份。现有 owner/admin 权限边界、奖励 request_id 唯一约束保持不变。

新项目的 schema 修改仅在 `backend/migrations/00001_init.sql`。已执行旧版基线的库不会因为修改文件而更新；使用测试工具自动创建的独立 schema 验证完整基线，禁止篡改 Goose 记录或自动清库。

## LLM 出站代理诊断

代理配置与 `llm_proxy_change` 审计在同一事务保存，记录操作者、时间、修订、启用状态、地址、用户名及密码设置/替换/清除。密码值、密文和 `Proxy-Authorization` 永不记录；CA 只记录是否配置和 SHA-256 指纹，不写完整 PEM。保存失败或修订冲突不发布运行快照，也不产生误导性成功审计。

- 普通玩家的 HTTP/WS/问答/Memory 错误统一脱敏：不出现代理地址、用户名、密码、CA 原文或底层 CONNECT 文本。模型列表失败仍是 `502/upstream_error`；未知 500 仍返回 trace。
- 服务端出站错误由 `llmoutbound.Error` 归类为固定代码：`proxy_unreachable`、`proxy_auth_failed`、`target_unreachable`、`tls_certificate_failed`、`timeout`、`config_invalid`、`outbound_error`。`SafeLogMessage` 只输出 `llm_outbound <code>`；底层 `url.Error` 仅保留为错误链原因，不进入日志正文。
- 代理认证拒绝、明确证书错误和配置错误不重试；临时不可达/超时继续使用原有有限传输重试。Memory 任务对临时出站故障可后台退避，对认证、TLS、配置错误不重试。
- “测试连接”只使用已保存配置，不调生成接口、不带供应商凭据，不跟随重定向，10 秒截止，发送一次 `HEAD`。目标返回 401/403/405 仍显示网络通路可达和目标状态码；网络失败返回结构化错误码。结果带配置修订，保存并发变化时标记 `stale`，不会冒充当前配置已验证。

2026-10-21 本地功能验证：

- 后端 `go test ./...`、`go vet ./...` 通过；前端 `node --experimental-strip-types --test tests/*.test.mjs` 的 79 项测试及 `npm run build`（含类型检查）通过；本次变更文件的 Prettier 检查和 `git diff --check` 通过。全项目 `npm run format:check` 另报四处既有格式问题：`src/stores/auth.ts`、`src/types/auth.ts`、`src/views/LoginView.vue`、`src/views/SettingsView.vue`，本次未修改它们。
- 本机模拟代理/模型覆盖：直连忽略环境代理、Basic Auth、HTTP/HTTPS 代理、HTTPS CONNECT 与 407 分类、CA、凭据不传给目标、`NO_PROXY` 不绕过、两协议的流式/非流式 Agent、两处模型列表、活动流完成后清理旧池、同一调用重试固定出口及下一次调用切换出口。
- 保存与权限覆盖：密码保留/替换/清除、清除可撤销、保存失败不发布、并发修订冲突、损坏密文启动失败、无需解密旧密码即可停用代理、owner 权限、必填修订号、通用配置 JSON 脱敏、Memory 临时/永久错误退避边界；前端验证初次读取失败重试、防重复提交和卸载后忽略迟到结果。
- PostgreSQL 事务测试已编写，包含代理与普通设置互不覆盖（数据库和同仓储缓存）、修订冲突及审计脱敏；本机未配置 `FATECRAFT_TEST_DATABASE_URL`，数据库集成测试实际跳过，不能算作已验证。未做性能测试、真实供应商请求或外部代理/容器验收；部署仍需确认容器内代理可达、CA 可用且 `AUTH_DATA_ENCRYPTION_KEY` 稳定。

## Docker 日志

两个 Compose 文件的所有服务均使用 `json-file`，`max-size=10m`、`max-file=3`。这是每容器约 30 MiB 的轮转预算，多服务/副本须合计；不限制数据库和备份文件。备份 cron 输出到容器 stdout/stderr，由 Docker 轮转，不再追加 `backups/backup.log`；已有历史文件不会自动删除。

## 验证方式

本地执行 `task backend:test`、`task frontend:check`、`task frontend:build`、`task backend:build`。日志回归覆盖秘密过滤、500 关联、JSON/UTF-8 边界、摘要与重试、预算、关闭竞争和游标绑定；并发代码还应在具备 C 工具链的环境执行 `go test -race`。

配置专用 `FATECRAFT_TEST_DATABASE_URL` 后运行 `task backend:test:postgres`，测试自动建删随机隔离 schema 并执行完整 00001。新增数据库测试验证实际 JSONB/text、同时间戳分页、SKIP LOCKED 清理、审计与配置回滚以及缓存冲突；测试被跳过不算通过。

### 已完成的本地检查

2026-09-29 的检查结果如下，真实环境测试不计入这些通过项：

| 检查 | 结果 |
| --- | --- |
| `go test ./...`、`go vet ./...` | 通过；PostgreSQL 用例因未配置可用测试 URL 而跳过 |
| 前端类型检查、生产构建、变更文件 Prettier | 通过 |
| Linux amd64 服务与迁移工具构建 | 通过 |
| 开发/生产 Compose YAML 解析 | 通过；分别 4/5 个服务均配置 json-file、10m × 3，未验证运行容器 |
| Go 格式、文档链接和 `git diff --check` | 通过 |

2026-09-30 增加详细正文采集范围后，重新通过 `go test ./...`、`go vet ./...`、前端类型检查与生产构建、变更文件 Prettier、Linux amd64 服务及迁移工具构建、文档链接与差异检查。新增回归覆盖范围匹配/缺失归属、范围外仅摘要、服务端与 Relay、调用中修改配置、后台 Memory/模型诊断归属、PATCH 校验和缓存目标隔离。PostgreSQL 范围持久化、目标存在性、审计与 CHECK 约束用例已添加，随数据库测试跳过，仍待维护者真实执行。

2026-10-20 完成对话请求精简与有限重连后，前端全部 75 项功能测试（活动、资源调度、设置/奖励去重、行动、奖励、结局和玩法就绪）通过，`vue-tsc --noEmit` 与 Vite 生产构建通过。功能脚本验证：首连之外最多 5 次自动重连、第 5 次成功及稳定 30 秒后预算归零、耗尽后写消息返回拒绝、耗尽后不再新建连接、同资源单飞与在途补读一次、调度器隐藏 actions 零请求、暂停上下文阻止读取、设置缓存/强制检查及账号切换丢弃迟到响应。审查回归另覆盖：隐藏/离线不消耗重连预算、握手期间不重复建连、旧对话定位的迟到鉴权错误隔离、业务恢复失败 5 轮后停止、补偿成功后恢复稳定计时、检查器在途失效补读、首屏结局等待对话详情、Campaign 在途快照与后到正文合并、旧读取不得写回，以及玩法校验等待期间的连接耗尽、切换对话、连点与暂停版本拦截。该轮未运行性能测试、A/B 或 EXPLAIN 对比，不能据此给出 CPU、P95 或容量数字。

本地摘要微基准：Windows amd64、i5-14600KF，无网络/数据库，单个轻量成功摘要约 4.35 μs/op、1848 B/op、估算队列载荷 4189 字节/条。估算含 4096 字节保守记账开销，不是 PostgreSQL 行大小，也不代表生产吞吐。

### 维护者真实环境验收清单

以下项目于 2026-09-30 交接维护者执行，尚无实测结果。测试数据放在专用测试环境；数据库测试工具创建并清理随机隔离 schema，不修改已有业务 schema 或 Goose 版本。执行过旧版 00001 的库不会自动获得此次基线字段，不能直接用它代替全新 schema 验收。

- [ ] **数据库与业务回归**：设置 `FATECRAFT_TEST_DATABASE_URL` 后运行 `task backend:test:postgres`，确认完整 00001、JSONB/text 边界、权限、发奖/撤回/兑换/结算和客户端直连用例真实执行，无数据库测试跳过。
- [ ] **分页**：同一时间戳多条记录逐页读取不重不漏；新增或清理记录后继续翻页；非法游标、筛选变化、越权访问被拒绝。
- [ ] **配置审计**：配置和脱敏审计同时提交；强制审计失败时配置与缓存不提前改变、Relay 不关闭；并发旧版本返回 409，刷新后可重试。
- [ ] **采集范围**：分别保存全部、指定用户、指定对话；叙事、Memory、角色询问和模型诊断按归属采集，服务端/客户端直连行为一致，范围外只留摘要。检查无效/已删除目标、关闭后保留范围、配置审计及多实例缓存生效时间，无自动关闭。
- [ ] **清理**：只删除过期模型/上下文诊断，保留新记录和业务审计；检查部分失败、批次/时间上限、积压后的冷却追赶及多实例 SKIP LOCKED。
- [ ] **浏览器**：验证筛选、加载更多、空列表、读取失败、详情已清理，以及关闭/开启正文和截断状态；复现业务拒绝、内部故障与模型重试，按关联 ID 查到对应事件，确认同一次逻辑调用只有一条最终摘要。
- [ ] **Nginx 脱敏**：用合成秘密测试 `/s/`、`/shared/`、公共分享 API、OAuth 回调、敏感查询、未匹配/编码路径，以及上游不可用时的 access/error 输出；确认无原始秘密，并保留安全失败摘要和可诊断的启动错误。
- [ ] **容器与备份**：全新测试部署在迁移成功后启动服务；通过 `docker inspect` 确认 LogConfig，实际触发日志轮转，检查备份成功/失败与调度器输出进入 stdout/stderr，备份文件保留策略正常。
- [ ] **负载与关闭**：用代表性小/大请求记录摘要/详细记录大小、队列峰值和清理耗时；确认 128 条/32 MiB 预算、丢弃原因计数、失败释放、正常关闭排空和超时未排空统计。记录并发数、正文开关、负载大小与环境，不能仅引用上述微基准。
- [ ] **Race**：在具备 C 编译器的 Go 环境启用 cgo，运行下述并发检查；此前本机缺少 gcc，尚未运行。

在仓库根目录执行（先在当前进程配置专用测试库 URL）：

```powershell
task backend:test:postgres
$env:CGO_ENABLED = '1'
go -C backend test -race ./internal/logging ./internal/llm ./internal/realtime ./internal/storage
```

完成后记录测试日期、Git 版本、环境、执行命令及失败/跳过项，再更新本清单。实际能力与维护规则以本文和相关正式设计文档为准。


### 活动对账诊断边界

活动发布器记录快照读取失败并关闭受影响对话订阅；数据库 LISTEN 缺口关闭所有本地订阅以恢复。健康空闲零周期读取与 CPU 降幅是不同指标：功能测试已覆盖前者，后者仍需在 2 核/2.5GiB Beta 环境实测 SQL、CPU、P95、触发器和锁成本。尚未新增完整的按恢复原因聚合指标；测试记录不能替代线上容量数据。

NOTIFY 无 durable replay，健康空闲且没有后续触发的静默末条业务事件丢失没有确定恢复时限。普通 ping/pong 仅检测连接，已知任务 watchdog 只核验任务。客户端最多自动重连 5 次，耗尽后停止自动兜底并提示刷新；该边界见 [05 前端设计](05-frontend-vue.md#活动恢复协调)。

### 模型诊断写入与业务行锁

异步日志有独立事务，但 `llm_request_logs` 的外键仍会对 conversation、attempt、memory job、memory 父行取得 KEY SHARE 锁。异步不等于锁隔离；若业务同时用 FOR UPDATE 锁定多个父行，交叉取得锁可能造成死锁，且被 PostgreSQL 中止的可能是业务事务。

生成写围栏 `AssertGenerationActive` 先单独对 conversation 取得 FOR NO KEY UPDATE 锁，再校验并锁定 run、attempt、turn，避免联合查询执行计划改变“先对话”的顺序。FOR NO KEY UPDATE 与外键 KEY SHARE 兼容，仍阻止业务更新、删除与生命周期变更。围栏和业务写入必须在同一事务内；等待锁后重新检查状态、分支和生成归属，不能依靠锁前读到的状态授权。

诊断插入在同一短事务内，对所有非空外键父行逐个执行 FOR KEY SHARE NOWAIT，再实际插入，保留数据库外键约束。遇到父行繁忙立即回滚，释放本次已取得的全部锁；只对 55P03 / 40P01 最多尝试 3 次，间隔 20 / 40 毫秒，退避期间不保留本次事务连接或父行锁。最终失败沿用异步 writer 的 Failed 计数及限频告警，不把错误返回生成任务。重试仅覆盖诊断事务，不重放模型调用或业务工具。父行已不存在则拒绝该条诊断，不插入悬空引用。异步 writer 的单条 5 秒上下文预算继续生效。

真实 PostgreSQL 回归覆盖 run / attempt 围栏与外键锁双向兼容、受保护行仍排斥其他业务写锁、步骤创建与完成、四类繁忙父行的锁释放和失败回滚、锁释放后日志恢复、缺失父行拒绝，以及取消与围栏并发时的重新校验。修复前实现会在同一外键锁回归中超时，修复后通过。此验证针对并发正确性，不是性能或容量测试；无需修改 schema。

2026-10-19 本地验证：使用独立 PostgreSQL，强制启用数据库集成测试，`go test -p 2 ./... -count=1 -timeout=10m` 全量通过；`go vet ./...` 通过。未执行性能测试，未操作项目实际数据库。
