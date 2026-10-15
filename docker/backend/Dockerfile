FROM --platform=$BUILDPLATFORM golang:1.25-alpine AS builder

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

FROM alpine:3.22

WORKDIR /app/backend
RUN apk add --no-cache ca-certificates tzdata wget

ENV GIN_MODE=release
ENV TZ=Asia/Shanghai
ENV DATA_DIR=data

COPY --from=builder /out/fatecraft ./fatecraft
COPY --from=builder /out/fatecraft-migrate ./fatecraft-migrate
COPY --from=builder /out/fatecraft-gameplay-check ./fatecraft-gameplay-check
COPY backend/data ./data

RUN chmod +x ./fatecraft ./fatecraft-migrate ./fatecraft-gameplay-check

EXPOSE 8081
HEALTHCHECK --interval=10s --timeout=3s --start-period=15s --retries=5 \
  CMD wget -qO- http://127.0.0.1:8081/api/health >/dev/null || exit 1

CMD ["./fatecraft"]
