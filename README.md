# Fatecraft

Fatecraft 是一个由版本化玩法驱动的 AI 文字冒险平台。它把角色、场景、世界设定、状态规则、行动能力、结局和奖励组织成可以发布的完整玩法，而不是只有一段系统提示词的通用聊天界面。

模型负责理解玩家意图和讲述故事，后端负责状态变化、随机判定、资源消耗、结局验证与奖励结算。故事可以自由表达，但会始终运行在玩法规则和可信存档之上。

## 玩法体验

玩家先选择一个已发布的玩法，再选择身份与开局场景。每次创建的对话都是一份独立存档，角色状态、历史事件、长期记忆和结算结果不会与其他对话混用。

一轮游戏大致按以下方式进行：

1. 玩家使用自然语言描述行动，不必局限于预设选项。
2. 叙事模型结合当前角色、场景、世界书、历史和存档状态继续故事。
3. 涉及判定、资源或世界变化时，模型调用当前玩法允许的动作，由后端校验并执行。
4. 结果写回存档并实时展示，随后进入下一轮叙事。

项目支持两类玩法：

- **单局挑战**：围绕明确目标、失败条件和结算奖励展开，适合节奏紧凑、可以反复游玩的独立冒险。
- **长期 Campaign**：持续维护地点、角色、物品、任务、市场、势力和世界事件，适合不断成长和演化的长期存档。

长期 Campaign 不限定修仙题材。服务端提供 `campaign.generic@1` 作为无境界的通用长期世界引擎，玩法可以围绕调查、经营、交易、势力、NPC、任务和地点探索组织自己的规则；修炼、功法和境界等机制仍由独立的 `cultivation.campaign@1` 引擎提供。玩法包只能组合服务端已经安装并审核过的引擎、状态规则和动作，不能通过配置上传或执行任意代码。

长期对话会在后台整理记忆，减少历史增长对当前回复的影响。角色栏还可以基于当前存档回答状态、任务和已发生事件，但不会借问答修改游戏事实。

## 内置玩法

仓库目前提供两种运行方式不同、但共享修仙基础设定的官方玩法。

### 青河修仙志

长期 Campaign。玩家从青河村起步，可以探索不同区域、修炼功法、生产和交易物品、接受任务并与 NPC 往来。角色、势力、市场和区域局势会随着行动持续演化，形成一份可以长期推进的修仙存档。

详细规则见 [Campaign Agent 参考](docs/design/16-cultivation-campaign-agent-reference.md)。

### 百年飞升劫

单局挑战。玩家在寿元压力下积累资源、逐级突破，并选择离场路线：

- 正统渡劫：至少化神期，完成九波雷劫，成功后按局内灵石的 3 倍结算。
- 虚空裂缝：至少元婴期，发现裂缝并通过穿越判定，成功后按 2 倍结算。
- 仙缘传送：至少结丹期并取得飞升符，成功后按 1.5 倍结算。
- 主动坐化是 <code>neutral</code> 结局，寿元耗尽是 <code>failure</code> 结局，二者都不发放挑战奖励。

详细规则见 [单局玩法规格](docs/design/14-ascension-trial-gameplay.md)。

两个玩法共享凡人、练气、筑基、结丹、元婴、化神、炼虚、合体、大乘的九阶设定，以及灵根、功法、法宝、丹药和灵石等基础语义；各自的角色、任务、事件、判定、结局和存档彼此独立。设定边界见 [内置修仙玩法共享设定](docs/design/20-cultivation-shared-setting.md)。

## 平台能力

