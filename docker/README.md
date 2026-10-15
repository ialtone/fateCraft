# Docker 部署

生产部署由一个 GHCR 镜像和一个 Docker 容器组成。容器内运行 PostgreSQL、FateCraft 后端、Nginx 前端以及数据库备份调度器；宿主机只需安装 Docker Engine 和 Docker Compose 插件，不需要安装 Go、Node.js 或 PostgreSQL。

该方案按单机、自部署场景设计。应用与数据库共享容器的升级和故障生命周期，容器停止时整个服务会一起停止；数据库文件保存在独立 Docker named volume 中，不会写入镜像层。不要删除 `fatecraft_postgres_data` 卷，否则会删除数据库数据。

## 安装与启动

在 NAS 或服务器上准备 Compose 文件和环境配置（可从仓库的 `docker` 目录取得这些文件）：

```bash
cp docker/.env.example docker/.env
cp docker/backend.production.env docker/backend.production.env.local
```

编辑 `.env` 选择发布版本。编辑 `backend.production.env.local`，至少设置数据库密码、同步更新 `DATABASE_URL`，并设置独立且稳定的 `AUTH_DATA_ENCRYPTION_KEY`、`SHARE_LINK_SIGNING_KEY`（各至少 32 个字符）。LinuxDo Connect 未配置时，相关登录功能不可用。不要将本地密钥提交到仓库。

拉取并启动：

```bash
docker compose --env-file .env -f docker-compose.prod.yml pull
docker compose --env-file .env -f docker-compose.prod.yml up -d
```

访问 `http://<host>:8088`。服务只发布 Web 端口；PostgreSQL 和后端 HTTP 端口仅在容器内部监听。生产环境应在前方配置 HTTPS 反向代理。

升级时将 `.env` 中的 `FATECRAFT_VERSION` 改为目标版本，再执行 `pull` 和 `up -d`。容器启动后会先等待 PostgreSQL 就绪、执行待运行的数据库迁移，再启动后端提供服务。空数据库会从镜像中的内置数据初始化玩法。

## 数据与迁移

权威数据库结构位于 `backend/migrations/*.sql`。迁移版本记录在 `goose_db_version`；启动迁移失败时后端不会开始服务。

PostgreSQL 数据持久化在 Compose named volume `fatecraft_postgres_data`，容器内路径为 `/var/lib/postgresql`。生产数据库不映射到宿主机端口。如需用数据库客户端检查数据，可临时创建本机端口转发：

```bash
docker compose --env-file docker/.env -f docker/docker-compose.prod.yml exec fatecraft sh
```

进入容器后可使用 PostgreSQL 自带的 `psql` 连接 `127.0.0.1:5432`。不要为了远程数据库访问直接开放 PostgreSQL 端口。

当前未冻结的基线变更继续并入 `00001`；已执行旧版 `00001` 的开发数据库不会因更新容器而自动同步结构。新版本正式发布后应冻结基线，后续结构修改通过新增 migration 升级。迁移仅支持 `up/status/version`，`00001` 明确禁止回退。

## 备份与恢复

容器内调度器默认按东八区每天 `02:00` 执行 `pg_dump`，压缩格式备份写入宿主机 `backups`，默认保留 7 天。在 `.env` 中可设置 `BACKUP_TIME` 和 `BACKUP_RETENTION_DAYS`。

查看服务日志和立即创建备份：

```bash
docker compose --env-file .env -f docker-compose.prod.yml logs -f fatecraft
docker compose --env-file .env -f docker-compose.prod.yml exec fatecraft /usr/local/bin/postgres-backup-now
```

恢复前停止后端进程并准备好目标数据库。以下命令会覆盖同名数据库对象；`<backup-file>` 替换为 `backups` 中的文件名：

```bash
docker compose --env-file .env -f docker-compose.prod.yml exec fatecraft sh
```

在容器 shell 中执行：

```bash
supervisorctl stop backend
PGPASSWORD="$POSTGRES_PASSWORD" pg_restore --clean --if-exists --no-owner --no-privileges --host=127.0.0.1 --port=5432 --username="$POSTGRES_USER" --dbname="$POSTGRES_DB" "/backups/<backup-file>"
supervisorctl start backend
```

退出后重启容器：

```bash
docker compose --env-file .env -f docker-compose.prod.yml restart fatecraft
```

建议定期将 `backups` 复制到另一台设备，并实际演练恢复。

## 镜像发布

推送 `vX.Y.Z` 标签后，GitHub Actions 先运行验证，再构建并发布一个多架构镜像 `ghcr.io/ialtone/fatecraft`，标签包括 `vX.Y.Z`、`X.Y.Z` 和 `latest`。首次发布后需在 GitHub Packages 确认该镜像为公开，用户才可匿名拉取。

镜像支持 `linux/amd64` 和 `linux/arm64`。容器内 Nginx 托管 SPA，并将 `/api`、`/s` 和 `/ws` 反代到同容器后端。Nginx 请求体上限为 `33m`，与后端挑战版本保存的 32 MiB 上限对齐。

应用访问日志会脱敏分享 token；若使用 CDN、WAF 或额外负载均衡，也应对 `/s/*`、`/shared/*`、`/api/public/conversation-shares/*` 禁止记录完整路径或作等价脱敏。日志保留、容量与诊断说明见[日志与诊断](../docs/design/23-logging-and-diagnostics.md)。
