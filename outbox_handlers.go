package main

import (
	"encoding/json/v2"
	"net/http"

	"ire/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
)

// handleOutbox lists recent deliveries so an operator can see what was sent and why a retry is pending.
func (a *ireApp) handleOutbox(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewer(w, r)
	if !ok {
		return
	}

	records, err := a.outboxStore().List(r.Context(), a.store, parseLimit(r))
	if err != nil {
		writeStorage(w, err)

		return
	}

	response := contract.OutboxResponse{Deliveries: []contract.OutboxRow{}}

	for _, record := range records {
		if record.Metadata["program_id"] != person.ProgramID {
			continue
		}

		item := contract.OutboxRow{
			ID:        record.ID,
			Event:     record.Event,
			Status:    normalizeOutboxStatus(record.Status),
			Attempts:  record.Attempts,
			CreatedAt: record.CreatedAt.Format(timestampFormat),
		}

		if submissionID := record.Metadata["submission_id"]; submissionID != "" {
			item.SubmissionID = &submissionID
		}

		if record.LastHTTPStatus > 0 {
			lastHTTP := record.LastHTTPStatus
			item.LastHttpStatus = &lastHTTP
		}

		if record.LastError != "" {
			lastError := record.LastError
			item.LastError = &lastError
		}

		if record.DeliveredAt != nil {
			deliveredAt := record.DeliveredAt.Format(timestampFormat)
			item.DeliveredAt = &deliveredAt
		}

		if record.DeliveryID != "" {
			deliveryID := record.DeliveryID
			item.DeliveryID = &deliveryID
		}

		if len(record.Payload) > 0 {
			payload := string(record.Payload)
			item.Payload = &payload

			if !person.Has(permViewPII) {
				item.Payload = redactOutboxPayload(payload)
			}
		}

		response.Deliveries = append(response.Deliveries, item)
	}

	app.WriteJSON(w, http.StatusOK, response)
}

// redactOutboxPayload hides the maker's address from members without the view permission reading delivery debug output, matching the bundle's redaction.
func redactOutboxPayload(payload string) *string {
	var event map[string]any
	if err := json.Unmarshal([]byte(payload), &event); err != nil {
		return &payload
	}

	maker, ok := event["maker"].(map[string]any)
	if !ok {
		return &payload
	}

	email, ok := maker["email"].(string)
	if !ok || email == "" {
		return &payload
	}

	maker["email"] = redactEmail(email)

	redacted, err := json.Marshal(event)
	if err != nil {
		return &payload
	}

	value := string(redacted)

	return &value
}

// handleFakeDeliveries lists what the prototype's in-process receiver accepted, with the signature it verified.
func (a *ireApp) handleFakeDeliveries(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.currentReviewer(w, r); !ok {
		return
	}

	rows, err := a.store.Query(r.Context(), `select id, delivery_id, event, timestamp, body, signature, verified, received_at
		from fake_receiver_delivery order by received_at desc limit ?`, parseLimit(r))
	if err != nil {
		writeStorage(w, err)

		return
	}
	defer rows.Close()

	response := contract.FakeDeliveriesResponse{Deliveries: []contract.FakeDelivery{}}

	for rows.Next() {
		var item contract.FakeDelivery
		if err := rows.Scan(&item.ID, &item.DeliveryID, &item.Event, &item.Timestamp, &item.Body, &item.Signature, &item.Verified, &item.ReceivedAt); err != nil {
			writeStorage(w, err)

			return
		}

		response.Deliveries = append(response.Deliveries, item)
	}

	app.WriteJSON(w, http.StatusOK, response)
}
