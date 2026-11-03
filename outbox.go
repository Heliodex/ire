package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/heliodex/better-deploy/wt/packages/app"
)

// backoffSeconds is the retry ladder for an outbound delivery: 30s, 2m, 10m, 30m, 2h, 6h, 12h, 24h.
var backoffSeconds = []int{30, 120, 600, 1800, 7200, 21600, 43200, 86400}

// outboxRow is one pending or finished delivery.
type outboxRow struct {
	ID         string
	ProgramID  string
	Submission string
	Event      string
	Payload    string
	URL        string
	Status     string
	Attempts   int
	DeliveryID string
	LastStatus int
	LastError  string
}

// handleTick is driven by app.json's schedule. It sends due deliveries, releases expired claims and clears expired sessions.
func (a *reviewApp) handleTick(w http.ResponseWriter, r *http.Request) {
	now := a.nowText()

	delivered, retried, failed := a.deliverDue(r.Context())

	claimRows, _ := a.store.Query(r.Context(), `select submission_id from claim where expires_at <= ? limit 200`, now)
	if claimRows != nil {
		expired := []string{}

		for claimRows.Next() {
			var id string
			if err := claimRows.Scan(&id); err == nil {
				expired = append(expired, id)
			}
		}

		_ = claimRows.Close()

		for _, id := range expired {
			_, _ = a.store.Exec(r.Context(), `update submission set status = case when status in ('claimed','in_review') then 'ready' else status end, updated_at = ? where id = ?`, now, id)
			_, _ = a.store.Exec(r.Context(), `delete from claim where submission_id = ?`, id)
			_, _ = a.store.Exec(r.Context(), `insert into submission_event (id, submission_id, kind, data, created_at) values (?, ?, 'released', '{"reason":"claim expired"}', ?)`, newID("evt"), id, now)
		}
	}

	_, _ = a.store.Exec(r.Context(), `delete from session where expires_at <= ?`, now)
	_, _ = a.store.Exec(r.Context(), `update submission set status = 'needs_attention', updated_at = ?
		where status = 'checking' and ingested_at < ?`, now, a.later(-checkStaleAfter))

	app.WriteJSON(w, http.StatusOK, map[string]int{"delivered": delivered, "retried": retried, "failed": failed})
}

// deliverDue sends the pending deliveries whose retry time has arrived.
func (a *reviewApp) deliverDue(ctx context.Context) (int, int, int) {
	rows, err := a.store.Query(ctx, `select id, program_id, coalesce(submission_id, ''), event, payload, url, status, attempts, coalesce(delivery_id, '')
		from outbox where status = 'pending' and next_attempt_at <= ? order by next_attempt_at asc limit 20`, a.nowText())
	if err != nil {
		return 0, 0, 0
	}
	defer rows.Close()

	ids := []string{}

	for rows.Next() {
		var row outboxRow
		if err := rows.Scan(&row.ID, &row.ProgramID, &row.Submission, &row.Event, &row.Payload, &row.URL, &row.Status, &row.Attempts, &row.DeliveryID); err == nil {
			ids = append(ids, row.ID)
		}
	}

	delivered, retried, failed := 0, 0, 0

	for _, id := range ids {
		switch a.deliverOutboxByID(ctx, id) {
		case "delivered":
			delivered++
		case "failed":
			failed++
		case "pending":
			retried++
		}
	}

	return delivered, retried, failed
}

