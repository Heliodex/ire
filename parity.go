package main

import (
	"database/sql"
	"net/http"

	"ire/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
)

const maxParityShips = 1000

// handleParity returns the open queue with the values the Half Life parity script compares. It is machine-only and never writes.
func (a *ireApp) handleParity(w http.ResponseWriter, r *http.Request) {
	program, ok := a.programFor(w, r)
	if !ok {
		return
	}

	if !a.requireMachine(w, r) {
		return
	}

	rows, err := a.store.Query(r.Context(), `select id, external_id, version, status, stage,
			hours_claimed_seconds, hours_approved_seconds, coalesce(bom_total_cents, 0), coalesce(check_summary, ''), updated_at
		from submission
		where program_id = ? and status in ('checking','ready','claimed','in_review','needs_attention','stage2_hold')
		order by ingested_at asc, id asc limit ?`, program.ID, maxParityShips)
	if err != nil {
		writeStorage(w, err)

		return
	}
	defer rows.Close()

	response := contract.ParityResponse{GeneratedAt: a.nowText(), Ships: []contract.ParityShip{}}

	for rows.Next() {
		var (
			ship          contract.ParityShip
			summary       string
			approvedHours sql.NullInt64
		)

		if err := rows.Scan(&ship.ID, &ship.ExternalID, &ship.Version, &ship.Status, &ship.Stage,
			&ship.HoursClaimedSeconds, &approvedHours, &ship.BomTotalCents, &summary, &ship.UpdatedAt); err != nil {
			writeStorage(w, err)

			return
		}

		if approvedHours.Valid {
			hours := int(approvedHours.Int64)
			ship.HoursApprovedSeconds = &hours
		}

		ship.CheckSummary = parseCheckSummary(summary)
		ship.Phase = ariPhase(ship.Status)
		response.Ships = append(response.Ships, ship)
	}

	if err := rows.Err(); err != nil {
		writeStorage(w, err)

		return
	}

	app.WriteJSON(w, http.StatusOK, response)
}
