package main

import (
	"net/http"
	"strings"

	"ire/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
)

const reviewedFilterAll = "all"

// handleReviewed returns decided ships, newest first, for the Reviewed tab.
func (a *ireApp) handleReviewed(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewer(w, r)
	if !ok {
		return
	}

	decision := strings.TrimSpace(r.URL.Query().Get("decision"))
	if decision == "" {
		decision = reviewedFilterAll
	}

	query := `select s.id, s.external_id, s.title, coalesce(s.maker_name, ''),
			coalesce(s.theme, ''), coalesce(s.thumbnail_url, ''),
			rv.decision, rv.stage, coalesce(rv.hours_approved_seconds, 0),
			s.hours_claimed_seconds, coalesce(s.decided_at, rv.created_at)
		from submission s
		join review rv on rv.submission_id = s.id and rv.is_final = 1 and rv.invalidated_at is null
		where s.program_id = ? and s.status = 'decided'`

	args := []any{person.ProgramID}

	switch decision {
	case reviewedFilterAll:
	case decisionApproved, decisionChanges, decisionRejected:
		query += ` and rv.decision = ?`

		args = append(args, decision)
	default:
		app.WriteErrorField(w, http.StatusBadRequest, "invalid", "decision", "decision must be all, approved, changes or rejected")
		return
	}

	limit := parseLimit(r)
	query += ` order by coalesce(s.decided_at, rv.created_at) desc, s.id asc limit ? offset ?`

	args = append(args, limit+1, parseOffset(r))

	rows, err := a.store.Query(r.Context(), query, args...)
	if err != nil {
		writeStorage(w, err)

		return
	}
	defer rows.Close()

	response := contract.ReviewedResponse{HasMore: false, Items: []contract.ReviewedItem{}}

	for rows.Next() {
		var (
			item                    contract.ReviewedItem
			theme, thumb, makerName string
		)
		if err := rows.Scan(&item.ID, &item.ExternalID, &item.Title, &makerName,
			&theme, &thumb, &item.Decision, &item.Stage, &item.HoursApprovedSeconds,
			&item.HoursClaimedSeconds, &item.DecidedAt); err != nil {
			writeStorage(w, err)

			return
		}

		if makerName == "" {
			makerName = "Unknown maker"
		}

		item.MakerName = makerName

		if theme != "" {
			item.Theme = &theme
		}

		if thumb != "" {
			item.ThumbnailURL = &thumb
		}

		response.Items = append(response.Items, item)
	}

	if err := rows.Err(); err != nil {
		writeStorage(w, err)

		return
	}

	if len(response.Items) > limit {
		response.Items = response.Items[:limit]
		response.HasMore = true
	}

	app.WriteJSON(w, http.StatusOK, response)
}
