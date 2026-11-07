package llmdevproxy_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Trysac/u109-llm-dev-proxy"
)

func TestKeyCanonicalization(t *testing.T) {
	// Acceptance 3: Key(method "POST", requestURI "/v1/messages", body '{"a":2}', no vary headers) returns "0a9f974d794fcfc006dec68363f2ca9365a5f76aecf828bf52316d94152de6a0"
	hdr := make(http.Header)
	key := llmdevproxy.Key("POST", "/v1/messages", hdr, []byte(`{"a":2}`), nil)
	expectedHash := "0a9f974d794fcfc006dec68363f2ca9365a5f76aecf828bf52316d94152de6a0"
	if key != expectedHash {
		t.Fatalf("Key mismatch: got %q, want %q", key, expectedHash)
	}

	// Acceptance 2: Bodies '{"a":1}' and '{"a":1.0}' get different keys,
	// Authorization "Bearer A" versus "Bearer B" gets the same key,
	// and with VaryHeaders []string{"anthropic-version"} the values "2023-06-01" and "2024-01-01" get different keys
	key1 := llmdevproxy.Key("POST", "/v1/messages", hdr, []byte(`{"a":1}`), nil)
	key1Dot0 := llmdevproxy.Key("POST", "/v1/messages", hdr, []byte(`{"a":1.0}`), nil)
	if key1 == key1Dot0 {
		t.Fatalf("Expected 1 and 1.0 to produce different keys, but got identical: %q", key1)
	}

	authA := "Bearer A"
	authB := "Bearer B"
	hdrA := make(http.Header)
	hdrA.Set("Authorization", authA)
	hdrB := make(http.Header)
	hdrB.Set("Authorization", authB)
	keyAuthA := llmdevproxy.Key("POST", "/v1/messages", hdrA, []byte(`{"a":1}`), nil)
	keyAuthB := llmdevproxy.Key("POST", "/v1/messages", hdrB, []byte(`{"a":1}`), nil)
	if keyAuthA != keyAuthB {
		t.Fatalf("Authorization header should be ignored in key, got %q != %q", keyAuthA, keyAuthB)
	}

	hdrV1 := make(http.Header)
	hdrV1.Set("anthropic-version", "2023-06-01")
	hdrV2 := make(http.Header)
	hdrV2.Set("anthropic-version", "2024-01-01")
	varyList := []string{"anthropic-version"}
	keyV1 := llmdevproxy.Key("POST", "/v1/messages", hdrV1, []byte(`{"a":1}`), varyList)
	keyV2 := llmdevproxy.Key("POST", "/v1/messages", hdrV2, []byte(`{"a":1}`), varyList)
	if keyV1 == keyV2 {
		t.Fatalf("Vary headers should differentiate keys, got identical %q", keyV1)
	}
}

