package main

import (
	"fmt"
	"os"

	"lab_env/internal/invariants"
)

func main() {
	if err := invariants.GenerateSpecIndex(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}