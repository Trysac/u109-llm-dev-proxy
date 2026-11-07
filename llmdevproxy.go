package llmdevproxy

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Mode defines proxy operational caching behavior.
type Mode string

const (
	// ModeRecord looks for cached responses, calls upstream on miss, and caches status 200 responses.
	ModeRecord Mode = "record"
	// ModeReplay returns cached responses and yields 404 cache_miss without calling upstream.
	ModeReplay Mode = "replay"
	// ModePassthrough proxies directly to upstream without reading or writing cache entries.
	ModePassthrough Mode = "passthrough"
)

// ErrInvalidConfig is returned when Config fails validation.
var ErrInvalidConfig = errors.New("llmdevproxy: invalid config")

const (
	defaultMaxRequestBytes  int64 = 10 << 20
	defaultMaxResponseBytes int64 = 50 << 20
	defaultMaxEntries       int   = 10000
)

// Event notifies listeners of proxy actions.
type Event struct {
	Kind   string // "hit", "miss", "stored", "bypass", "store_full", "replay_miss", "store_error"
	Key    string
	Status int
}

// Config specifies options to configure a Proxy instance.
type Config struct {
	Upstream         *url.URL
	Dir              string
	Mode             Mode
	VaryHeaders      []string
	MaxRequestBytes  int64
	MaxResponseBytes int64
	MaxEntries       int
	Transport        http.RoundTripper
	OnEvent          func(Event)
}

// Proxy is an HTTP handler caching LLM requests on disk.
type Proxy struct {
	cfg        Config
	transport  http.RoundTripper
	mu         sync.Mutex
	entryCount int
}

// StoredEntry represents the disk cache format.
type StoredEntry struct {
	Status      int    `json:"status"`
	ContentType string `json:"content_type"`
	BodyB64     string `json:"body_b64"`
}

// Hop-by-hop headers to drop when proxying.
var hopHeaders = map[string]bool{
	"connection":          true,
	"keep-alive":          true,
	"proxy-authenticate":  true,
	"proxy-authorization": true,
	"te":                  true,
	"trailer":             true,
	"transfer-encoding":   true,
	"upgrade":             true,
}

// New creates and initializes a Proxy.
func New(cfg Config) (*Proxy, error) {
	if cfg.Upstream == nil || cfg.Dir == "" {
		return nil, fmt.Errorf("%w: upstream and dir are required", ErrInvalidConfig)
	}

	switch cfg.Mode {
	case "":
		cfg.Mode = ModeRecord
	case ModeRecord, ModeReplay, ModePassthrough:
	default:
		return nil, fmt.Errorf("%w: unknown mode %q", ErrInvalidConfig, cfg.Mode)
	}

	if cfg.MaxRequestBytes <= 0 {
		cfg.MaxRequestBytes = defaultMaxRequestBytes
	}
	if cfg.MaxResponseBytes <= 0 {
		cfg.MaxResponseBytes = defaultMaxResponseBytes
	}
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = defaultMaxEntries
	}

	if err := os.MkdirAll(cfg.Dir, 0o755); err != nil {
		return nil, fmt.Errorf("%w: cannot create dir %s: %v", ErrInvalidConfig, cfg.Dir, err)
	}

	count := 0
	entries, err := os.ReadDir(cfg.Dir)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
				count++
			}
		}
	}

	transport := cfg.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}

	return &Proxy{
		cfg:        cfg,
		transport:  transport,
		entryCount: count,
	}, nil
}

func (p *Proxy) emit(kind, key string, status int) {
	if p.cfg.OnEvent != nil {
		p.cfg.OnEvent(Event{
			Kind:   kind,
			Key:    key,
			Status: status,
		})
	}
}

