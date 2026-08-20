package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/kokosx/stratumcms/internal/app"
	"github.com/kokosx/stratumcms/internal/config"
	"github.com/kokosx/stratumcms/internal/operations"
)

var version = "dev"
var commit = "unknown"
var buildDate = "unknown"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	if os.Args[1] == "version" {
		fmt.Printf("stratum %s\ncommit %s\nbuilt %s\n", version, commit, buildDate)
		return
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuration error:", err)
		os.Exit(2)
	}

	switch os.Args[1] {
	case "serve":
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()

		if err := app.Run(ctx, cfg, logger); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("server stopped with an error", "error", err)
			os.Exit(1)
		}
	case "doctor":
		if err := operations.Doctor(context.Background(), cfg, os.Stdout); err != nil {
			os.Exit(1)
		}
	case "maintenance":
		if err := operations.Maintenance(context.Background(), cfg, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, "maintenance:", err)
			os.Exit(1)
		}
	case "backup":
		fs := flag.NewFlagSet("backup", flag.ContinueOnError)
		fs.SetOutput(os.Stderr)
		output := fs.String("output", "", "backup archive path")
		if err := fs.Parse(os.Args[2:]); err != nil {
			os.Exit(2)
		}
		name, err := operations.Backup(context.Background(), cfg, version, *output)
		if err != nil {
			fmt.Fprintln(os.Stderr, "backup:", err)
			os.Exit(1)
		}
		fmt.Println(name)
	case "restore":
		if len(os.Args) != 3 {
			fmt.Fprintln(os.Stderr, "usage: stratum restore <backup>")
			os.Exit(2)
		}
		if err := operations.Restore(context.Background(), cfg, os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, "restore:", err)
			os.Exit(1)
		}
		fmt.Println("restore complete")
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: stratum <serve|version|doctor|maintenance|backup|restore>")
}
