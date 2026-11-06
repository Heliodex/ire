package main

import (
	"compress/gzip"
	"net/http"
	"strings"
)

// withCompression gzips JSON API responses when the browser accepts gzip. The queue and bundle payloads
// repeat check labels and ship fields across many rows, so they shrink by roughly nine tenths on the wire.
func (a *ireApp) withCompression(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !acceptsGzip(r) {
			next.ServeHTTP(w, r)

			return
		}

		writer := &gzipWriter{ResponseWriter: w}
		defer writer.close()

		next.ServeHTTP(writer, r)
	})
}

// acceptsGzip reports whether the request lists gzip as an accepted encoding.
func acceptsGzip(r *http.Request) bool {
	for part := range strings.SplitSeq(r.Header.Get("Accept-Encoding"), ",") {
		if strings.EqualFold(strings.TrimSpace(strings.SplitN(part, ";", 2)[0]), "gzip") {
			return true
		}
	}

	return false
}

// gzipWriter compresses only JSON bodies; redirects, files and static pages pass through untouched.
type gzipWriter struct {
	http.ResponseWriter

	gzip        *gzip.Writer
	compressing bool
	wroteHeader bool
}

func (w *gzipWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}

	w.wroteHeader = true
	w.compressing = strings.HasPrefix(w.Header().Get("Content-Type"), "application/json")

	if w.compressing {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		w.Header().Del("Content-Length")
		w.gzip = gzip.NewWriter(w.ResponseWriter)
	}

	w.ResponseWriter.WriteHeader(status)
}

func (w *gzipWriter) Write(data []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}

	if w.gzip == nil {
		return w.ResponseWriter.Write(data)
	}

	return w.gzip.Write(data)
}

// Flush forwards buffered bytes so a streamed JSON response is not held back by compression.
func (w *gzipWriter) Flush() {
	if w.gzip != nil {
		_ = w.gzip.Flush()
	}

	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// close writes the gzip trailer once the handler returns.
func (w *gzipWriter) close() {
	if w.gzip != nil {
		_ = w.gzip.Close()
	}
}
