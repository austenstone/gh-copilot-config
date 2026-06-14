package main

import (
	"fmt"
	"os"

	"github.com/austenstone/gh-copilot-config/internal/cmd"
)

var version = "dev"

func main() {
	if err := cmd.Execute(version); err != nil {
		fmt.Fprintln(os.Stderr, "copilot-config:", err)
		os.Exit(1)
	}
}
