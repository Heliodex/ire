package main

import (
	"net/http"
	"strings"

	"review/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// handleSendBack returns a held ship to stage 1 and invalidates the first-pass review that held it.
func (a *reviewApp) handleSendBack(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewer(w, r)
	if !ok {
		return
	}

	if !a.requireWritable(w) {
		return
	}

	if !canReviewSecondPass(person.Role) {
		app.WriteError(w, http.StatusForbidden, "only organisers can send a ship back")
		return
	}

	ship, ok := a.loadShipForReview(w, r)
	if !ok {
		return
	}

	var request contract.SendBackRequest
	if !app.ReadJSON(w, r, maxAPIBody, "reason", &request) {
		return
	}

	if ship.Status != statusStage2Hold || ship.Stage != 2 {
		app.WriteError(w, http.StatusConflict, "only a held second pass can be sent back")
		return
	}

	if request.Version != ship.Version {
		app.WriteErrorCode(w, http.StatusConflict, "stale_version", "this ship changed since you opened it; reload before sending it back")
		return
	}

	if !a.holdsClaim(r.Context(), ship.ID, person.ID) {
		app.WriteError(w, http.StatusConflict, "claim the ship before sending it back")
		return
	}

	reason := strings.TrimSpace(request.Reason)
	if reason == "" {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "reason_required", "reason", "explain what stage 1 should look at again")
		return
	}

	if len(reason) > maxNote {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "reason", "the reason is limited to 4000 characters")
		return
	}

	held, found, err := a.heldReview(r.Context(), ship.ID)
	if err != nil {
		writeStorage(w, err)

		return
	}

	if !found {
		app.WriteError(w, http.StatusConflict, "no first-pass review to send back")
		return
	}

	now := a.nowText()
	statements := []storage.TransactionStatement{
		{SQL: `update review set invalidated_at = ?, invalidated_by = ?, invalidation_reason = ?
				where id = ? and invalidated_at is null`,
			Args: []any{now, person.ID, reason, held.ID}},
		{SQL: `update submission set status = ?, stage = ?, hours_approved_seconds = null, held_reason = null,
				decided_at = null, updated_at = ? where id = ? and status = 'stage2_hold'`,
			Args: []any{statusReady, stageOne, now, ship.ID}},
		{SQL: `delete from claim where submission_id = ?`, Args: []any{ship.ID}},
		{SQL: `delete from draft where submission_id = ?`, Args: []any{ship.ID}},
		{SQL: `insert into submission_event (id, submission_id, kind, actor_id, actor_email, data, created_at)
				values (?, ?, 'sent_back', ?, ?, ?, ?)`,
			Args: []any{newID("evt"), ship.ID, person.ID, person.Email, marshalMap(map[string]any{"reason": reason, "review": held.ID}), now}},
		{SQL: `insert into audit_log (id, program_id, actor_id, actor_email, action, target_type, target_id, before_json, after_json, created_at)
				values (?, ?, ?, ?, 'review.send_back', 'submission', ?, ?, ?, ?)`,
			Args: []any{newID("aud"), ship.ProgramID, person.ID, person.Email, ship.ID,
				marshalMap(map[string]any{"status": ship.Status, "stage": ship.Stage, "version": ship.Version}),
				marshalMap(map[string]any{"status": statusReady, "stage": stageOne, "invalidated": held.ID, "reason": reason}), now}},
	}

	if _, err := a.store.Transaction(r.Context(), statements...); err != nil {
		writeStorage(w, err)

		return
	}

	app.WriteJSON(w, http.StatusOK, contract.SendBackResult{ID: ship.ID, Status: statusReady, Stage: stageOne})
}
