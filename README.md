# Fatecraft

Fatecraft 是一个版本化挑战驱动的 AI 文字冒险平台。玩家选择管理员已发布的挑战和允许的 Persona，在平台统一的 Prompt、世界书、判定和奖励规则下完成试炼；每个对话的状态、消息和记忆彼此隔离。

- 后端：Go、Gin、GORM、PostgreSQL、OpenAI 兼容 LLM、WebSocket。
- 前端：Vue 3、Vite、TypeScript、Pinia、Naive UI。
- 登录：LinuxDo Connect OAuth 2.0 + PKCE，以及默认关闭的邮箱登录/注册。

## 核心架构

PostgreSQL 是运行时挑战内容的唯一权威。角色卡、Persona、场景、Prompt、世界书和奖励规则都属于一个 `challenge_version`，发布后不可原地修改。新对话固定保存版本 ID 和内容哈希，因此管理员发布新版本不会改变已经开始的挑战。

普通用户只能：

- 查看已发布挑战及公开奖励概要。
- 选择挑战允许的 Persona。
- 创建和操作自己的对话。

管理员可以创建挑战包、复制草稿版本、编辑完整版本 JSON、执行发布校验，并统一设置判定开关和难度。Prompt、世界书、场景、角色和奖励规则没有普通用户读写 API。

成功结局只允许模型提交 `reward_key`。后端按对话固定版本读取结构化奖励规则，校验服务端 `roll_history`，计算奖励，并在同一 PostgreSQL 事务中更新账号、写入唯一 `challenge_run` 和结束对话。

## 内置玩法：飞升试炼

当前官方挑战是“飞升试炼”。玩家从凡人或低阶修士起步，在寿元耗尽前通过任务、探索、战斗、炼制和闭关积累修为与灵石。突破需要模型发起天命判定，由后端掷骰并保存真实流水；失败可能消耗资源、寿元或造成境界倒退。

一局的主要抉择是“继续提高境界争取更高奖励，还是尽早以较安全的路线离场”：

- 正统渡劫：至少化神期，完成九波雷劫，成功后按局内灵石的 3 倍结算。
- 虚空裂缝：至少元婴期，发现裂缝并通过穿越判定，成功后按 2 倍结算。
- 仙缘传送：至少结丹期并取得飞升符，风险最低，成功后按 1.5 倍结算。
- 主动坐化是 `neutral` 结局，寿元耗尽或陨落是 `bad` 结局，二者都不发放挑战奖励。

境界与寿元表、资源循环、完整路线条件、结构化状态、结算事务和当前尚未硬校验的边界，统一维护在 [飞升试炼玩法规格](docs/design/14-ascension-trial-gameplay.md)。协议示例见 [AI 动作示例](docs/design/12-ai-actions-examples.md)。

## 目录

```text
backend/
  cmd/server/             HTTP 与 WebSocket 服务组合根
  internal/challenge/     挑战查询、版本发布、哈希与奖励结算
  internal/challengeimport/ 仓库挑战材料加载、导入与空库初始化策略
  internal/orchestration/ 上下文、LLM、判定和动作编排
  internal/storage/       PostgreSQL 模型与仓储
  scripts/sql/init.sql    空数据库权威 schema
  data/preset_packs/      空库首次启动使用的官方挑战材料
  data/roll_config.yaml   判定算法与词表
  data/schemas/           模型结构化输出 Schema
frontend/
  src/views/              挑战首页、对话、设置与管理员页面
  src/stores/             会话、挑战、对话和运行时状态
docs/design/              正式设计文档
```

## 首次初始化

这是新项目结构，不包含旧表兼容、迁移或 AutoMigrate 旁路。请创建或清空 `fatecraft` 数据库后，通过 Navicat 连接该数据库并完整执行 [init.sql](backend/scripts/sql/init.sql)。脚本会一次创建认证、挑战版本、对话、上下文、判定和结算表。

然后配置 `backend/.env` 并启动后端：

```bash
cd backend
go run ./cmd/server
```

后端发现 `challenge_packs` 完全为空时，由独立的 `internal/challengeimport` 模块在事务中读取 `data/preset_packs/*`，校验资源引用和奖励规则、计算稳定内容哈希，并自动发布内置挑战。多个后端同时首次启动会通过 PostgreSQL advisory lock 串行初始化。

只要数据库已经存在任意挑战包，启动过程就完全跳过内置内容初始化，不读取、不升级也不覆盖数据库内容。仓库内置玩法提升版本后，由管理员进入“内置内容”检查版本与哈希并显式导入发布；自定义玩法仍通过草稿编辑器维护。

```bash
cd frontend
pnpm install
pnpm dev
```

默认地址是前端 `http://localhost:5173`、后端 `http://localhost:8081`。前端开发服务器代理 `/api` 和 `/ws`。

## 登录配置

首个创建的用户自动成为管理员，后续用户默认为普通用户。服务端会话使用随机令牌，数据库只保存令牌哈希；会话默认有效期为 14 天，可通过 `AUTH_SESSION_TTL` 调整。

邮箱登录、注册、邮箱验证码、邀请码和 SMTP 发信均由管理员在“管理 -> 账号与注册”中维护，保存后立即生效。SMTP 密码经 AES-GCM 加密后写入数据库；部署前必须配置一个至少 32 字符且长期保持不变的密钥：

```env
AUTH_DATA_ENCRYPTION_KEY=replace-with-a-long-random-secret
```

验证码只保存 bcrypt 摘要，最多尝试 5 次且只能消费一次。邀请码使用高熵随机值，数据库只保存 SHA-256 摘要；邀请码原文只在管理员创建后展示一次。邮箱注册默认关闭，邮箱登录默认开启。

LinuxDo Connect 需要在应用后台登记完全一致的回调地址：

```text
本地：http://localhost:5173/api/auth/linuxdo/callback
生产：https://你的域名/api/auth/linuxdo/callback
```

对应环境变量为 `LINUXDO_CLIENT_ID`、`LINUXDO_CLIENT_SECRET`、`LINUXDO_REDIRECT_URI`。不要提交包含真实密钥的 `.env`，也不要在已保存 SMTP 密码后随意更换 `AUTH_DATA_ENCRYPTION_KEY`。

## 内容发布

管理员登录后进入“管理 -> 挑战内容”：

1. 从当前发布版本复制一个新版本草稿，或新建挑战包和首个草稿。
2. 编辑 `manifest`、`assets`、`reward_rules` 组成的版本文档。
3. 保存草稿。
4. 执行“校验并发布”。

发布会检查 Manifest 与挑战包的一致性、资源类型与 ID、`system_master`、`settlement_security`、`output_schema` 三个核心 Prompt 分段、世界书条目、奖励条件和 Prompt 奖励引用。锁定草稿后，校验、SHA-256 计算和当前版本切换在同一事务内完成。

## 验证

```bash
cd backend
go test ./...
go vet ./...
```

```bash
cd frontend
pnpm build
```

数据库 schema、主要 HTTP 路由、内容生命周期和玩法维护索引详见 [设计文档](docs/design/README.md)。
