package main

import (
	"context"
	"fmt"
	"github.com/fanonwue/go-short-link/internal"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	appContext, _ := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := internal.Run(appContext)
	internal.OnExit()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Unrecoverable Error: %v", err)
		os.Exit(1)
	}
}
