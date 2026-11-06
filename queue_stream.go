package main

import (
	"context"
	"encoding/json/v2"
	"net/http"

	"ire/contract"
)

// queueStreamBatch is how many ships one NDJSON line carries. Small batches let the first rows paint before the
// rest of the page has been read from storage.
const queueStreamBatch = 10

// queueStreamMeta opens the stream with the page size hint. Counts follow after the ships so the badge query
// never holds the first rows back.
type queueStreamMeta struct {
	Type    string `json:"type"`
	HasMore bool   `json:"hasMore"`
	Items   int    `json:"items"`
}

type queueStreamShips struct {
	Type  string               `json:"type"`
	Items []contract.QueueItem `json:"items"`
}

type queueStreamCounts struct {
	Type   string               `json:"type"`
	Counts contract.QueueCounts `json:"counts"`
}

type queueStreamDone struct {
	Type string `json:"type"`
}

type queueStreamFailure struct {
	Type  string `json:"type"`
	Error string `json:"error"`
}

// streamQueue writes the queue as newline-delimited JSON: a meta line, then ships in batches, then counts.
// The browser appends each batch as it arrives instead of waiting for the whole response.
func (a *ireApp) streamQueue(w http.ResponseWriter, r *http.Request, person reviewer, params queueParams) {
	limit := parseLimit(r)
	query, args := a.queueListQuery(params, person, limit, parseOffset(r))

	rows, err := a.store.Query(r.Context(), query, args...)
	if err != nil {
		writeStorage(w, err)

		return
	}
	defer rows.Close()

	items := make([]contract.QueueItem, 0, limit)

	for rows.Next() {
		item, err := scanQueueItem(rows)
		if err != nil {
			writeStorage(w, err)

			return
		}

		items = append(items, item)
	}

	if err := rows.Err(); err != nil {
		writeStorage(w, err)

		return
	}

	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")

	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}

	writeQueueEvent(w, queueStreamMeta{Type: "meta", HasMore: hasMore, Items: len(items)})

	for start := 0; start < len(items); start += queueStreamBatch {
		batch := items[start:min(start+queueStreamBatch, len(items))]

		if err := a.attachQueueChecks(r.Context(), batch); err != nil {
			writeQueueEvent(w, queueStreamFailure{Type: "error", Error: "could not load the queue"})

			return
		}

		writeQueueEvent(w, queueStreamShips{Type: "ships", Items: batch})
	}

	counts, err := a.queueCounts(r.Context(), person.ProgramID, person.ID, a.nowText())
	if err != nil {
		writeQueueEvent(w, queueStreamFailure{Type: "error", Error: "could not load the queue totals"})

		return
	}

	if !person.Has(permSecondPass) {
		counts.Stage2Hold = 0
	}

	writeQueueEvent(w, queueStreamCounts{Type: "counts", Counts: counts})
	writeQueueEvent(w, queueStreamDone{Type: "done"})
}

// attachQueueChecks fills the dots and recomputes the summary for one batch of ships.
func (a *ireApp) attachQueueChecks(ctx context.Context, items []contract.QueueItem) error {
	checks, err := a.queueChecks(ctx, items)
	if err != nil {
		return err
	}

	for i := range items {
		items[i].Checks = checks[items[i].ID]
		items[i].CheckSummary = queueCheckSummary(items[i].Checks)
	}

	return nil
}

// writeQueueEvent writes one NDJSON line and flushes it, so the browser sees each batch immediately.
func writeQueueEvent(w http.ResponseWriter, event any) {
	data, err := json.Marshal(event)
	if err != nil {
		return
	}

	_, _ = w.Write(append(data, '\n'))

	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}
