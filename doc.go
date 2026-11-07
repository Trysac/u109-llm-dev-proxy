// Package llmdevproxy is a small, dependency-free Go library: Local HTTP proxy for LLM APIs that caches exact responses on disk for development: canonical JSON keys, auth headers never stored, record, replay and passthrough modes
//
// The public API is every exported identifier of this root package. Functions are pure: they do
// no I/O, keep no global mutable state and report invalid input through returned errors.
package llmdevproxy
