#!/usr/bin/python3
import datetime
import os
import subprocess
import time
from zoneinfo import ZoneInfo


def next_run(now: datetime.datetime, run_at: datetime.time) -> datetime.datetime:
    target = now.replace(
        hour=run_at.hour, minute=run_at.minute, second=0, microsecond=0
    )
    if target <= now:
        target += datetime.timedelta(days=1)
    return target


def main() -> None:
    value = os.environ.get("BACKUP_TIME", "02:00")
    hour, minute = (int(part) for part in value.split(":", maxsplit=1))
    run_at = datetime.time(hour=hour, minute=minute)
    timezone = ZoneInfo(os.environ.get("TZ", "UTC"))
    print(f"PostgreSQL backup scheduled daily at {value} ({timezone.key})", flush=True)

    while True:
        now = datetime.datetime.now(timezone)
        target = next_run(now, run_at)
        time.sleep(max(1, (target - now).total_seconds()))
        try:
            subprocess.run(["/usr/local/bin/postgres-backup-now"], check=True)
        except subprocess.CalledProcessError as error:
            print(f"PostgreSQL backup failed with exit code {error.returncode}", flush=True)


if __name__ == "__main__":
    main()
