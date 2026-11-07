# Architecture

## Overview

`llmdevproxy` sits between LLM client SDKs and upstream LLM providers (Anthropic, OpenAI, etc.).

```
[Client SDK] -> [llm-dev-proxy:8788] -> [Upstream Provider]
                      |
                 [.llm-cache/]
```

## Request Keying

Cache keys are SHA-256 digests in lowercase hex over:
1. HTTP Method + `\n`
2. Request URI + `\n`
3. Sorted vary header pairs (lowercase name + `:` + first value + `\n`)
4. `\n` delimiter
5. Canonical JSON body (or raw bytes if non-JSON)

### Canonical JSON Format

JSON bodies are decoded with `json.Decoder.UseNumber()` and re-encoded with sorted keys, HTML unescaped, no extraneous whitespace, and trailing newlines stripped. As numbers are preserved as `json.Number`, numeric formatting differences such as `1` and `1.0` produce distinct keys.

### Security & Redaction

Headers like `Authorization`, `x-api-key`, `x-goog-api-key`, and `Cookie` are forwarded to upstream during calls but are never stored on disk or included in key calculation.
