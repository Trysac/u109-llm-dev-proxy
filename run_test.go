package llmdevproxy_test

import (
	"bytes"
	"net"
	"strings"
	"testing"

	"github.com/Trysac/u109-llm-dev-proxy"
)

func TestRunRejectsBadArguments(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"unknown flag", []string{"-nope"}, ""},
		{"missing upstream", []string{}, "-upstream is required"},
		{"invalid upstream", []string{"-upstream", "not-a-url"}, "invalid -upstream URL"},
		{"invalid mode", []string{"-upstream", "http://127.0.0.1:1", "-mode", "bogus", "-dir", t.TempDir()}, "error:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := llmdevproxy.Run(tc.args, &stdout, &stderr); code != 2 {
				t.Fatalf("exit code = %d, want 2 (stderr %q)", code, stderr.String())
			}
			if !strings.Contains(stderr.String(), tc.want) {
				t.Fatalf("stderr %q does not contain %q", stderr.String(), tc.want)
			}
		})
	}
}

func TestRunReturnsOneWhenTheAddressIsTaken(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var stdout, stderr bytes.Buffer
	args := []string{"-upstream", "http://127.0.0.1:1", "-addr", ln.Addr().String(), "-dir", t.TempDir(), "-vary", "X-A, ,X-B"}
	if code := llmdevproxy.Run(args, &stdout, &stderr); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stdout.String(), "listening on") {
		t.Fatalf("stdout %q has no listening line", stdout.String())
	}
}