// Key computes the deterministic cache key for a request.
func Key(method, requestURI string, header http.Header, body []byte, vary []string) string {
	var b strings.Builder
	b.WriteString(method)
	b.WriteString("\n")
	b.WriteString(requestURI)
	b.WriteString("\n")

	if len(vary) > 0 {
		seen := make(map[string]bool, len(vary))
		var sortedVary []string
		for _, v := range vary {
			lower := strings.ToLower(strings.TrimSpace(v))
			if lower == "" || seen[lower] {
				continue
			}
			// Ignore sensitive headers
			if lower == "authorization" || lower == "x-api-key" || lower == "x-goog-api-key" || lower == "cookie" {
				continue
			}
			seen[lower] = true
			sortedVary = append(sortedVary, lower)
		}
		sort.Strings(sortedVary)

		for _, name := range sortedVary {
			val := header.Get(name)
			if val != "" {
				b.WriteString(name)
				b.WriteString(":")
				b.WriteString(val)
				b.WriteString("\n")
			}
		}
	}
	b.WriteString("\n")

	canonicalBody := canonicalizeJSON(body)
	h := sha256.New()
	h.Write([]byte(b.String()))
	h.Write(canonicalBody)
	return hex.EncodeToString(h.Sum(nil))
}

func canonicalizeJSON(data []byte) []byte {
	if len(bytes.TrimSpace(data)) == 0 {
		return data
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var val interface{}
	if err := dec.Decode(&val); err != nil {
		return data
	}
	// Ensure no trailing tokens
	if dec.More() {
		return data
	}
	var trailing json.RawMessage
	if err := dec.Decode(&trailing); err != io.EOF {
		return data
	}

	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(val); err != nil {
		return data
	}
	res := buf.Bytes()
	if len(res) > 0 && res[len(res)-1] == '\n' {
		res = res[:len(res)-1]
	}
	return res
}

func (p *Proxy) cacheFilePath(key string) string {
	return filepath.Join(p.cfg.Dir, key+".json")
}

func (p *Proxy) loadCache(key string) (*StoredEntry, error) {
	data, err := os.ReadFile(p.cacheFilePath(key))
	if err != nil {
		return nil, err
	}
	var entry StoredEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil, err
	}
	return &entry, nil
}

func (p *Proxy) storeCache(key string, status int, contentType string, body []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	path := p.cacheFilePath(key)
	_, existsErr := os.Stat(path)
	if os.IsNotExist(existsErr) {
		if p.entryCount >= p.cfg.MaxEntries {
			p.emit("store_full", key, status)
			return errors.New("store_full")
		}
	}

	entry := StoredEntry{
		Status:      status,
		ContentType: contentType,
		BodyB64:     base64.StdEncoding.EncodeToString(body),
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		p.emit("store_error", key, status)
		return err
	}

	tmp, err := os.CreateTemp(p.cfg.Dir, "tmp-entry-*.json")
	if err != nil {
		p.emit("store_error", key, status)
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		p.emit("store_error", key, status)
		return err
	}
	if err := tmp.Close(); err != nil {
		p.emit("store_error", key, status)
		return err
	}

	if err := os.Rename(tmpName, path); err != nil {
		p.emit("store_error", key, status)
		return err
	}

	if os.IsNotExist(existsErr) {
		p.entryCount++
	}
	p.emit("stored", key, status)
	return nil
}

// ServeHTTP handles incoming requests.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Check request body size
	bodyReader := io.LimitReader(r.Body, p.cfg.MaxRequestBytes+1)
	bodyBytes, err := io.ReadAll(bodyReader)
	if err != nil {
		http.Error(w, `{"error":"read_request_failed"}`, http.StatusBadRequest)
		return
	}
	if int64(len(bodyBytes)) > p.cfg.MaxRequestBytes {
		http.Error(w, `{"error":"request_entity_too_large"}`, http.StatusRequestEntityTooLarge)
		return
	}

	reqURI := r.RequestURI
	if reqURI == "" {
		reqURI = r.URL.RequestURI()
	}

	if p.cfg.Mode == ModePassthrough {
		p.forward(w, r, reqURI, bodyBytes, "", false)
		return
	}

	key := Key(r.Method, reqURI, r.Header, bodyBytes, p.cfg.VaryHeaders)

	if p.cfg.Mode == ModeReplay {
		entry, err := p.loadCache(key)
		if err != nil {
			p.emit("replay_miss", key, http.StatusNotFound)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			_, _ = fmt.Fprintf(w, `{"error":"cache_miss","key":%q}`, key)
			return
		}
		p.respondWithEntry(w, entry, key)
		return
	}

	// ModeRecord
	entry, err := p.loadCache(key)
	if err == nil && entry != nil {
		p.respondWithEntry(w, entry, key)
		return
	}

	p.forward(w, r, reqURI, bodyBytes, key, true)
}

