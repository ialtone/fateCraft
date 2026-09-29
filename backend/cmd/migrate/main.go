package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ialtone/fatecraft/internal/config"
	"github.com/ialtone/fatecraft/internal/dbmigration"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:])
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	command := "up"
	if len(args) > 1 {
		return fmt.Errorf("usage: fatecraft-migrate [up|status|version]")
	}
	if len(args) == 1 {
		command = args[0]
	}
	// Validate before loading configuration or opening a database connection.
	switch command {
	case "up", "status", "version":
	case "down":
		return fmt.Errorf("down is disabled: baseline 00001 is irreversible; use up, status, or version")
	default:
		return fmt.Errorf("unknown migration command %q (use up, status, or version)", command)
	}

	cfg := config.Load()
	timeout, err := durationEnv("MIGRATION_TIMEOUT", 10*time.Minute)
	if err != nil {
		return err
	}
	connectTimeout, err := durationEnv("MIGRATION_CONNECT_TIMEOUT", 10*time.Second)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	connectCtx, cancelConnect := context.WithTimeout(ctx, connectTimeout)
	db, err := dbmigration.Open(connectCtx, cfg.DatabaseURL)
	cancelConnect()
	if err != nil {
		return err
	}
	defer db.Close()

	switch command {
	case "up":
		err = dbmigration.Up(ctx, db)
	case "status":
		statuses, statusErr := dbmigration.Status(ctx, db)
		if statusErr != nil {
			err = statusErr
			break
		}
		for _, item := range statuses {
			fmt.Printf("%d %s\n", item.Version, item.State)
		}
	case "version":
		version, versionErr := dbmigration.Version(ctx, db)
		if versionErr != nil {
			err = versionErr
			break
		}
		fmt.Println(version)
	}
	return err
}

func durationEnv(name string, fallback time.Duration) (time.Duration, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration, for example 10s or 10m", name)
	}
	return duration, nil
}
