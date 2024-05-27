package main

import (
	"fmt"
	"github.com/fanonwue/go-short-link/internal"
	"os"
)

func main() {
	err := internal.Run()
	internal.OnExit()
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "Unrecoverable Error: %v", err)
		os.Exit(1)
	}
}
