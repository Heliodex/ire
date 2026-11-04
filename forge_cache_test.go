package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// TestForgeCacheReusesResponses covers the per-resource cache and the explicit clear.
func TestForgeCacheReusesResponses(t *testing.T) {
	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Type", "application/json")

		switch {
		case strings.HasSuffix(r.URL.Path, "/readme"):
			_, _ = io.WriteString(w, strings.Repeat("Build notes and wiring details. ", 12))
		case strings.HasSuffix(r.URL.Path, "/git/trees/main"):
			_, _ = io.WriteString(w, `{"sha":"head1","tree":[{"path":"README.md","type":"blob"}]}`)
		case strings.HasSuffix(r.URL.Path, "/commits"):
			_, _ = io.WriteString(w, `[{"sha":"sha1","commit":{"author":{"name":"Maker","date":"2026-11-01T10:00:00Z"},"message":"First"}}]`)
		default:
			_, _ = io.WriteString(w, `{"default_branch":"main","full_name":"example/cache","private":false}`)
		}
	}))
	t.Cleanup(server.Close)

	application := newTestApp(t)
	application.config.GitHubAPIBase = server.URL

	if _, evidence := application.repoChecks(t.Context(), "https://github.com/example/cache", "", application.now(), nil); evidence == nil {
		t.Fatal("first fetch returned no evidence")
	}

	first := calls.Load()
	if first == 0 {
		t.Fatal("no requests were made")
	}

	if _, evidence := application.repoChecks(t.Context(), "https://github.com/example/cache", "", application.now(), nil); evidence == nil {
		t.Fatal("cached fetch returned no evidence")
	}

	if calls.Load() != first {
		t.Errorf("cached fetch made %d more requests", calls.Load()-first)
	}

	application.forgeCache.clear()

	if _, evidence := application.repoChecks(t.Context(), "https://github.com/example/cache", "", application.now(), nil); evidence == nil {
		t.Fatal("cleared fetch returned no evidence")
	}

	if calls.Load() != first*2 {
		t.Errorf("requests after clear = %d, want %d", calls.Load(), first*2)
	}
}

// TestForgeCacheCachesUnavailable covers the short negative cache for a failing forge.
func TestForgeCacheCachesUnavailable(t *testing.T) {
	var calls atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)

	application := newTestApp(t)
	application.config.GitHubAPIBase = server.URL

	for range 2 {
		if checks, _ := application.repoChecks(t.Context(), "https://github.com/example/down", "", application.now(), nil); len(checks) == 0 {
			t.Fatal("no failure check")
		}
	}

	if calls.Load() != 1 {
		t.Errorf("requests = %d, want 1 while the failure is cached", calls.Load())
	}
}
