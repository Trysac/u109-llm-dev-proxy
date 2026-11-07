package main

import (
	"os"

	"github.com/Trysac/u109-llm-dev-proxy"
)

func main() {
	os.Exit(llmdevproxy.Run(os.Args[1:], os.Stdout, os.Stderr))
}
