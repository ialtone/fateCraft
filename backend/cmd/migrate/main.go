package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ialtone/fatecraft/internal/config"
	"github.com/ialtone/fatecraft/internal/dbmigration"
)

func main() {
	command := "up"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	cfg := config.Load()
	db, err := dbmigration.Open(cfg.DatabaseURL)
	if err != nil {
		fatal(err)
	}
	defer db.Close()

	ctx := context.Background()
	switch command {
	case "up":
		err = dbmigration.Up(ctx, db)
	case "down":
		if os.Getenv("ALLOW_MIGRATION_DOWN") != "true" {
			fatal(fmt.Errorf("down is disabled; set ALLOW_MIGRATION_DOWN=true for local development"))
		}
		err = dbmigration.Down(ctx, db)
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
	default:
		fatal(fmt.Errorf("unknown migration command %q (use up, down, status, or version)", command))
	}
	if err != nil {
		fatal(err)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
