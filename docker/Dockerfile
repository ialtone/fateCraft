FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS backend-builder

ARG TARGETOS
ARG TARGETARCH
WORKDIR /src/backend

COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN mkdir -p /out && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags "-s -w" -o /out/fatecraft ./cmd/server && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags "-s -w" -o /out/fatecraft-migrate ./cmd/migrate && \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags "-s -w" -o /out/fatecraft-gameplay-check ./cmd/gameplay-check

FROM --platform=$BUILDPLATFORM node:22-alpine AS frontend-builder

WORKDIR /src/frontend
ENV COREPACK_ENABLE_DOWNLOAD_PROMPT=0
RUN corepack enable && corepack prepare pnpm@10.28.1 --activate
COPY frontend/package.json frontend/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY frontend/ ./
RUN pnpm run build

FROM postgres:18-alpine

RUN apk add --no-cache nginx supervisor su-exec tzdata wget python3

WORKDIR /app/backend
ENV GIN_MODE=release \
    TZ=Asia/Shanghai \
    DATA_DIR=data \
    BACKUP_DIR=/backups \
    BACKUP_TIME="02:00" \
    BACKUP_RETENTION_DAYS=7

COPY --from=backend-builder /out/fatecraft ./fatecraft
COPY --from=backend-builder /out/fatecraft-migrate ./fatecraft-migrate
COPY --from=backend-builder /out/fatecraft-gameplay-check ./fatecraft-gameplay-check
COPY --from=backend-builder /src/backend/data ./data
COPY --from=frontend-builder /src/frontend/dist /usr/share/nginx/html
COPY docker/frontend/nginx.conf /etc/nginx/http.d/default.conf
COPY docker/postgres-backup/backup.sh /usr/local/bin/postgres-backup-now
COPY docker/postgres-backup/scheduler.py /usr/local/bin/postgres-backup-scheduler
COPY docker/entrypoint.sh /usr/local/bin/fatecraft-entrypoint
COPY docker/backend-start.sh /usr/local/bin/fatecraft-backend-start
COPY docker/supervisord.conf /etc/supervisord.conf

RUN chmod +x ./fatecraft ./fatecraft-migrate ./fatecraft-gameplay-check \
      /usr/local/bin/fatecraft-entrypoint \
      /usr/local/bin/fatecraft-backend-start \
      /usr/local/bin/postgres-backup-now \
      /usr/local/bin/postgres-backup-scheduler \
    && mkdir -p /backups

EXPOSE 80
ENTRYPOINT ["/usr/local/bin/fatecraft-entrypoint"]
CMD ["all"]
