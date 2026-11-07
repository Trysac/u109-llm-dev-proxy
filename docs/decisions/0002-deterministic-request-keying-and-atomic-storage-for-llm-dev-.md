# Deterministic Request Keying and Atomic Storage for LLM Dev Proxy

Status: accepted (generated, pending human review)

## Context

Development workflows with LLM APIs incur recurring costs, latency, and test non-determinism. A caching proxy needs to deterministically match requests without leaking credentials or leaving corrupt cache states upon crash.

## Decision

Use SHA-256 over method, URI, sorted vary headers, and canonical JSON bodies (preserving exact json.Number formatting). Strip credentials from key derivation and disk storage. Atomically write cache entries using temporary files and rename.

## Consequences

Requests with identical JSON payloads in differing key order match the same cache entry, while distinct numeric types (1 vs 1.0) stay distinct. Cache writes are crash-safe and never leak auth tokens to disk.