// deliverOutboxByID sends one delivery and returns its new status.
func (a *reviewApp) deliverOutboxByID(ctx context.Context, id string) string {
	var row outboxRow

	err := a.store.QueryRow(ctx, `select id, program_id, coalesce(submission_id, ''), event, payload, url, status, attempts, coalesce(delivery_id, '')
		from outbox where id = ?`, id).
		Scan(&row.ID, &row.ProgramID, &row.Submission, &row.Event, &row.Payload, &row.URL, &row.Status, &row.Attempts, &row.DeliveryID)
	if err != nil {
		return ""
	}

	if row.Status == "delivered" {
		return "delivered"
	}

	if row.DeliveryID == "" {
		row.DeliveryID = newID("del")
	}

	timestamp := strconv.FormatInt(a.now().Unix(), 10)
	signature := hmacHex(a.config.OutboundSecret, []byte(timestamp+"."+row.DeliveryID+"."+row.Payload))

	httpStatus, permanent, sendErr := a.sendOutbox(ctx, row, timestamp, signature)
	now := a.nowText()

	if sendErr == nil {
		if _, err := a.store.Exec(ctx, `update outbox set status = 'delivered', attempts = attempts + 1, last_http_status = ?, last_error = NULL, delivered_at = ?, delivery_id = ? where id = ?`,
			httpStatus, now, row.DeliveryID, row.ID); err != nil {
			return "pending"
		}

		_, _ = a.store.Exec(ctx, `insert into submission_event (id, submission_id, kind, data, created_at) values (?, ?, 'delivered', ?, ?)`,
			newID("evt"), row.Submission, marshalMap(map[string]any{"event": row.Event, "deliveryId": row.DeliveryID}), now)

		return "delivered"
	}

	attempts := row.Attempts + 1
	if permanent || attempts >= len(backoffSeconds) {
		_, _ = a.store.Exec(ctx, `update outbox set status = 'failed', attempts = ?, last_http_status = ?, last_error = ?, delivery_id = ? where id = ?`,
			attempts, httpStatus, sendErr.Error(), row.DeliveryID, row.ID)
		_, _ = a.store.Exec(ctx, `insert into submission_event (id, submission_id, kind, data, created_at) values (?, ?, 'delivery_failed', ?, ?)`,
			newID("evt"), row.Submission, marshalMap(map[string]any{"event": row.Event, "error": sendErr.Error()}), now)

		return "failed"
	}

	next := a.now().Add(time.Duration(backoffSeconds[attempts-1]) * time.Second).Format(timestampFormat)
	if _, err := a.store.Exec(ctx, `update outbox set attempts = ?, next_attempt_at = ?, last_http_status = ?, last_error = ?, delivery_id = ? where id = ?`,
		attempts, next, httpStatus, sendErr.Error(), row.DeliveryID, row.ID); err != nil {
		return "pending"
	}

	return "pending"
}

// sendOutbox posts the exact stored bytes with the Ari headers, or hands them to the in-process receiver when the URL is fake://.
func (a *reviewApp) sendOutbox(ctx context.Context, row outboxRow, timestamp, signature string) (int, bool, error) {
	if strings.HasPrefix(row.URL, "fake://") {
		return a.receiveFake(ctx, row, timestamp, signature)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, row.URL, strings.NewReader(row.Payload))
	if err != nil {
		return 0, true, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Ari-Timestamp", timestamp)
	req.Header.Set("X-Ari-Delivery-Id", row.DeliveryID)
	req.Header.Set("X-Ari-Signature", signature)

	timeoutCtx, cancel := context.WithTimeout(ctx, outboundTimeout)
	defer cancel()

	req = req.WithContext(timeoutCtx)

	resp, err := a.client.Do(req)
	if err != nil {
		return 0, false, err
	}
	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp.StatusCode, false, nil
	}

	permanent := resp.StatusCode >= 400 && resp.StatusCode < 500 && resp.StatusCode != http.StatusRequestTimeout && resp.StatusCode != http.StatusTooEarly && resp.StatusCode != http.StatusTooManyRequests

	return resp.StatusCode, permanent, errors.New("receiver returned " + resp.Status)
}

// receiveFake verifies and stores a delivery in the prototype's in-process receiver.
func (a *reviewApp) receiveFake(ctx context.Context, row outboxRow, timestamp, signature string) (int, bool, error) {
	expected := hmacHex(a.config.OutboundSecret, []byte(timestamp+"."+row.DeliveryID+"."+row.Payload))

	verified := 0
	if hmacEqual(expected, signature) {
		verified = 1
	}

	// The receiver enforces the same five-minute window as a real Ari handler, so a replay from an old delivery is rejected even with a valid MAC.
	if seconds, err := strconv.ParseInt(timestamp, 10, 64); err != nil || absInt64(a.now().Unix()-seconds) > signatureWindow {
		verified = 0
	}

	if _, err := a.store.Exec(ctx, `insert or ignore into fake_receiver_delivery (id, delivery_id, event, timestamp, body, signature, verified, received_at)
		values (?, ?, ?, ?, ?, ?, ?, ?)`,
		newID("rcv"), row.DeliveryID, row.Event, timestamp, row.Payload, signature, verified, a.nowText()); err != nil {
		return http.StatusInternalServerError, false, err
	}

	if verified == 0 {
		return http.StatusUnauthorized, false, errors.New("fake receiver rejected the signature")
	}

	return http.StatusOK, false, nil
}
