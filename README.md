# Fatecraft

基于 `docs/design/` 实现的沉浸式 AI 文字冒险 / 角色扮演平台。前后端分离，对话之间默认互相隔离。

- 后端：Go + Gin + GORM（PostgreSQL）+ OpenAI 兼容流式 LLM，WebSocket 推送。
- 前端：Vue 3 + Vite + TypeScript + Pinia。

## 目录结构

```
backend/
  cmd/server/main.go      入口，装配依赖（组合根）
  internal/               config / storage / filestore / contextbuilder / llm / realtime / api / tokenizer / 各领域服务
                          orchestration 生成编排管线（上下文→LLM→解析→落库→应用状态→动作→推送）
                          protocol 模型输出信封协议（narrative/state_update/action/roll_request 解析与流式提取）
                          roll 天命判定（判定计算 / 历史调整 / 词表与特质匹配）
                          identity 玩家身份抽象（单用户 Provider，为多用户预留）
  scripts/sql/init.sql    数据库初始化脚本（手动执行）
  data/                   模板与配置数据目录（预设包 / 共享资源 / 判定配置 / 输出 schema），可由 DATA_DIR 改址
  .env.example            环境变量样例
frontend/
  src/                    api / stores / views / components / types
docker/                   Docker 部署（compose、镜像、env、自动备份），见 docker/README.md
Taskfile.yml              go-task 任务入口（开发/构建/测试）
```

## 后端运行

数据库由你手动初始化。使用 `backend/scripts/sql/init.sql` 建表（权威 schema）：

```bash
# 创建数据库和用户
psql -h localhost -p 5432 -U postgres -c "CREATE DATABASE fatecraft WITH ENCODING='UTF8' LC_COLLATE='C' LC_CTYPE='C' TEMPLATE=template0;"
psql -h localhost -p 5432 -U postgres -c "CREATE USER fatecraft WITH PASSWORD 'fatecraft-dev';"
psql -h localhost -p 5432 -U postgres -c "GRANT ALL PRIVILEGES ON DATABASE fatecraft TO fatecraft;"
psql -h localhost -p 5432 -U postgres -d fatecraft -c "GRANT ALL ON SCHEMA public TO fatecraft;"

# 执行初始化脚本
psql -h localhost -p 5432 -U fatecraft -d fatecraft -f backend/scripts/sql/init.sql
```

模板数据不入数据库，存放在 `backend/data/` 下，启动时自动加载：

- `preset_packs/reincarnation-trials/` — 内置修仙预设包「轮回试炼」，含 characters（命运编织者 / 天道试炼官 / 轮回引路人）/ personas（剑修、炼丹师等玩家人设）/ scenarios（轮回之门 / 命运长河 / 天道试炼场）/ prompts（飞升试炼预设）/ worldbooks（飞升系统设定），预设包内容整体只读；
- `shared/` — 运行时可写目录（characters / personas / scenarios / prompts），前端新建或编辑模板时后端写回这里的 JSON 文件，初始为空；
- `schemas/narrative_response.json` — 模型结构化输出的 JSON Schema；
- `roll_config.yaml` — 天命判定系统配置（判定类别、情境修正词表、收益平衡等）。

然后配置并启动后端：

```bash
cd backend
cp .env.example .env        # 填写 DATABASE_URL，可选 LLM_API_KEY
go mod tidy
go run ./cmd/server         # 监听 :8081
```

> LLM 的 Base URL / API Key / 模型可在前端「设置」页配置，或写入 `llm_settings` 表 / `LLM_API_KEY` 环境变量。

## 前端运行

```bash
cd frontend
npm install
npm run dev                 # 监听 :5173，自动代理 /api 与 /ws 到 :8081
```

打开 http://localhost:5173 ，新建对话即可开始。

## Task 与 Docker

装有 [go-task](https://taskfile.dev) 时可用统一入口（任务清单见 `Taskfile.yml`）：

```bash
task backend:dev        # 本机跑后端（:8081）
task frontend:dev       # 本机跑前端（:5173）
task backend:test       # go vet + go test
task backend:build      # 交叉编译 Linux amd64 二进制 → docker/backend/bin/
task frontend:build     # 前端产物 → docker/frontend/dist/
```

Docker 部署（PostgreSQL 18 + 每日自动备份 sidecar + 后端，生产另含 nginx 前端站点）：

```bash
task backend:build && task frontend:build
cd docker
docker compose -f docker-compose.dev.yml up --build          # 开发：后端 :8081
docker compose -f docker-compose.prod.yml up --build -d      # 生产：前端入口 :8088
```

数据库需要手动初始化（见 `docker/README.md`）；模板 JSON 目录直接挂载进容器。详见 `docker/README.md`。

## 关键设计落点

- **对话隔离**：所有运行时表带 `conversation_id`，仓储层强制按对话查询；模板（角色卡 / Persona / 场景 / Prompt 预设 / 世界书）存放于 `backend/data/` 下的 JSON 文件（每个模板一个文件，id 即文件名 slug），可跨对话复用，但不携带运行时记忆。
- **上下文管理**：`contextbuilder` 按 token 预算装配（必选段超预算直接报错、不发请求），倒序填充最近历史，世界书按关键词 + 预算激活，写入 `context_build_logs` 供「上下文检查器」展示。
- **结构化状态**：模型输出 `{narrative, state_update, action}` 信封；后端按路径白名单校验后应用补丁并记录修订；AI 可通过 action 字段触发系统级操作（如自动结束对话）。
- **生成流程**：WebSocket `user_input` → 校验 active + 抢生成锁 → 保存用户消息 → 构造上下文 → 流式回复 → 解析 → 应用状态 → 处理动作 → 落库 → 推送。

## 测试

```bash
cd backend && go test ./...     # contextbuilder 预算 / state 路径白名单 / worldbook 扫描 / action 解析
```
