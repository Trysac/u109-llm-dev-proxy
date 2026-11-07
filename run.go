package llmdevproxy

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

// Run parses CLI flags and starts the dev proxy server. Returns 0 on clean exit, 2 on usage/config error.
func Run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("llm-dev-proxy", flag.ContinueOnError)
	fs.SetOutput(stderr)

	upstreamStr := fs.String("upstream", "", "Upstream LLM provider URL (required)")
	dir := fs.String("dir", ".llm-cache", "Directory to store cached responses")
	mode := fs.String("mode", "record", "Proxy mode: record, replay, or passthrough")
	addr := fs.String("addr", "127.0.0.1:8788", "Address to listen on")
	vary := fs.String("vary", "", "Comma-separated list of headers to vary on")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *upstreamStr == "" {
		fmt.Fprintln(stderr, "error: -upstream is required")
		return 2
	}

	upURL, err := url.Parse(*upstreamStr)
	if err != nil || upURL.Scheme == "" || upURL.Host == "" {
		fmt.Fprintf(stderr, "error: invalid -upstream URL: %v\n", *upstreamStr)
		return 2
	}

	var varyHeaders []string
	if *vary != "" {
		for _, v := range strings.Split(*vary, ",") {
			if trimmed := strings.TrimSpace(v); trimmed != "" {
				varyHeaders = append(varyHeaders, trimmed)
			}
		}
	}

	proxy, err := New(Config{
		Upstream:    upURL,
		Dir:         *dir,
		Mode:        Mode(*mode),
		VaryHeaders: varyHeaders,
		OnEvent: func(ev Event) {
			fmt.Fprintf(stdout, "[%s] key=%s status=%d\n", ev.Kind, ev.Key, ev.Status)
		},
	})
	if err != nil {
		fmt.Fprintf(stderr, "error: %v\n", err)
		return 2
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           proxy,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	fmt.Fprintf(stdout, "llm-dev-proxy listening on %s -> %s (mode: %s)\n", *addr, *upstreamStr, *mode)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	select {
	case <-sigCh:
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		return 0
	case err := <-errCh:
		if err != nil {
			log.Printf("server error: %v", err)
			return 1
		}
		return 0
	}
}
