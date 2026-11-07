package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/Trysac/u109-llm-dev-proxy"
)

type safeBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}

func (s *safeBuffer) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf.Reset()
}

func TestRun_Usage(t *testing.T) {
	stdout := &safeBuffer{}
	stderr := &safeBuffer{}

	// Acceptance 8: Run([]string{"-mode", "bogus"}, stdout, stderr) returns 2
	code := llmdevproxy.Run([]string{"-mode", "bogus"}, stdout, stderr)
	if code != 2 {
		t.Fatalf("Run(-mode bogus) = %d, want 2; stderr: %s", code, stderr.String())
	}

	// Missing -upstream
	stderr.Reset()
	code = llmdevproxy.Run([]string{}, stdout, stderr)
	if code != 2 {
		t.Fatalf("Run() without upstream = %d, want 2", code)
	}
	if !strings.Contains(stderr.String(), "-upstream is required") {
		t.Fatalf("unexpected stderr message: %s", stderr.String())
	}

	// Invalid address failure
	stderr.Reset()
	code = llmdevproxy.Run([]string{
		"-upstream", "http://127.0.0.1:8788",
		"-dir", t.TempDir(),
		"-addr", "invalid address:port:extra",
	}, stdout, stderr)
	if code != 1 {
		t.Fatalf("Run() with invalid addr = %d, want 1", code)
	}
}

func TestRun_CleanStart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a process cannot send itself SIGTERM on Windows")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir := t.TempDir()
	stdout := &safeBuffer{}
	stderr := &safeBuffer{}

	done := make(chan int, 1)
	go func() {
		// Use 127.0.0.1:0 for dynamic port test if possible, or arbitrary open address
		c := llmdevproxy.Run([]string{
			"-upstream", srv.URL,
			"-dir", dir,
			"-mode", "passthrough",
			"-addr", "127.0.0.1:0",
			"-vary", "anthropic-version, x-custom",
		}, stdout, stderr)
		done <- c
	}()

	// Wait briefly for server output
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if strings.Contains(stdout.String(), "llm-dev-proxy listening on") {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !strings.Contains(stdout.String(), "llm-dev-proxy listening on") {
		t.Logf("stdout: %s", stdout.String())
	}
	p, err := os.FindProcess(os.Getpid())
	if err == nil {
		_ = p.Signal(syscall.SIGTERM)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("expected exit code 0 on SIGTERM, got %d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for Run to finish")
	}
}
