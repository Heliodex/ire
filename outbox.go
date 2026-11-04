package main

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/outbox"
)

const outboxTable = "webhook_outbox"

// outboxStore builds the signed delivery queue around the app clock, HTTP client and fake receiver.
func (a *reviewApp) outboxStore() outbox.Outbox {
	return outbox.Outbox{
		Table:  outboxTable,
		Signer: a.signer(a.config.OutboundSecret),
		HTTP:   &http.Client{Transport: a.client.Transport, Timeout: outboundTimeout},
		Now:    a.now,
		Sender: reviewSender{app: a},
	}
}

// reviewSender keeps the in-process fake receiver for the demo and posts real HTTP deliveries with Ari's headers.
type reviewSender struct {
	app *reviewApp
}

// Send performs one delivery attempt. A dry run records the delivery as sent without touching the receiver, so a pilot can compare before cutover.
func (s reviewSender) Send(ctx context.Context, delivery outbox.Delivery) (int, error) {
	if s.app.config.OutboundDryRun {
		app.Infof("outbox dry-run %s %s", delivery.Event, delivery.DeliveryID)

		return http.StatusOK, nil
	}

	if strings.HasPrefix(delivery.URL, "fake://") {
		return s.app.receiveFake(ctx, delivery)
	}

	sender := outbox.HTTPSender{
		Client:          s.app.outboxStore().HTTP,
		TimestampHeader: "X-Ari-Timestamp",
		DeliveryHeader:  "X-Ari-Delivery-Id",
		SignatureHeader: "X-Ari-Signature",
		UserAgent:       "half-life-review",
	}

	return sender.Send(ctx, delivery)
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

	if _, err := a.pruneRetention(r.Context()); err != nil {
		app.Errorf("retention: %v", err)
	}

	_, _ = a.store.Exec(r.Context(), `delete from session where expires_at <= ?`, now)
	_, _ = a.store.Exec(r.Context(), `update submission set status = 'needs_attention', updated_at = ?
		where status = 'checking' and ingested_at < ?`, now, a.later(-checkStaleAfter))

	app.WriteJSON(w, http.StatusOK, map[string]int{"delivered": delivered, "retried": retried, "failed": failed})
}

// deliverDue sends every delivery whose retry time has arrived.
func (a *reviewApp) deliverDue(ctx context.Context) (int, int, int) {
	if !a.outboundEnabled(ctx, programID) {
		return 0, 0, 0
	}

	summary, err := a.outboxStore().DeliverDue(ctx, a.store, outbox.DefaultLimit)
	if err != nil {
		app.Errorf("outbox delivery: %v", err)

		return 0, 0, 0
	}

	return summary.Delivered, summary.Retried, summary.Failed
}

// deliverOutboxNow sends the queue and returns the stored status of one delivery.
func (a *reviewApp) deliverOutboxNow(ctx context.Context, id string) string {
	a.deliverDue(ctx)

	record, err := a.outboxStore().Get(ctx, a.store, id)
	if err != nil {
		return ""
	}

	return normalizeOutboxStatus(record.Status)
}

// normalizeOutboxStatus collapses a claimed delivery to pending for the API.
func normalizeOutboxStatus(status string) string {
	if status == outbox.StatusSending {
		return outbox.StatusPending
	}

	return status
}

// receiveFake verifies and stores a delivery in the prototype's in-process receiver.
func (a *reviewApp) receiveFake(ctx context.Context, delivery outbox.Delivery) (int, error) {
	signer := a.signer(a.config.OutboundSecret)

	verified := 0
	if signer.Verify(delivery.Timestamp, delivery.DeliveryID, delivery.Signature, delivery.Payload) == nil {
		verified = 1
	}

	if _, err := a.store.Exec(ctx, `insert or ignore into fake_receiver_delivery (id, delivery_id, event, timestamp, body, signature, verified, received_at)
		values (?, ?, ?, ?, ?, ?, ?, ?)`,
		newID("rcv"), delivery.DeliveryID, delivery.Event, delivery.Timestamp, string(delivery.Payload), delivery.Signature, verified, a.nowText()); err != nil {
		return http.StatusInternalServerError, err
	}

	if verified == 0 {
		return http.StatusUnauthorized, errors.New("fake receiver rejected the signature")
	}

	return http.StatusOK, nil
}