- **玩法版本管理**：管理员可以编辑和发布角色、身份、场景、Prompt、世界书、状态规则、结局与奖励；已经开始的存档始终固定在创建时的玩法版本。
- **自由叙事与可信行动**：玩家可以自由输入，模型只负责理解和叙事，所有会影响存档的动作仍由后端确认。
- **独立存档与长期记忆**：消息、状态、回合、判定、记忆和问答按对话隔离，长期历史由后台异步整理。
- **角色状态与只读问答**：玩家可以查看当前状态，并询问任务、物品和已经发生的事件。
- **结局与账号奖励**：玩法可以定义成功、失败或中性结局，以及对应的账号奖励规则。
- **模型配置与能力检测**：支持用户自备模型和管理员统一模型，可分别配置叙事、记忆与角色问答模型。
- **对话分享**：从对话页头把完整当前分支或一段连续消息发布为可撤销、可过期的只读快照，可选择匿名或显示账号名称与头像；“我的分享”集中管理所有对话的分享记录。
- **认证与运营**：支持 LinuxDo Connect、邮箱登录与注册控制，并提供内容管理和诊断日志。

## 实现概览

Fatecraft 将生成过程拆成可追踪的回合和模型步骤。模型可以返回玩家可见文本，也可以请求调用当前步骤开放的工具；工具参数经过后端重新校验，并在事务中提交状态变化。

```text
玩家输入
  -> Conversation Turn / Generation Attempt
  -> 装配版本化 Prompt、世界书、历史、记忆与当前状态
  -> 主叙事模型步骤
       -> 普通文本：协议检查后写入消息
       -> 工具调用：后端校验、事务提交、回填结果，再进入下一模型步骤
  -> WebSocket 推送玩家可见事件
  -> 回合封口并异步调度长期记忆
```

PostgreSQL 和后端领域服务是玩法事实的权威来源。浏览器、玩家文本、模型输出和 OpenAI 兼容网关都按不可信输入处理，模型不能仅凭一段叙事修改状态、指定随机结果或发放奖励。

## 技术栈

| 层级     | 主要技术                                                                     |
| -------- | ---------------------------------------------------------------------------- |
| 前端     | Vue 3、Vite、TypeScript、Pinia、Naive UI                                     |
| API      | Go 1.25、Gin、GORM                                                           |
| 实时通信 | WebSocket                                                                    |
| 数据库   | PostgreSQL，官方 Docker 配置使用 PostgreSQL 18                               |
| LLM      | OpenAI 兼容 Chat Completions / Responses、Function Calling、原生 JSON Schema |
| 部署     | Go Task、Docker Compose、Nginx、PostgreSQL 备份 sidecar                      |

## 安装与使用

### 环境要求

- Go 1.25。
- Node.js 20+ 和 npm。
- 一个可访问的 PostgreSQL 数据库；可以是本机、Docker 或其他独立数据库。
- 可选：Go Task，用于执行仓库根目录的快捷任务。

### 1. 初始化数据库

Fatecraft 不使用 AutoMigrate，也没有旧表兼容或自动迁移旁路。空数据库必须完整执行一次 [init.sql](backend/scripts/sql/init.sql)：

```bash
psql "$DATABASE_URL" -f backend/scripts/sql/init.sql
```

没有本机 <code>psql</code> 时，可以使用 Navicat、DBeaver 等数据库管理工具连接目标数据库并执行同一脚本。

请勿对已有 Fatecraft schema 重复执行 <code>init.sql</code>。结构升级必须显式维护 SQL 和数据迁移，而不是依赖服务启动时猜测数据库状态。

### 2. 配置后端

复制示例配置：

```bash
cp backend/.env.example backend/.env
```

至少确认以下项目：

```env
PORT=8081
DATABASE_URL=postgres://user:password@host:5432/fatecraft?sslmode=disable
DATA_DIR=data
AUTH_DATA_ENCRYPTION_KEY=replace-with-a-stable-secret-at-least-32-characters
SHARE_LINK_SIGNING_KEY=replace-with-an-independent-stable-secret-at-least-32-characters
```

常用环境变量：

