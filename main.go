package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/lmittmann/tint"
)

const (
	target       = "https://scrape-me.dreamsofcode.io"
	workersCount = 10
)

func main() {
	logger := slog.New(tint.NewHandler(os.Stdout, &tint.Options{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	deadlinks, err := StartScraper(target, workersCount)
	if err != nil {
		slog.Error(fmt.Sprintf("Error: %s", err.Error()))
		return
	}

	slog.Info("Result deadlinks:")
	slog.Info(fmt.Sprintf("%v", deadlinks))
	// for _, deadlink := range deadlinks {
	// 	slog.Info(deadlink)
	// }
}
