// Package llmdevproxy implements a local caching proxy for Large Language Model (LLM) APIs.
//
// It caches exact HTTP responses on disk using a deterministic key generated from
// the method, request URI, optional vary headers, and a canonical JSON representation
// of the request body. Sensitive headers such as Authorization and API keys are forwarded
// to upstream providers during record mode but are strictly excluded from cache keys
// and cache storage.
//
// # Threat Model & Security
//
// Defended:
//   - Credentials (Authorization, x-api-key, x-goog-api-key, Cookie) are stripped from
//     stored responses and excluded from the deterministic SHA-256 cache key calculation.
//   - Atomic file writes prevent cache corruption if the process crashes mid-write.
//   - Default listen address is 127.0.0.1.
//
// Not defended:
//   - Local cache poisoning: any local process or user with write access to Dir can alter cached responses.
//   - Local listener TLS: communications between local client SDKs and llmdevproxy are unencrypted HTTP.
//   - Concurrent identical misses: multiple in-flight identical misses may both query upstream.
package llmdevproxy
