# Testing

## Running

```sh
go test -cover .          # tests with coverage summary
go run ./scripts/check    # every gate, as CI and the factory run it
```

## Coverage & Policy

- Statement coverage must remain at or above 80%.
- Tests reside in package `llmdevproxy_test` and interact exclusively with the public API.
- We use `httptest.NewServer` for upstream mocks to ensure no actual network calls occur during testing.
- Tests are deterministic, avoiding reliance on wall-clock time or specific execution order.
- `t.TempDir` is used for all disk-based operations to ensure clean state per test.
- Every bug report must be accompanied by a failing test case that reproduces the issue.