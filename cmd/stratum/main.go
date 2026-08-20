package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/kokosx/stratumcms/internal/app"
	"github.com/kokosx/stratumcms/internal/config"
)

const version = "dev"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if len(os.Args) != 2 {
		usage()
		os.Exit(2)
	}

	switch os.Args[1] {
	case "version":
		fmt.Println(version)
	case "serve":
		cfg := config.Load()
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		if err := app.Run(ctx, cfg, logger); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("server stopped with an error", "error", err)
			os.Exit(1)
		}
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: stratum <serve|version>")
}
