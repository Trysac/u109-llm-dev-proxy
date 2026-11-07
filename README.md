# llm-dev-proxy

During development, repeated LLM requests are slow, costly, and introduce non-determinism. This project provides a local HTTP proxy that caches API responses on disk, using canonical JSON keying to ensure cache hits remain reliable across runs.

## Installation

```bash
go get github.com/Trysac/u109-llm-dev-proxy
```

## Usage Example

```go
import (
    "net/http"
    "net/url"
    "github.com/Trysac/u109-llm-dev-proxy/llmdevproxy"
)

upURL, _ := url.Parse("https://api.openai.com")
proxy, _ := llmdevproxy.New(llmdevproxy.Config{
    Upstream: upURL,
    Dir:      ".llm-cache",
    Mode:     llmdevproxy.ModeRecord,
})
http.ListenAndServe("127.0.0.1:8788", proxy)
```

## Public API

- `New(Config) (*Proxy, error)`: Initializes the proxy.
- `ServeHTTP(http.ResponseWriter, *http.Request)`: Standard interface to handle proxying.
- `Key(method, uri string, header http.Header, body []byte, vary []string) string`: Calculates the cache key.
- `Config`: Configuration struct for `Upstream`, `Dir`, `Mode`, `VaryHeaders`, `MaxRequestBytes`, `MaxResponseBytes`, `MaxEntries`, `Transport`, and `OnEvent` callback.
- `Mode`: Constants `ModeRecord`, `ModeReplay`, `ModePassthrough`.
- `ErrInvalidConfig`: Returned when configuration is improper.

## Why not generic VCR cassettes?

Unlike SDK-specific VCR libraries which require language-specific instrumentation, this is a standalone proxy. Any language or client SDK can use it simply by pointing its base URL at the local proxy, keeping the code environment-agnostic.

## Security

See [docs/SECURITY.md](docs/SECURITY.md) for the threat model and limits on credential storage.