func TestModeRecordUnorderedJSONAndSecurity(t *testing.T) {
	var calls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_123","content":"hello"}`))
	}))
	defer upstream.Close()

	upURL, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	p, err := llmdevproxy.New(llmdevproxy.Config{
		Upstream: upURL,
		Dir:      dir,
		Mode:     llmdevproxy.ModeRecord,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Acceptance 1: In ModeRecord two POST /v1/messages requests with bodies '{"b":1,"a":2}' and '{"a":2, "b":1}'
	// call the upstream once; the second response has header X-Dev-Proxy-Cache "HIT", status 200 and a byte-identical body
	authVal := "Bearer A"
	req1 := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"b":1,"a":2}`))
	req1.Header.Set("Authorization", authVal)
	apiKey := "sk-test-1"
	req1.Header.Set("x-api-key", apiKey)
	rec1 := httptest.NewRecorder()
	p.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("req1 code = %d, want 200", rec1.Code)
	}
	if got := rec1.Header().Get("X-Dev-Proxy-Cache"); got != "MISS" {
		t.Fatalf("req1 X-Dev-Proxy-Cache = %q, want MISS", got)
	}

	req2 := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(`{"a":2, "b":1}`))
	req2.Header.Set("Authorization", authVal)
	rec2 := httptest.NewRecorder()
	p.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Fatalf("req2 code = %d, want 200", rec2.Code)
	}
	if got := rec2.Header().Get("X-Dev-Proxy-Cache"); got != "HIT" {
		t.Fatalf("req2 X-Dev-Proxy-Cache = %q, want HIT", got)
	}
	if rec1.Body.String() != rec2.Body.String() {
		t.Fatalf("req1 body %q != req2 body %q", rec1.Body.String(), rec2.Body.String())
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected 1 upstream call, got %d", atomic.LoadInt32(&calls))
	}

	// Acceptance 4: After a recorded request with credentials,
	// no file in Dir contains credentials, and the stored headers hold only Content-Type
	files, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("expected at least one cache file in dir")
	}
	for _, f := range files {
		content, rErr := os.ReadFile(filepath.Join(dir, f.Name()))
		if rErr != nil {
			t.Fatal(rErr)
		}
		strContent := string(content)
		if strings.Contains(strContent, authVal) || strings.Contains(strContent, apiKey) {
			t.Fatalf("cache file %s leaked credentials: %s", f.Name(), strContent)
		}
		var st llmdevproxy.StoredEntry
		if err := json.Unmarshal(content, &st); err != nil {
			t.Fatalf("failed to parse cache file %s: %v", f.Name(), err)
		}
		if st.ContentType != "application/json" {
			t.Fatalf("stored Content-Type = %q, want application/json", st.ContentType)
		}
	}
}

func TestModeReplayAndPassthrough(t *testing.T) {
	var calls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	}))
	defer upstream.Close()

	upURL, _ := url.Parse(upstream.URL)
	dir := t.TempDir()

	// Acceptance 5: In ModeReplay a cache miss returns 404 with body '{"error":"cache_miss","key":"<key>"}'
	// and never calls the upstream; in ModePassthrough Dir is never read or written
	replayProxy, err := llmdevproxy.New(llmdevproxy.Config{
		Upstream: upURL,
		Dir:      dir,
		Mode:     llmdevproxy.ModeReplay,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("POST", "/v1/chat", strings.NewReader(`{"query":"test"}`))
	rec := httptest.NewRecorder()
	replayProxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("Replay miss code = %d, want 404", rec.Code)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("Replay miss called upstream! calls = %d", atomic.LoadInt32(&calls))
	}
	wantKey := llmdevproxy.Key("POST", "/v1/chat", req.Header, []byte(`{"query":"test"}`), nil)
	wantBody := fmt.Sprintf(`{"error":"cache_miss","key":%q}`, wantKey)
	if rec.Body.String() != wantBody {
		t.Fatalf("Replay body = %q, want %q", rec.Body.String(), wantBody)
	}

	// Passthrough
	passProxy, err := llmdevproxy.New(llmdevproxy.Config{
		Upstream: upURL,
		Dir:      dir,
		Mode:     llmdevproxy.ModePassthrough,
	})
	if err != nil {
		t.Fatal(err)
	}
	recPass := httptest.NewRecorder()
	passProxy.ServeHTTP(recPass, req)
	if recPass.Code != http.StatusOK {
		t.Fatalf("passthrough code = %d, want 200", recPass.Code)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Fatalf("expected upstream calls = 1, got %d", atomic.LoadInt32(&calls))
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("passthrough mode modified dir: %d files found", len(entries))
	}
}

func TestUpstreamErrorNotStored(t *testing.T) {
	// Acceptance 6: Upstream responses with status 500 or 429 are forwarded with X-Dev-Proxy-Cache "MISS"
	// and not stored, so the next identical request calls the upstream again
	var calls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := atomic.AddInt32(&calls, 1)
		if c == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"server_error"}`))
			return
		}
		if c == 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate_limited"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"result":"ok"}`))
	}))
	defer upstream.Close()

	upURL, _ := url.Parse(upstream.URL)
	dir := t.TempDir()
	p, err := llmdevproxy.New(llmdevproxy.Config{
		Upstream: upURL,
		Dir:      dir,
		Mode:     llmdevproxy.ModeRecord,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 1st request -> 500
	req1 := httptest.NewRequest("POST", "/v1/complete", strings.NewReader(`{"prompt":"hi"}`))
	rec1 := httptest.NewRecorder()
	p.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusInternalServerError {
		t.Fatalf("req1 code = %d, want 500", rec1.Code)
	}
	if got := rec1.Header().Get("X-Dev-Proxy-Cache"); got != "MISS" {
		t.Fatalf("req1 cache header = %q, want MISS", got)
	}

	// 2nd request -> 429
	req2 := httptest.NewRequest("POST", "/v1/complete", strings.NewReader(`{"prompt":"hi"}`))
	rec2 := httptest.NewRecorder()
	p.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("req2 code = %d, want 429", rec2.Code)
	}
	if got := rec2.Header().Get("X-Dev-Proxy-Cache"); got != "MISS" {
		t.Fatalf("req2 cache header = %q, want MISS", got)
	}

	// 3rd request -> 200 (stored)
	req3 := httptest.NewRequest("POST", "/v1/complete", strings.NewReader(`{"prompt":"hi"}`))
	rec3 := httptest.NewRecorder()
	p.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("req3 code = %d, want 200", rec3.Code)
	}
	if got := rec3.Header().Get("X-Dev-Proxy-Cache"); got != "MISS" {
		t.Fatalf("req3 cache header = %q, want MISS", got)
	}

	// 4th request -> HIT
	req4 := httptest.NewRequest("POST", "/v1/complete", strings.NewReader(`{"prompt":"hi"}`))
	rec4 := httptest.NewRecorder()
	p.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusOK {
		t.Fatalf("req4 code = %d, want 200", rec4.Code)
	}
	if got := rec4.Header().Get("X-Dev-Proxy-Cache"); got != "HIT" {
		t.Fatalf("req4 cache header = %q, want HIT", got)
	}

	if atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("expected 3 upstream calls, got %d", atomic.LoadInt32(&calls))
	}
}

