package main

import (
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// jsonHandler writes a small JSON body the way app.WriteJSON does.
func jsonHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"items":[1,2,3]}`))
}

func TestCompressionGzipsJSON(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/programs/half-life/queue", nil)
	request.Header.Set("Accept-Encoding", "gzip, deflate, br")

	recorder := httptest.NewRecorder()

	(&ireApp{}).withCompression(http.HandlerFunc(jsonHandler)).ServeHTTP(recorder, request)

	if encoding := recorder.Header().Get("Content-Encoding"); encoding != "gzip" {
		t.Fatalf("Content-Encoding = %q, want gzip", encoding)
	}

	if vary := recorder.Header().Get("Vary"); vary != "Accept-Encoding" {
		t.Fatalf("Vary = %q, want Accept-Encoding", vary)
	}

	reader, err := gzip.NewReader(recorder.Body)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	defer reader.Close()

	body, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if string(body) != `{"items":[1,2,3]}` {
		t.Fatalf("body = %q", body)
	}
}

func TestCompressionLeavesPlainRequestsAlone(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/api/v1/programs/half-life/queue", nil)

	recorder := httptest.NewRecorder()

	(&ireApp{}).withCompression(http.HandlerFunc(jsonHandler)).ServeHTTP(recorder, request)

	if encoding := recorder.Header().Get("Content-Encoding"); encoding != "" {
		t.Fatalf("Content-Encoding = %q, want none", encoding)
	}

	if body := recorder.Body.String(); body != `{"items":[1,2,3]}` {
		t.Fatalf("body = %q", body)
	}
}

func TestCompressionSkipsNonJSON(t *testing.T) {
	page := func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<p>queue</p>"))
	}

	request := httptest.NewRequest(http.MethodGet, "/queue", nil)
	request.Header.Set("Accept-Encoding", "gzip")

	recorder := httptest.NewRecorder()

	(&ireApp{}).withCompression(http.HandlerFunc(page)).ServeHTTP(recorder, request)

	if encoding := recorder.Header().Get("Content-Encoding"); encoding != "" {
		t.Fatalf("Content-Encoding = %q, want none", encoding)
	}

	if body := recorder.Body.String(); body != "<p>queue</p>" {
		t.Fatalf("body = %q", body)
	}
}

func TestAcceptsGzip(t *testing.T) {
	cases := map[string]bool{
		"":                    false,
		"br":                  false,
		"gzip":                true,
		"deflate, gzip;q=1.0": true,
		"GZIP":                true,
	}

	for header, want := range cases {
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.Header.Set("Accept-Encoding", header)

		if got := acceptsGzip(request); got != want {
			t.Fatalf("acceptsGzip(%q) = %v, want %v", header, got, want)
		}
	}
}

// TestCompressionRoundTripsThroughClient checks the encoded response is still readable by a normal client.
func TestCompressionRoundTripsThroughClient(t *testing.T) {
	server := httptest.NewServer((&ireApp{}).withCompression(http.HandlerFunc(jsonHandler)))
	defer server.Close()

	request, err := http.NewRequest(http.MethodGet, server.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	request.Header.Set("Accept-Encoding", "gzip")

	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if !strings.Contains(string(body), `"items"`) {
		t.Fatalf("body = %q", body)
	}
}
