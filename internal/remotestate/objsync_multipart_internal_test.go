package remotestate

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// multipartMock mocks the object plane's chunked-upload sub-protocol
// (state-api-contract §3): objects/missing, start, per-part PUT, complete, and
// the single-shot PUT, recording what the client did.
type multipartMock struct {
	mu            sync.Mutex
	partSize      int
	parts         map[int][]byte
	startCalls    int
	completed     bool
	completeKind  string
	singleShotPut bool
}

func (m *multipartMock) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		writeEnv := func(status int, payload any) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": payload,
				"meta": map[string]any{"requestId": "req_test"},
			})
		}
		switch {
		case strings.HasSuffix(p, "/state/objects/missing") && r.Method == http.MethodPost:
			var req struct {
				Digests []string `json:"digests"`
			}
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &req)
			writeEnv(200, map[string]any{"missing": req.Digests})

		case strings.HasSuffix(p, "/uploads") && r.Method == http.MethodPost:
			m.mu.Lock()
			m.startCalls++
			ps := m.partSize
			m.mu.Unlock()
			writeEnv(201, map[string]any{"uploadId": "upl_test123", "partSize": ps})

		case strings.Contains(p, "/uploads/") && strings.Contains(p, "/parts/") && r.Method == http.MethodPut:
			n, err := strconv.Atoi(p[strings.LastIndex(p, "/")+1:])
			if err != nil {
				writeEnv(400, map[string]any{})
				return
			}
			body, _ := io.ReadAll(r.Body)
			m.mu.Lock()
			m.parts[n] = body
			m.mu.Unlock()
			writeEnv(200, map[string]any{"partNumber": n, "etag": fmt.Sprintf("etag-%d", n)})

		case strings.HasSuffix(p, "/complete") && r.Method == http.MethodPost:
			m.mu.Lock()
			m.completed = true
			m.completeKind = r.Header.Get("Orun-Object-Kind")
			m.mu.Unlock()
			writeEnv(200, map[string]any{"digest": "ok"})

		case r.Method == http.MethodPut && strings.Contains(p, "/state/objects/sha256:"):
			m.mu.Lock()
			m.singleShotPut = true
			m.mu.Unlock()
			writeEnv(201, map[string]any{})

		default:
			w.WriteHeader(404)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": "not_found", "message": "route: " + p},
			})
		}
	})
}

func (m *multipartMock) assembled() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	nums := make([]int, 0, len(m.parts))
	for n := range m.parts {
		nums = append(nums, n)
	}
	sort.Ints(nums)
	var out []byte
	for _, n := range nums {
		out = append(out, m.parts[n]...)
	}
	return out
}

func TestPutObjectMultipart_StartPartsComplete(t *testing.T) {
	m := &multipartMock{partSize: 4, parts: map[int][]byte{}}
	srv := httptest.NewServer(m.handler())
	defer srv.Close()
	c := NewClient(srv.URL, "test", NewStaticTokenSource("tok"))

	blob := []byte("0123456789") // 10 bytes, 4-byte parts → 4,4,2
	if err := c.putObjectMultipart(context.Background(), Digest(blob), ObjectKindPlan, blob); err != nil {
		t.Fatalf("putObjectMultipart: %v", err)
	}

	if m.startCalls != 1 {
		t.Fatalf("startCalls = %d, want 1", m.startCalls)
	}
	if len(m.parts) != 3 {
		t.Fatalf("parts = %d, want 3 (4+4+2)", len(m.parts))
	}
	if got := string(m.assembled()); got != string(blob) {
		t.Fatalf("assembled = %q, want %q (order/content)", got, blob)
	}
	if !m.completed {
		t.Fatalf("complete was not called")
	}
	if m.completeKind != ObjectKindPlan {
		t.Fatalf("complete kind = %q, want %q", m.completeKind, ObjectKindPlan)
	}
}

func TestEnsureObject_DispatchesBySize(t *testing.T) {
	// Lower the single-shot budget so a tiny blob exercises the multipart path
	// without a 25 MiB allocation.
	orig := singleShotMaxBytes
	singleShotMaxBytes = 8
	defer func() { singleShotMaxBytes = orig }()

	t.Run("large blob → multipart", func(t *testing.T) {
		m := &multipartMock{partSize: 4, parts: map[int][]byte{}}
		srv := httptest.NewServer(m.handler())
		defer srv.Close()
		c := NewClient(srv.URL, "test", NewStaticTokenSource("tok"))

		blob := []byte("over-the-budget-blob") // 20 bytes > 8
		digest, err := c.EnsureObject(context.Background(), ObjectKindPlan, blob)
		if err != nil {
			t.Fatalf("EnsureObject: %v", err)
		}
		if digest != Digest(blob) {
			t.Fatalf("digest = %q, want %q", digest, Digest(blob))
		}
		if m.startCalls != 1 || !m.completed {
			t.Fatalf("expected multipart (start=%d completed=%v)", m.startCalls, m.completed)
		}
		if m.singleShotPut {
			t.Fatalf("large blob must not single-shot PUT")
		}
		if got := string(m.assembled()); got != string(blob) {
			t.Fatalf("assembled = %q, want %q", got, blob)
		}
	})

	t.Run("small blob → single shot", func(t *testing.T) {
		m := &multipartMock{partSize: 4, parts: map[int][]byte{}}
		srv := httptest.NewServer(m.handler())
		defer srv.Close()
		c := NewClient(srv.URL, "test", NewStaticTokenSource("tok"))

		if _, err := c.EnsureObject(context.Background(), ObjectKindPlan, []byte("tiny")); err != nil {
			t.Fatalf("EnsureObject: %v", err)
		}
		if !m.singleShotPut {
			t.Fatalf("small blob should single-shot PUT")
		}
		if m.startCalls != 0 {
			t.Fatalf("small blob must not open a multipart upload (startCalls=%d)", m.startCalls)
		}
	})
}
