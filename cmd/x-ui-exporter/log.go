package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
)

func getLogHandler(filePath string) *slog.TextHandler {
	var w io.Writer = os.Stderr
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		w = f
	} else {
		fmt.Fprintf(os.Stderr, "WARNING: cannot open log file %q: %v. Logging to stderr instead.\n", filePath, err)
	}

	handler := slog.NewTextHandler(w, &slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
	})
	return handler
}
