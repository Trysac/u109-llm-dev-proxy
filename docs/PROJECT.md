# Project record

## Objective

To provide a local HTTP proxy for LLM APIs that caches exact responses on disk for development, using canonical JSON keys, ensuring auth headers are never stored, and supporting record, replay, and passthrough modes.

## Scope

- Implementing the `llmdevproxy` Go package for caching HTTP requests.
- Providing a CLI tool `llm-dev-proxy` for development environments.
- Maintaining canonical request keying based on method, URI, vary headers, and JSON-decoded bodies.
- Ensuring atomic file writes to the cache directory.

## Out of Scope

- Semantic caching or fuzzy response matching.
- TTL-based cache eviction or automatic cleanup.
- HTTPS interception (clients must be configured to point their base URL at this proxy).
- Request rewriting logic beyond transparent proxying.

## Factory run u109 (measured from API usage)

| Phase | Model | Batch | Input | Output | Cache write | Cache read | Cost (USD) |
|---|---|---|---|---|---|---|---|
| generation | gemini-3.8-flash | yes | 4370 | 16125 | 0 | 0 | 0.0319 |
| fix-1 | gemini-3.8-flash | no | 53654 | 774 | 0 | 145056 | 0.0540 |
| fix-2 | gemini-3.8-flash | no | 20669 | 261 | 0 | 13515 | 0.0175 |
| fix-3 | gemini-3.8-flash | no | 72484 | 1701 | 0 | 414258 | 0.0918 |
| fix-4 | gemini-3.8-flash | no | 65445 | 1645 | 0 | 162833 | 0.0675 |
| fix-1 | gemini-3.8-flash | no | 86254 | 918 | 0 | 661949 | 0.1178 |
| fix-2 | gemini-3.8-flash | no | 85973 | 2384 | 0 | 662880 | 0.1231 |
| fix-3 | gemini-3.8-flash | no | 84102 | 909 | 0 | 618633 | 0.1129 |
| fix-4 | gemini-3.8-flash | no | 85817 | 724 | 0 | 664839 | 0.1169 |
| fix-1 | gemini-3.8-flash | no | 66710 | 1644 | 0 | 397813 | 0.0860 |
| fix-2 | gemini-3.8-flash | no | 21141 | 2120 | 0 | 27485 | 0.0259 |
| audit | gemini-3.1-flash-lite | no | 28807 | 766 | 0 | 20385 | 0.0089 |
| fix-3 | gemini-3.8-flash | no | 43597 | 3000 | 0 | 197907 | 0.0588 |
| fix-4 | gemini-3.8-flash | no | 90832 | 3099 | 0 | 772555 | 0.1377 |
| docs | gemini-3.1-flash-lite | no | 4046 | 1580 | 0 | 0 | 0.0034 |

Total: $1.0540. Prices: Anthropic list prices.