| 变量                                             | 说明                                                                                                     |
| ------------------------------------------------ | -------------------------------------------------------------------------------------------------------- |
| <code>DATABASE_URL</code>                        | PostgreSQL 连接串，支持外部数据库                                                                        |
| <code>DB_MAX_OPEN_CONNS</code>                   | 单个后端实例最大数据库连接数，默认 10；多实例部署时需汇总计算                                            |
| <code>DB_MAX_IDLE_CONNS</code>                   | 单个后端实例最大空闲数据库连接数，默认 3                                                                 |
| <code>DB_CONN_MAX_LIFETIME</code>                | 数据库连接最长复用时间，默认 `30m`                                                                       |
| <code>DB_CONN_MAX_IDLE_TIME</code>               | 数据库空闲连接回收时间，默认 `5m`                                                                        |
| <code>DATA_DIR</code>                            | <code>roll_config.yaml</code> 与内置玩法材料目录；从 <code>backend</code> 启动时默认是 <code>data</code> |
| <code>AUTH_DATA_ENCRYPTION_KEY</code>            | 加密 LLM API Key 和 SMTP 密码；至少 32 字符，投入使用后不得随意更换                                      |
| <code>SHARE_LINK_SIGNING_KEY</code>              | 签发公开分享链接；至少 32 字符，必须与数据加密密钥独立，轮换会让既有链接失效                              |
| <code>ALLOWED_ORIGINS</code>                     | 允许访问 API 与 WebSocket 的前端来源，生产环境必须明确配置                                               |
| <code>TRUSTED_PROXIES</code>                     | 允许设置 <code>X-Forwarded-For</code> 的反向代理 IP/CIDR；留空时不信任任何代理，后端端口不得对公网开放 |
| <code>AUTH_COOKIE_SECURE</code>                  | HTTPS 部署时设为 <code>true</code>                                                                       |
| <code>AUTH_SESSION_TTL</code>                    | 登录会话有效期，默认示例为 <code>336h</code>                                                             |
| <code>LINUXDO_CLIENT_ID</code>                   | LinuxDo Connect 客户端 ID                                                                                |
| <code>LINUXDO_CLIENT_SECRET</code>               | LinuxDo Connect 客户端密钥                                                                               |
| <code>LINUXDO_REDIRECT_URI</code>                | OAuth 回调地址，必须与服务商后台完全一致                                                                 |
| <code>LOG_LEVEL</code> / <code>LOG_FORMAT</code> | 日志级别与 <code>console</code> / <code>json</code> 格式                                                 |
| <code>CHALLENGE_CACHE_MAX_VERSIONS</code>        | 每个后端进程最多驻留的已发布/退役挑战版本，范围 1-64，默认 16                                           |
| <code>CHALLENGE_CACHE_MAX_MIB</code>             | 只读挑战内容缓存估算内存上限，范围 16-1024 MiB，默认 256；2C4G 建议保持默认值                            |

完整注释见 [backend/.env.example](backend/.env.example)。

### 3. 启动后端

```bash
cd backend
go run ./cmd/server
```

健康检查：

```text
GET http://localhost:8081/api/health
```