func (p *Proxy) respondWithEntry(w http.ResponseWriter, entry *StoredEntry, key string) {
	dec, err := base64.StdEncoding.DecodeString(entry.BodyB64)
	if err != nil {
		http.Error(w, `{"error":"corrupted_cache"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("X-Dev-Proxy-Cache", "HIT")
	if entry.ContentType != "" {
		w.Header().Set("Content-Type", entry.ContentType)
	}
	w.WriteHeader(entry.Status)
	_, _ = w.Write(dec)
	p.emit("hit", key, entry.Status)
}

func (p *Proxy) forward(w http.ResponseWriter, r *http.Request, reqURI string, bodyBytes []byte, key string, canRecord bool) {
	relURL, err := url.Parse(reqURI)
	if err != nil {
		http.Error(w, `{"error":"invalid_request_uri"}`, http.StatusBadRequest)
		return
	}

	outURL := *p.cfg.Upstream
	outURL.Path = singleJoiningSlash(outURL.Path, relURL.Path)
	outURL.RawQuery = relURL.RawQuery
	outURL.Fragment = relURL.Fragment

	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, outURL.String(), bytes.NewReader(bodyBytes))
	if err != nil {
		http.Error(w, `{"error":"create_request_failed"}`, http.StatusInternalServerError)
		return
	}

	for k, vv := range r.Header {
		lower := strings.ToLower(k)
		if hopHeaders[lower] {
			continue
		}
		for _, v := range vv {
			outReq.Header.Add(k, v)
		}
	}
	outReq.Host = p.cfg.Upstream.Host

	resp, err := p.transport.RoundTrip(outReq)
	if err != nil {
		p.emit("bypass", key, http.StatusBadGateway)
		http.Error(w, `{"error":"upstream_failed"}`, http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	copyHeaders := func(headerVal string) {
		for k, vv := range resp.Header {
			lower := strings.ToLower(k)
			if hopHeaders[lower] {
				continue
			}
			for _, v := range vv {
				w.Header().Add(k, v)
			}
		}
		if headerVal != "" {
			w.Header().Set("X-Dev-Proxy-Cache", headerVal)
		}
	}

	if !canRecord {
		copyHeaders("")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		p.emit("bypass", key, resp.StatusCode)
		return
	}

	if resp.StatusCode != http.StatusOK {
		copyHeaders("MISS")
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
		p.emit("miss", key, resp.StatusCode)
		return
	}

	// Status is 200: buffer up to MaxResponseBytes + 1
	limit := p.cfg.MaxResponseBytes + 1
	lr := io.LimitReader(resp.Body, limit)
	respBuf, err := io.ReadAll(lr)
	if err != nil {
		http.Error(w, `{"error":"read_upstream_response_failed"}`, http.StatusBadGateway)
		return
	}

	if int64(len(respBuf)) > p.cfg.MaxResponseBytes {
		// Response is too large to cache; stream remainder and mark BYPASS
		copyHeaders("BYPASS")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBuf)
		_, _ = io.Copy(w, resp.Body)
		p.emit("bypass", key, resp.StatusCode)
		return
	}

	contentType := resp.Header.Get("Content-Type")
	if err := p.storeCache(key, resp.StatusCode, contentType, respBuf); err != nil {
		copyHeaders("BYPASS")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(respBuf)
		return
	}

	copyHeaders("MISS")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(respBuf)
	p.emit("miss", key, resp.StatusCode)
}

func singleJoiningSlash(a, b string) string {
	aslash := strings.HasSuffix(a, "/")
	bslash := strings.HasPrefix(b, "/")
	switch {
	case aslash && bslash:
		return a + b[1:]
	case !aslash && !bslash:
		return a + "/" + b
	}
	return a + b
}
