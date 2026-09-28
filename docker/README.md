# Docker Workflow

## 这台机器（不装 Docker）

- 后端开发：`task backend:dev`
- 数据库升级/状态：`task db:migrate` / `task db:status`
- 后端测试：`task backend:test`
- 前端开发：`task frontend:dev`
- 前端类型检查：`task frontend:check`
- 后端 Linux amd64 二进制：`task backend:build`（输出 `docker/backend/bin/fatecraft` 和 `fatecraft-migrate`）
- 前端静态产物：`task frontend:build`（输出 `docker/frontend/dist`）

本机直跑时的配置文件：`backend/.env`（参考 `backend/.env.example`）。

## 有 Docker 的机器

### 开发环境

- 配置文件：`docker/backend.development.env`
- 确保两个后端二进制已由 `task backend:build` 生成
- 启动后端 + PostgreSQL + 自动备份：`docker compose -f docker-compose.dev.yml up --build`
- Compose 会在 PostgreSQL healthy 后运行一次性 `migrate` 服务；只有迁移成功后才启动后端
- 前端开发仍在宿主机跑 `task frontend:dev`（Vite 把 `/api`、`/ws` 代理到 :8081）
- 复用外部 PostgreSQL：改 `POSTGRES_HOST` 与 `DATABASE_URL` 后先执行
  `docker compose -f docker-compose.dev.yml run --rm --build --no-deps migrate`，再按需启动后端和备份服务

### 生产环境

- 配置文件：`docker/backend.production.env`（**务必改 `POSTGRES_PASSWORD` 并同步 `DATABASE_URL`**）
- 确保二进制与前端产物已生成：`task backend:build && task frontend:build`
- 启动全部服务（含前端站点）：`docker compose -f docker-compose.prod.yml up --build -d`
- 访问入口：前端 `http://<host>:8088`（nginx 同时反代 `/api` 与 `/ws`，无需直连 8081）
- 后端端口在生产 compose 中只绑定 `127.0.0.1:8081`，公网流量必须经过 nginx；`TRUSTED_PROXIES` 默认信任 Docker 网络内的 nginx，用于从 `X-Forwarded-For` 取真实客户端 IP。若改成对外暴露后端端口，必须同时收紧 `TRUSTED_PROXIES`，否则客户端可以伪造来源 IP 绕过限流
- nginx 请求体上限为 `33m`，与后端挑战版本保存的 32 MiB 上限对齐
- 生产前必须替换 `SHARE_LINK_SIGNING_KEY`；它至少 32 字符、独立于 `AUTH_DATA_ENCRYPTION_KEY`，且所有后端实例必须一致

## 数据与初始化

- **数据库初始化与升级（Docker 自动编排）**：权威结构是 `backend/migrations/*.sql`。
  两个 compose 文件都会在 PostgreSQL healthy 后运行一次性 `migrate` 服务，执行内嵌在
  `fatecraft-migrate` 中的全部待执行 migration。空库完成初始化，已有数据库只执行新版本；
  版本记录保存在 `goose_db_version`，迁移失败时后端不会启动。

  ```bash
  # 初始化或升级数据库，然后启动后端
  docker compose -f docker-compose.dev.yml up --build postgresql migrate backend

  # 单独查看迁移状态
  docker compose -f docker-compose.dev.yml run --rm migrate status
  ```

  注：compose 已设置 `name: fatecraft`，容器名为 `fatecraft-postgresql-1`；
  容器内的超级用户就是 `POSTGRES_USER` 指定的 `fatecraft`，没有 `postgres` 用户。

- **挑战内容**：运行时内容存于 PostgreSQL。compose 挂载 `backend/data/` 用于
  `roll_config.yaml`、输出 Schema 和空库首次启动的内置挑战材料。后端启动时发现
  `challenge_packs` 为空会自动校验并发布内置挑战，不需要额外命令；数据库已有任意挑战包时
  不读取、不升级也不覆盖内容。服务不会把管理端修改写回宿主机文件。
- **复用外部 PostgreSQL** 时由管理员创建数据库与用户，再运行迁移命令：

  ```bash
  psql -h 你的数据库地址 -U 管理员用户 -c "CREATE DATABASE fatecraft WITH ENCODING='UTF8' LC_COLLATE='C' LC_CTYPE='C' TEMPLATE=template0;"
  psql -h 你的数据库地址 -U 管理员用户 -c "CREATE USER fatecraft WITH PASSWORD '你的密码';"
  psql -h 你的数据库地址 -U 管理员用户 -c "GRANT ALL PRIVILEGES ON DATABASE fatecraft TO fatecraft;"
  psql -h 你的数据库地址 -U 管理员用户 -d fatecraft -c "GRANT ALL ON SCHEMA public TO fatecraft;"
  cd backend
  DATABASE_URL='postgres://fatecraft:密码@数据库地址:5432/fatecraft?sslmode=require' go run ./cmd/migrate up
  ```

- Postgres 18+ 的数据卷挂载到 `/var/lib/postgresql`（不要挂 `/var/lib/postgresql/data`）。
- **端口映射**：PostgreSQL 映射到宿主机端口按环境区分——开发环境（`docker-compose.dev.yml`）用 `15433`，生产环境（`docker-compose.prod.yml`）用 `15432`（容器内均为 `5432`）。

## 数据库备份

- 由 `postgres-backup` sidecar 容器执行，宿主机无需装 cron；限制 `0.75 CPU`、`256MB` 内存
- 默认东八区每天 `02:00` 触发 `pg_dump`，备份输出到宿主机 `docker/backups`，日志在 `docker/backups/backup.log`
- 默认保留最近 `7` 天，可用 `BACKUP_RETENTION_DAYS` 覆盖；备份时间用 `BACKUP_CRON_SCHEDULE` 覆盖
- 手动立即备份：
  `docker compose -f docker-compose.prod.yml exec postgres-backup /usr/local/bin/postgres-backup-now`
- 恢复示例：
  `docker run --rm -e PGPASSWORD=你的密码 -v $(pwd)/backups:/backups postgres:18-alpine pg_restore --clean --if-exists --no-owner --host=你的数据库地址 --port=5432 --username=fatecraft --dbname=fatecraft /backups/你的备份文件.dump`

## 其他

- 后端镜像不做编译，只拷贝宿主机交叉编译的服务与迁移二进制（alpine + 健康检查 `GET /api/health`）
- 前端镜像为 `nginx:1.27-alpine`，托管 SPA 并反代 API/WebSocket；WebSocket 反代已配置 `Upgrade` 头与 1h 读超时
- Nginx 将 `/s/:token` 反代到后端的动态 Open Graph 外壳，再由浏览器进入 `/shared/:token` SPA 页面；该入口和公共分享 API不配置共享缓存，撤销立即生效
- 应用访问日志会脱敏分享 token；外围 CDN、WAF 和负载均衡也必须对 `/s/*`、`/api/public/conversation-shares/*` 禁止记录完整路径或做等价脱敏
- 分享 HTML 使用严格 CSP、`Referrer-Policy: no-referrer` 和 `noindex,nofollow`；公开 API 的 gzip 由 Nginx 处理
- LLM 的 Base URL、API Key 和模型可在前端「设置」页配置，用户配置写入 `llm_settings` 表，平台公共配置写入 `system_settings` 单例
- 主站当前没有 CSP，客户端直连不受阻；今后若给主站加 CSP，`connect-src` 必须允许用户配置的模型域名，否则客户端直连整体失效
- 敏感信息的本机覆盖写 `backend.development.env.local` / `backend.production.env.local`（git 已忽略；如使用需在 compose 的 `env_file` 中追加该文件）
