FROM postgres:18-alpine

RUN apk add --no-cache tzdata

ENV TZ=Asia/Shanghai
ENV BACKUP_DIR=/backups
ENV BACKUP_CRON_SCHEDULE="0 2 * * *"
ENV BACKUP_RETENTION_DAYS=7

COPY docker/postgres-backup/entrypoint.sh /usr/local/bin/postgres-backup-entrypoint
COPY docker/postgres-backup/backup.sh /usr/local/bin/postgres-backup-now

RUN chmod +x /usr/local/bin/postgres-backup-entrypoint /usr/local/bin/postgres-backup-now && \
    mkdir -p /backups

ENTRYPOINT ["/usr/local/bin/postgres-backup-entrypoint"]
CMD ["daemon"]
