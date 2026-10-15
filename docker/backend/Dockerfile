# Fatecraft 后端运行时镜像。
# 二进制由宿主机 `task backend:build` 交叉编译产出，
# 镜像本身不做编译，保持构建快、体积小。
FROM alpine:3.22

WORKDIR /app/backend

RUN apk add --no-cache ca-certificates tzdata wget

ENV GIN_MODE=release
ENV TZ=Asia/Shanghai

COPY ./bin/fatecraft ./fatecraft
COPY ./bin/fatecraft-migrate ./fatecraft-migrate
COPY ./bin/fatecraft-gameplay-check ./fatecraft-gameplay-check

# 判定配置、响应 Schema 和首次启动材料由 compose 挂载到 /app/backend/data（DATA_DIR=data）。
RUN chmod +x ./fatecraft ./fatecraft-migrate ./fatecraft-gameplay-check && mkdir -p data

EXPOSE 8081

HEALTHCHECK --interval=10s --timeout=3s --start-period=15s --retries=5 \
  CMD wget -qO- http://127.0.0.1:8081/api/health >/dev/null || exit 1

CMD ["./fatecraft"]
