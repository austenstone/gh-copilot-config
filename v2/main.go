package main

import (
	"fmt"
	"os"

	"github.com/austenstone/copilot-config/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "copilot-config:", err)
		os.Exit(1)
	}
}
