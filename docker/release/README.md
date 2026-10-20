# fateCraft Linux 部署包

这是 FateCraft 的 Linux 二进制部署包，当前版本见同目录的 `VERSION` 文件。

如果你只想快速部署，推荐使用 Docker 镜像 `ghcr.io/ialtone/fatecraft`，它已经包含 PostgreSQL、Nginx、备份调度器和启动脚本。本部署包适用于已有 PostgreSQL 和 Nginx、希望直接运行二进制文件的 Linux 服务器。

## 包含内容

```text
fatecraft                  后端服务
fatecraft-migrate          数据库迁移工具
fatecraft-gameplay-check   玩法数据校验工具
dist/                      前端静态文件
data/                      内置玩法包、Schema 和初始数据
nginx.conf                 前端站点与反向代理示例
.env.example               生产配置示例
README.md                  本文件
VERSION                    版本号
LICENSE                    开源许可证
```

## 前置条件

- Linux `amd64` 或 `arm64`
- PostgreSQL 17 或 18
- Nginx
- 一个专用系统用户，例如 `fatecraft`

Windows 和 macOS 请使用 Docker Desktop 或 Docker Engine 运行正式镜像，不建议直接运行本部署包。

## 安装

以下示例假设安装到 `/opt/fatecraft`。如果使用其他目录，请同步修改 `nginx.conf` 中的 `root` 和 systemd 配置中的路径。

### 1. 解压并创建用户

```bash
sudo useradd --system --user-group --home /opt/fatecraft --shell /usr/sbin/nologin fatecraft
sudo mkdir -p /opt/fatecraft
sudo tar -xzf fatecraft-linux-amd64.tar.gz --strip-components=1 -C /opt/fatecraft
```

`arm64` 机器请把文件名换成 `fatecraft-linux-arm64.tar.gz`。

### 2. 创建数据库

在 PostgreSQL 中创建专用用户和数据库：

```sql
CREATE USER fatecraft WITH PASSWORD '替换为强密码';
CREATE DATABASE fatecraft OWNER fatecraft;
```

如果密码包含 `@`、`/`、`:` 等字符，写入 `DATABASE_URL` 前需要做 URL 编码。

### 3. 配置环境变量

```bash
cd /opt/fatecraft
sudo cp .env.example .env
sudoedit .env
```

至少修改：

- `DATABASE_URL`
- `AUTH_DATA_ENCRYPTION_KEY`：运行 `openssl rand -base64 32` 生成
- `SHARE_LINK_SIGNING_KEY`：另外生成一个不同的随机值
- 使用 HTTPS 时设置 `AUTH_COOKIE_SECURE=true`

`AUTH_DATA_ENCRYPTION_KEY` 和 `SHARE_LINK_SIGNING_KEY` 发布后不要随意更换，否则已有密文或分享链接可能无法解密/校验。

```bash
sudo chown -R fatecraft:fatecraft /opt/fatecraft
sudo chmod 600 /opt/fatecraft/.env
```

### 4. 执行数据库迁移

```bash
cd /opt/fatecraft
sudo -u fatecraft ./fatecraft-migrate up
```

可用命令：

```bash
sudo -u fatecraft ./fatecraft-migrate status
sudo -u fatecraft ./fatecraft-migrate version
```

迁移只支持前进，不支持回退。升级前请先备份数据库。

### 5. 启动后端

先手动确认可以启动：

```bash
cd /opt/fatecraft
sudo -u fatecraft ./fatecraft
```

默认监听所有地址的 `8081` 端口。请勿把 8081 直接暴露到公网，应由 Nginx 代理访问。

生产环境建议使用 systemd：

```ini
[Unit]
Description=fateCraft backend
After=network-online.target postgresql.service
Wants=network-online.target

[Service]
Type=simple
User=fatecraft
Group=fatecraft
WorkingDirectory=/opt/fatecraft
EnvironmentFile=/opt/fatecraft/.env
ExecStart=/opt/fatecraft/fatecraft
Restart=on-failure
RestartSec=5
NoNewPrivileges=true
PrivateTmp=true

[Install]
WantedBy=multi-user.target
```

保存为 `/etc/systemd/system/fatecraft.service`，然后执行：

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now fatecraft
sudo systemctl status fatecraft
```

### 6. 配置 Nginx

部署包中的 `nginx.conf` 已把站点根目录设为 `/opt/fatecraft/dist`，并将 `/api/`、`/s/`、`/shared/` 和 `/ws/` 代理到 `127.0.0.1:8081`。

```bash
sudo cp /opt/fatecraft/nginx.conf /etc/nginx/conf.d/fatecraft.conf
sudo nginx -t
sudo systemctl reload nginx
```

默认监听 80 端口。如果服务器上已有其他站点，请修改 `listen` 和 `server_name`，或把配置合并到现有站点中。

访问 `http://<服务器地址>/`。生产环境建议在前面再加一层 HTTPS 反向代理，或直接为 Nginx 配置 TLS。

## 升级

1. 备份数据库和 `/opt/fatecraft/.env`。
2. 解压新版本到临时目录。
3. 保留原有 `.env`，替换二进制文件、`dist/`、`data/` 和 `nginx.conf`。
4. 执行 `sudo -u fatecraft ./fatecraft-migrate up`。
5. 重启 `fatecraft` 和 Nginx。

不要直接删除整个安装目录，否则可能丢失 `.env` 或本地数据。

## 备份

使用 PostgreSQL 工具定期备份：

```bash
pg_dump -Fc -d fatecraft -f fatecraft-$(date +%F).dump
```

备份文件应复制到另一台设备，并定期实际演练恢复。恢复前先停止后端，避免写入。

## 校验下载文件

发布页同时提供 `checksums.txt`。下载后在校验和文件所在目录执行：

```bash
sha256sum -c checksums.txt
```

只有显示 `OK` 才表示文件下载完整、未被传输过程破坏。

## 安全建议

- 使用专用低权限用户运行，不要使用 root。
- 不要直接暴露后端 8081 端口。
- 使用 HTTPS，并设置 `AUTH_COOKIE_SECURE=true`。
- 妥善保管 `.env`，不要提交到 Git。
- 定期备份数据库和密钥。
- 升级前阅读对应 Release 的变更说明。
