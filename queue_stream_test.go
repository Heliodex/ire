package main

import (
	"bufio"
	"bytes"
	"encoding/json/v2"
	"net/http"
	"testing"

	"ire/contract"
)

// TestQueueStream covers the NDJSON order, the batch size and the checks attached to every ship.
func TestQueueStream(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	recorder := do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all&limit=50&stream=1", nil, cookie, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("stream = %d: %s", recorder.Code, recorder.Body.String())
	}

	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/x-ndjson" {
		t.Fatalf("Content-Type = %q, want application/x-ndjson", contentType)
	}

	var (
		meta     bool
		hasMore  bool
		items    []contract.QueueItem
		counts   *contract.QueueCounts
		finished bool
	)

	scanner := bufio.NewScanner(bytes.NewReader(recorder.Body.Bytes()))

	for scanner.Scan() {
		var header struct {
			Type string `json:"type"`
		}

		if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
			t.Fatalf("decode line %q: %v", scanner.Text(), err)
		}

		switch header.Type {
		case "meta":
			var event queueStreamMeta
			if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
				t.Fatal(err)
			}

			meta = true
			hasMore = event.HasMore
		case "ships":
			var event queueStreamShips
			if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
				t.Fatal(err)
			}

			if len(event.Items) > queueStreamBatch {
				t.Fatalf("batch of %d ships, want at most %d", len(event.Items), queueStreamBatch)
			}

			items = append(items, event.Items...)
		case "counts":
			var event queueStreamCounts
			if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
				t.Fatal(err)
			}

			counts = &event.Counts
		case "done":
			finished = true
		case "error":
			t.Fatalf("stream error: %s", scanner.Text())
		default:
			t.Fatalf("unexpected event: %s", scanner.Text())
		}
	}

	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}

	if !meta || !finished {
		t.Fatalf("meta=%v done=%v", meta, finished)
	}

	if hasMore {
		t.Fatal("hasMore = true with the seeded fixtures")
	}

	if len(items) != 3 {
		t.Fatalf("got %d ships, want 3", len(items))
	}

	for _, item := range items {
		if len(item.Checks) == 0 {
			t.Fatalf("%s has no checks", item.Title)
		}

		if item.CheckSummary != queueCheckSummary(item.Checks) {
			t.Fatalf("%s summary = %+v, want %+v", item.Title, item.CheckSummary, queueCheckSummary(item.Checks))
		}
	}

	if counts == nil || counts.Ready+counts.NeedsAttention != 3 {
		t.Fatalf("counts = %+v", counts)
	}
}
