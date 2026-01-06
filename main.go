package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/fanonwue/go-short-link/internal"
)

func main() {
	appContext, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	err := internal.Run(appContext)
	internal.OnExit()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Unrecoverable Error: %v", err)
		os.Exit(1)
	}
}
