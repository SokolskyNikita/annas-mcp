package main

import (
	"os"

	"github.com/SokolskyNikita/annas-mcp/internal/modes"
)

func main() {
	os.Exit(modes.RunCLI())
}