后端发现 <code>challenge_packs</code> 完全为空时，会在事务中读取 <code>backend/data/preset_packs/*</code>，校验并发布官方玩法。多个实例同时首次启动时通过 PostgreSQL advisory lock 串行初始化。

只要数据库已经存在任意挑战包，启动过程就完全跳过内置内容初始化，不自动升级或覆盖数据库内容。

运行时会在每个后端进程内缓存已发布/退役版本的 Manifest、Runtime、Prompt、世界书和玩法定义编译结果。PostgreSQL 仍是权威来源，草稿和玩家动态数据不缓存；多实例不要求 Redis，各实例独立受上述双重上限约束。回合运行面板可查看聚合命中率、驻留版本和估算内存。

### 4. 启动前端

另开终端：

```bash
cd frontend
npm ci
npm run dev
```

默认地址：

- 前端：<code>http://localhost:5173</code>
- 后端：<code>http://localhost:8081</code>

Vite 开发服务器会把 <code>/api</code> 和 <code>/ws</code> 代理到后端。

生产 Nginx 还会把公开入口 <code>/s/:token</code> 转给后端生成 Open Graph 摘要，再进入前端只读页面。公开响应不使用代理共享缓存，撤销、过期、对话删除和管理员下架会在下一次读取时立即生效。

### 5. 完成首次配置

空数据库首次打开时会进入网页安装向导。部署者直接在向导中设置本地用户名和密码，并可选填写邮箱作为联系地址；明确确认后该账号成为唯一的 `owner`。用户名是正式登录标识，有邮箱时邮箱也可用于登录。安装完成前邮箱和 LinuxDo 登录不会开放，完成后安装入口关闭，后续用户一律默认为普通用户。LinuxDo 只能在已有本地账号的设置中绑定，不能通过 OAuth 自动创建站内用户；管理员可在“管理 -> 第三方账户”审计绑定关系。`owner` 可在“管理 -> 用户与权限”中授予或撤销 `admin`。

登录后需要完成以下配置：

1. 在“设置”中填写用户自己的 LLM Base URL、API Key、协议与叙事模型；或者由管理员启用平台统一模型。
2. 分别检测叙事模型的 Agent Tool Loop 能力，以及长期记忆和角色问答模型的原生 JSON Schema 能力。
3. 创建对话并确认 WebSocket、模型输出和状态面板正常工作。

## LLM 配置

系统提供两种访问模式：

- <code>user_managed</code>：每个用户维护自己的 LLM 设置。
- <code>platform_managed</code>：管理员统一提供公共设置，普通用户只读使用。

API Key 使用 <code>AUTH_DATA_ENCRYPTION_KEY</code> 经 AES-GCM 加密后写入数据库，运行时才在内存中解密；API 响应和诊断日志不会返回密钥。

### 模型角色

| 角色         | 用途                                 | 模型留空行为 | 思考强度留空行为                     |
| ------------ | ------------------------------------ | ------------ | ------------------------------------ |
| 叙事模型     | 流式叙事与可信工具循环               | 不允许留空   | 完全不发送思考强度字段               |
| 长期记忆模型 | 后台异步归档，原生 JSON Schema       | 继承叙事模型 | 继承叙事思考强度；叙事也为空则不发送 |
| 角色问答模型 | 当前存档的只读问答，原生 JSON Schema | 继承叙事模型 | 继承叙事思考强度；叙事也为空则不发送 |

思考强度提供 <code>默认（不传）</code>、<code>low</code>、<code>medium</code>、<code>high</code> 候选，也允许直接输入供应商支持的自定义档位。不同供应商没有统一的思考强度元数据；“获取模型列表”只读取兼容接口的 <code>/models</code>，不能可靠推断每个模型支持哪些档位。

能力检测是一次显式、无副作用的真实请求：

- 叙事模型必须完成“调用诊断工具 -> 接收工具结果 -> 返回普通文本”的两步循环。
- 长期记忆和角色问答模型必须返回符合原生 JSON Schema 的随机 nonce。
- 检测不会自动切换模型、重试请求、记录健康分数或改变生产请求行为。

## Agent 与信任边界

主叙事模型每一步只能选择普通文本或当前工具快照中的真实工具。工具可能来自：

- 通用状态白名单。
- 服务端判定系统。
- 挑战结局验证。
- 当前 Campaign revision 投影出的动态动作。

后端会重新检查工具是否仍可用、参数是否符合收窄后的 Schema、资源与前置条件是否满足、revision 是否过期，并在事务中写入结果。模型不能直接提交随机概率、骰面、账号余额、奖励数量或伪造的领域 revision。

单个模型步骤不会因解析、工具或结构化契约错误而自动重放，不更换协议、不切换模型，也不会把原生 JSON Schema 静默降级为 Prompt-only JSON。只有在响应体交付前遇到连接错误、429 或网关临时错误时，传输层才按有限策略重试；失败会以明确的回合、attempt、步骤和错误码留痕。

完整协议见 [Agent Tool Loop](docs/design/19-agent-tool-loop.md) 和 [AI 动作协议](docs/design/12-ai-actions.md)。

## 内容与发布

PostgreSQL 是运行时玩法内容的唯一权威。Character、Persona、Scenario、Prompt、WorldBook、Runtime Contract、结局和奖励规则都属于一个 <code>challenge_version</code>。

管理员在“管理 -> 挑战内容”中：

1. 创建玩法包和首个草稿，或从当前发布版本复制新草稿。
2. 分区编辑基本信息、入口资产、Runtime Contract、状态 Schema、玩家展示、outcome verifier 和奖励 calculator。
3. 保存草稿并执行“校验并发布”。

发布过程会在同一事务内锁定草稿、校验资源引用和已安装模块、计算稳定 SHA-256 哈希，并切换玩法包的当前版本。已发布版本不可原地修改，新版本不会改变已经开始的对话。

仓库玩法包格式为：

```text
pack.json
runtime/
  runtime.json
  state-schema.json
  actions.json
  settlement.json
  presentation.json
rewards.json
characters/
personas/
scenarios/
prompts/
worldbooks/
```

仓库 JSON 只负责空库首次初始化和管理员显式导入，不是运行时可写存储。已有数据库中的官方玩法更新必须由管理员在“内置内容”页面检查版本与哈希后显式导入或覆盖。

玩法制作说明见 [挑战制作指南](docs/design/15-challenge-authoring-guide.md)。

## 认证与安全

- 用户角色为 `owner`、`admin`、`user`；普通注册创建 `user`，LinuxDo 只登录已绑定的本地账号，不自动创建站内用户。
- 空库必须通过 `/setup` 首次安装向导明确设置唯一 `owner`，不能由第一个访问者自动获得管理员权限。
- `owner` 可以管理 `admin`、认证策略和平台密钥；`admin` 负责日常运营，不能修改或转移 `owner`。
- 初始化完成后必须始终保留唯一的 `owner`，后端事务阻止其被禁用、删除或降级。
- 服务端会话使用随机令牌，数据库只保存令牌哈希。
- LinuxDo Connect 使用 OAuth 2.0 + PKCE；绑定入口位于账号设置的“第三方账户”，绑定和解绑均由服务端校验。
- 邮箱注册、验证码、邀请码和 SMTP 由管理员配置；邮箱注册默认关闭。
- 验证码只保存 bcrypt 摘要，限制尝试次数且只能消费一次。
- 邀请码数据库只保存 SHA-256 摘要，原文仅在创建后展示一次。
- LLM API Key 和 SMTP 密码必须加密保存。

LinuxDo 回调地址示例：

```text
本地：http://localhost:5173/api/auth/linuxdo/callback
生产：https://example.com/api/auth/linuxdo/callback
```

生产环境必须：

- 使用稳定且妥善备份的 <code>AUTH_DATA_ENCRYPTION_KEY</code>。
- 设置明确的 <code>ALLOWED_ORIGINS</code>。
- 在 HTTPS 下启用 <code>AUTH_COOKIE_SECURE=true</code>。
- 不提交真实的 <code>.env</code>、API Key、SMTP 密码或数据库密码。

## 诊断日志

管理员可以开启详细 LLM 日志。开启后，<code>llm_request_logs</code> 会保存请求体、供应商原始响应、解析后的文本、状态码、耗时、模型、协议结果和关联的 conversation / attempt / memory / inquiry 标识；认证头和 API Key 不会写入。

详细日志默认关闭，因为其中仍可能包含技术 Prompt、世界书、玩家文本和模型推理载荷。生产环境应按故障排查需要临时开启。

<code>context_build_logs</code> 独立记录每次上下文装配的 token 预算、分段、选入消息、世界书命中和记忆覆盖情况。两个日志入口均仅对管理员开放。管理员可独立启用诊断日志自动清理并设置 1-3650 天保留期；默认启用并保留 30 天。清理每小时以有限批次执行，不处理分享治理审计记录。

## Docker

仓库 Docker 镜像不在容器内编译源码：后端镜像复制宿主机构建的 Linux amd64 二进制，前端镜像复制静态产物。

先生成产物：

```bash
task backend:build
task frontend:build
```

开发环境（后端、PostgreSQL、自动备份；前端仍可使用 Vite）：

```bash
docker compose -f docker/docker-compose.dev.yml up --build
task frontend:dev
```

生产环境：

```bash
docker compose -f docker/docker-compose.prod.yml up --build -d
```

生产前端入口默认为 <code>http://&lt;host&gt;:8088</code>，Nginx 同时反代 <code>/api</code> 和 <code>/ws</code>。备份 sidecar 默认按东八区每天 02:00 执行 <code>pg_dump</code>，保留 7 天。

端口、外部 PostgreSQL、数据卷、备份和恢复说明见 [Docker Workflow](docker/README.md)。

## 目录

```text
backend/
  cmd/server/               HTTP、WebSocket 与 Worker 组合根
  internal/api/             Gin 路由、权限与请求边界
  internal/auth/            登录、会话、验证码和邀请码
  internal/challenge/       玩法查询、版本发布、哈希与奖励结算
  internal/challengeimport/ 仓库玩法包校验、导入与空库初始化
  internal/contextbuilder/  上下文预算、世界书和 Campaign 投影
  internal/gameplay/        通用契约、注册表与可信题材模块
  internal/inquiry/         角色栏只读快照与问答
  internal/llm/             Chat / Responses、工具与 JSON Schema 适配
  internal/memory/          异步长期记忆调度、契约与 Worker
  internal/orchestration/   主叙事 Agent、工具目录和事务编排
  internal/storage/         PostgreSQL 模型、仓储和奖励账本
  scripts/sql/init.sql      空数据库权威 schema
  data/preset_packs/        官方玩法首次初始化材料
  data/roll_config.yaml     判定算法与词表
frontend/
  src/components/           对话、设置、状态和管理组件
  src/stores/               认证、玩法、对话、运行时和奖励状态
  src/views/                登录、玩法、对话、奖励、设置和管理页面
docker/                     Compose、Nginx、备份和镜像上下文
docs/design/                正式设计文档
```

## 验证

使用 Task：

```bash
task backend:test
task frontend:check
task frontend:build
```

或直接执行：

```bash
cd backend
go vet ./...
go test ./...
```

```bash
cd frontend
npm run format:check
npm run build
```

PostgreSQL 集成测试默认跳过。需要运行时，为测试进程提供专用、可丢弃的数据库：

```text
FATECRAFT_TEST_DATABASE_URL=postgres://...
```

真实供应商能力测试使用 <code>FATECRAFT_LIVE_LLM_BASE_URL</code>、<code>FATECRAFT_LIVE_LLM_API_KEY</code>、<code>FATECRAFT_LIVE_LLM_MODEL</code> 和可选的 <code>FATECRAFT_LIVE_LLM_PROTOCOL</code>，默认同样跳过。

## 设计文档

推荐阅读顺序：

1. [项目概览](docs/design/01-overview.md)
2. [产品能力](docs/design/03-product-features.md)
3. [挑战制作指南](docs/design/15-challenge-authoring-guide.md)
4. [通用 Campaign Action Capability](docs/design/17-generic-campaign-action-capabilities.md)
5. [异步长期记忆与只读角色问答](docs/design/18-async-memory-character-inquiry.md)
6. [Agent Tool Loop](docs/design/19-agent-tool-loop.md)
7. [Campaign 终局与奖励](docs/design/21-campaign-outcome-rewards.md)
8. [未来玩法方向](docs/design/future-gameplay-directions.md)

完整索引与权威来源优先级见 [设计文档目录](docs/design/README.md)。

## 当前边界

- 不向普通用户提供技术 Prompt、世界书、角色、场景或奖励规则 CRUD。
- 不允许对话创建后替换玩法版本或跨对话共享长期记忆。
- 不在已有数据库上自动修改 schema、导入新玩法版本或覆盖管理员内容。
- 不保证所有 OpenAI 兼容网关都正确实现 Function Calling、Responses 或原生 JSON Schema；请先执行能力检测。
- 不从 <code>/models</code> 推断模型的思考强度、上下文窗口或价格。
- 当前不提供通用插件市场、多人共享同一对话或跨玩法存档迁移。