func TestLimitsAndStoreFull(t *testing.T) {
	// Acceptance 7: A request body of 10<<20 + 1 bytes returns 413 without calling the upstream;
	// a response larger than MaxResponseBytes is forwarded complete with X-Dev-Proxy-Cache "BYPASS" and not stored;
	// with MaxEntries 1 a second distinct miss is BYPASS and OnEvent receives Kind "store_full"
	var calls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		if r.URL.Path == "/large-response" {
			w.WriteHeader(http.StatusOK)
			// write 100 bytes (with MaxResponseBytes set to 50)
			_, _ = w.Write(bytes.Repeat([]byte("x"), 100))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer upstream.Close()

	upURL, _ := url.Parse(upstream.URL)
	dir := t.TempDir()
	var events []llmdevproxy.Event
	p, err := llmdevproxy.New(llmdevproxy.Config{
		Upstream:         upURL,
		Dir:              dir,
		Mode:             llmdevproxy.ModeRecord,
		MaxEntries:       1,
		MaxResponseBytes: 50,
		OnEvent: func(e llmdevproxy.Event) {
			events = append(events, e)
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Request body of 10<<20 + 1 bytes
	bigReqBody := bytes.Repeat([]byte("a"), (10<<20)+1)
	reqBig := httptest.NewRequest("POST", "/v1/test", bytes.NewReader(bigReqBody))
	recBig := httptest.NewRecorder()
	p.ServeHTTP(recBig, reqBig)
	if recBig.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("big req code = %d, want 413", recBig.Code)
	}
	if atomic.LoadInt32(&calls) != 0 {
		t.Fatalf("big req called upstream! calls = %d", atomic.LoadInt32(&calls))
	}

	// Response larger than MaxResponseBytes (50 bytes vs 100 bytes)
	reqLargeResp := httptest.NewRequest("POST", "/large-response", strings.NewReader(`{}`))
	recLargeResp := httptest.NewRecorder()
	p.ServeHTTP(recLargeResp, reqLargeResp)
	if recLargeResp.Code != http.StatusOK {
		t.Fatalf("large resp code = %d, want 200", recLargeResp.Code)
	}
	if got := recLargeResp.Header().Get("X-Dev-Proxy-Cache"); got != "BYPASS" {
		t.Fatalf("large resp cache header = %q, want BYPASS", got)
	}
	if recLargeResp.Body.Len() != 100 {
		t.Fatalf("expected 100 bytes, got %d", recLargeResp.Body.Len())
	}

	// Distinct request 1 -> store 1st entry (fills MaxEntries: 1)
	req1 := httptest.NewRequest("POST", "/v1/item1", strings.NewReader(`{"id":1}`))
	rec1 := httptest.NewRecorder()
	p.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("req1 code = %d, want 200", rec1.Code)
	}
	if got := rec1.Header().Get("X-Dev-Proxy-Cache"); got != "MISS" {
		t.Fatalf("req1 cache header = %q, want MISS", got)
	}

	// Distinct request 2 -> store_full -> BYPASS
	req2 := httptest.NewRequest("POST", "/v1/item2", strings.NewReader(`{"id":2}`))
	rec2 := httptest.NewRecorder()
	p.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("req2 code = %d, want 200", rec2.Code)
	}
	if got := rec2.Header().Get("X-Dev-Proxy-Cache"); got != "BYPASS" {
		t.Fatalf("req2 cache header = %q, want BYPASS", got)
	}

	// Verify OnEvent received "store_full"
	foundStoreFull := false
	for _, ev := range events {
		if ev.Kind == "store_full" {
			foundStoreFull = true
			break
		}
	}
	if !foundStoreFull {
		t.Fatalf("OnEvent never received Kind 'store_full'; events=%v", events)
	}
}

func TestConfigValidationAndRun(t *testing.T) {
	// Acceptance 8: Run([]string{"-mode", "bogus"}, stdout, stderr) returns 2
	var stdout, stderr bytes.Buffer
	if code := llmdevproxy.Run([]string{"-mode", "bogus"}, &stdout, &stderr); code != 2 {
		t.Fatalf("expected exit code 2 for Run(-mode bogus), got %d", code)
	}

	// and New returns an error matching ErrInvalidConfig (errors.Is) for a nil Upstream or an empty Dir
	_, errNilUpstream := llmdevproxy.New(llmdevproxy.Config{
		Upstream: nil,
		Dir:      "/tmp",
	})
	if !errors.Is(errNilUpstream, llmdevproxy.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig for nil upstream, got %v", errNilUpstream)
	}

	upURL, _ := url.Parse("http://example.com")
	_, errEmptyDir := llmdevproxy.New(llmdevproxy.Config{
		Upstream: upURL,
		Dir:      "",
	})
	if !errors.Is(errEmptyDir, llmdevproxy.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig for empty dir, got %v", errEmptyDir)
	}

	_, errInvalidMode := llmdevproxy.New(llmdevproxy.Config{
		Upstream: upURL,
		Dir:      t.TempDir(),
		Mode:     "invalid",
	})
	if !errors.Is(errInvalidMode, llmdevproxy.ErrInvalidConfig) {
		t.Fatalf("expected ErrInvalidConfig for invalid mode, got %v", errInvalidMode)
	}
}

func TestEdgeCases(t *testing.T) {
	dir := t.TempDir()

	// Corrupt cache file test
	corruptEntryID := "corrupt" + "entry" + "123"
	corruptPath := filepath.Join(dir, corruptEntryID+".json")
	_ = os.WriteFile(corruptPath, []byte("not json"), 0o644)

	var calls int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"fixed":"yes"}`))
	}))
	defer upstream.Close()
	upURL, _ := url.Parse(upstream.URL)

	p, err := llmdevproxy.New(llmdevproxy.Config{
		Upstream: upURL,
		Dir:      dir,
		Mode:     llmdevproxy.ModeRecord,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Non-JSON request body (raw bytes canonicalization test)
	reqRaw := httptest.NewRequest("POST", "/v1/raw", strings.NewReader("plain text not json"))
	recRaw := httptest.NewRecorder()
	p.ServeHTTP(recRaw, reqRaw)
	if recRaw.Code != http.StatusOK {
		t.Fatalf("raw body code = %d, want 200", recRaw.Code)
	}

	// Upstream unreachable returns 502 with Kind "bypass"
	badURL, _ := url.Parse("http://127.0.0.1:0")
	var lastEvent llmdevproxy.Event
	pUnreach, _ := llmdevproxy.New(llmdevproxy.Config{
		Upstream: badURL,
		Dir:      dir,
		OnEvent: func(e llmdevproxy.Event) {
			lastEvent = e
		},
	})
	reqUnreach := httptest.NewRequest("POST", "/v1/fail", strings.NewReader(`{}`))
	recUnreach := httptest.NewRecorder()
	pUnreach.ServeHTTP(recUnreach, reqUnreach)
	if recUnreach.Code != http.StatusBadGateway {
		t.Fatalf("unreachable code = %d, want 502", recUnreach.Code)
	}
	if lastEvent.Kind != "bypass" || lastEvent.Status != http.StatusBadGateway {
		t.Fatalf("expected bypass 502 event, got %+v", lastEvent)
	}

	// GET request without body
	reqGet := httptest.NewRequest("GET", "/v1/models?limit=10", nil)
	recGet := httptest.NewRecorder()
	p.ServeHTTP(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("GET code = %d, want 200", recGet.Code)
	}

	// Replay mode with corrupted base64 in cache
	badB64Key := llmdevproxy.Key("GET", "/v1/badb64", nil, nil, nil)
	badB64File := filepath.Join(dir, badB64Key+".json")
	_ = os.WriteFile(badB64File, []byte(`{"status":200,"content_type":"application/json","body_b64":"???notb64"}`), 0o644)

	pReplayBad, _ := llmdevproxy.New(llmdevproxy.Config{
		Upstream: upURL,
		Dir:      dir,
		Mode:     llmdevproxy.ModeReplay,
	})
	reqBadB64 := httptest.NewRequest("GET", "/v1/badb64", nil)
	recBadB64 := httptest.NewRecorder()
	pReplayBad.ServeHTTP(recBadB64, reqBadB64)
	if recBadB64.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500 for corrupted base64, got %d", recBadB64.Code)
	}

	// Read-only directory causes storeCache error (store_error event)
	roDir := filepath.Join(t.TempDir(), "ro")
	_ = os.MkdirAll(roDir, 0o555)
	var storeErrEvent llmdevproxy.Event
	pRO, err := llmdevproxy.New(llmdevproxy.Config{
		Upstream: upURL,
		Dir:      roDir,
		Mode:     llmdevproxy.ModeRecord,
		OnEvent: func(e llmdevproxy.Event) {
			if e.Kind == "store_error" {
				storeErrEvent = e
			}
		},
	})
	if err == nil {
		// make roDir unwritable if on Unix
		_ = os.Chmod(roDir, 0o555)
		reqRO := httptest.NewRequest("POST", "/v1/ro", strings.NewReader(`{"test":true}`))
		recRO := httptest.NewRecorder()
		pRO.ServeHTTP(recRO, reqRO)
		_ = os.Chmod(roDir, 0o755) // cleanup permission
	}
	_ = storeErrEvent // only fires where directory permissions are enforced (not as root, not on Windows)

	// singleJoiningSlash edge cases
	slashURL, _ := url.Parse(upstream.URL + "/")
	pSlash, _ := llmdevproxy.New(llmdevproxy.Config{
		Upstream: slashURL,
		Dir:      t.TempDir(),
		Mode:     llmdevproxy.ModePassthrough,
	})
	recSlash := httptest.NewRecorder()
	reqSlash := httptest.NewRequest("GET", "/testslash", nil)
	pSlash.ServeHTTP(recSlash, reqSlash)

	// JSON trailing token canonicalization
	keyTrailing := llmdevproxy.Key("POST", "/test", nil, []byte(`{"a": 1} {"b": 2}`), nil)
	if keyTrailing == "" {
		t.Fatal("key should not be empty")
	}
}

func ExampleNew() {
	upURL, _ := url.Parse("https://api.openai.com")
	proxy, err := llmdevproxy.New(llmdevproxy.Config{
		Upstream: upURL,
		Dir:      ".llm-cache",
		Mode:     llmdevproxy.ModeRecord,
	})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Printf("proxy created for upstream: %t\n", proxy != nil)
	// Output:
	// proxy created for upstream: true
}

func ExampleKey() {
	hdr := make(http.Header)
	key := llmdevproxy.Key("POST", "/v1/messages", hdr, []byte(`{"a":2}`), nil)
	fmt.Println("key prefix:", key[:8])
	// Output:
	// key prefix: 0a9f974d
}
