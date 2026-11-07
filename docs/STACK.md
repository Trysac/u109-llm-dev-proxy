# Stack

## Used

- **Go >= 1.23**: Required for the standard library and toolchain.
- **Standard Library Only**: No external dependencies allowed to ensure stability and auditability.
- **go test**: Built-in test runner with coverage and race detection.
- **gofmt / go vet**: Standardized formatting and static analysis.

## Discarded

- **golangci-lint**: Too large for a single library; `go vet` provides sufficient basic analysis.
- **testify**: We strictly use the standard `testing` package to keep the module dependency-free.