// gameplay-check performs read-only deployment compatibility validation.
package main

import (
	"context"
	"fmt"
	"github.com/ialtone/fatecraft/internal/challenge"
	"github.com/ialtone/fatecraft/internal/config"
	"github.com/ialtone/fatecraft/internal/gameplay/bootstrap"
	"github.com/ialtone/fatecraft/internal/storage"
	"os"
	"time"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg := config.Load()
	db, err := storage.Open(cfg.DatabaseURL)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	problems, err := challenge.NewService(storage.NewChallengeRepository(db), bootstrap.NewRegistry()).CheckDeployment(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	for _, p := range problems {
		fmt.Fprintln(os.Stderr, p)
	}
	if len(problems) > 0 {
		os.Exit(1)
	}
	fmt.Println("gameplay dependencies compatible")
}
