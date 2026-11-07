# Security

## Threat Model

### Defended
- **Credential Leakage**: Sensitive headers (`Authorization`, `x-api-key`, `x-goog-api-key`, `Cookie`) are forwarded to upstream providers but strictly excluded from disk storage and key calculation.
- **Cache Corruption**: Cache writes use atomic temporary files (`os.CreateTemp` then `os.Rename`) to prevent partial writes.
- **Network Exposure**: The default proxy listener binds strictly to `127.0.0.1`.
- **Payload Limits**: Enforces `MaxRequestBytes` (default 10MB) and `MaxResponseBytes` (default 50MB) to prevent resource exhaustion.

### Not Defended
- **Local Multi-User Cache Poisoning**: Any user with write access to the cache directory can modify cached entries.
- **Concurrent Duplicate Misses**: Identical requests sent simultaneously are not single-flighted; both may trigger an upstream call.
- **Transport Security**: The local proxy listener communicates over plaintext HTTP.