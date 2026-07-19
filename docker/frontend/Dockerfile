# Fatecraft 前端静态托管镜像。
# 静态产物由宿主机 `task frontend:build` 产出到 docker/frontend/dist。
FROM nginx:1.27-alpine

ENV TZ=Asia/Shanghai

COPY ./nginx.conf /etc/nginx/conf.d/default.conf
COPY ./dist /usr/share/nginx/html

EXPOSE 80

HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=5 \
  CMD wget -qO- http://127.0.0.1:80/ >/dev/null || exit 1
