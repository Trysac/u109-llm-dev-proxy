# llm-dev-proxy

[![Go Reference](https://pkg.go.dev/badge/github.com/Trysac/u109-llm-dev-proxy.svg)](https://pkg.go.dev/github.com/Trysac/u109-llm-dev-proxy)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)
[![CI](https://github.com/Trysac/u109-llm-dev-proxy/actions/workflows/ci.yml/badge.svg)](https://github.com/Trysac/u109-llm-dev-proxy/actions/workflows/ci.yml)

Local HTTP proxy for LLM APIs that caches exact responses on disk for development: canonical JSON keys, auth headers never stored, record, replay and passthrough modes

## Problem

Describe in two or three sentences the concrete problem this utility solves, who hits it, and what
they do today without it. Replace this paragraph when the first real feature lands.

## Install

```sh
go get github.com/Trysac/u109-llm-dev-proxy
```

Requires Go 1.23 or newer. No dependencies outside the standard library.

## Usage

```go
package main

import (
	"errors"
	"fmt"

	"github.com/Trysac/u109-llm-dev-proxy"
)

func main() {
	// Normalize trims surrounding whitespace and lower-cases the text.
	out, _ := llmdevproxy.Normalize("  Hello World ")
	fmt.Println(out) // -> "hello world"

	// Blank input is rejected explicitly instead of returning an empty string.
	if _, err := llmdevproxy.Normalize("   "); errors.Is(err, llmdevproxy.ErrBlank) {
		fmt.Println("rejected:", err)
	}
}
```

## Documentation

- [Architecture](docs/ARCHITECTURE.md) and [decisions](docs/decisions/)
- [Testing](docs/TESTING.md) and [security](docs/SECURITY.md)
- [Project record](docs/PROJECT.md) and [stack](docs/STACK.md)
- [Changelog](CHANGELOG.md) and [contributing guide](CONTRIBUTING.md)

## License

[MIT](LICENSE) (c) 2026 Miguel Gomez
