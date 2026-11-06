package main

import (
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"ire/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
)

const (
	searchLimit     = 8
	maxSearchLimit  = 20
	maxSearchLength = 120
)

// likeEscaper keeps a typed % or _ literal instead of letting it widen the search.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// handleSearch finds open ships by title, maker, external id or theme for the header search.
func (a *ireApp) handleSearch(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewer(w, r)
	if !ok {
		return
	}

	query := strings.TrimSpace(r.URL.Query().Get("q"))
	if query == "" {
		app.WriteJSON(w, http.StatusOK, contract.SearchResponse{Results: []contract.SearchResult{}})
		return
	}

	if utf8.RuneCountInString(query) > maxSearchLength {
		app.WriteErrorField(w, http.StatusBadRequest, "invalid", "q", "search is too long")
		return
	}

	needle := "%" + likeEscaper.Replace(strings.ToLower(query)) + "%"
	prefix := likeEscaper.Replace(strings.ToLower(query)) + "%"

	var stageClause string

	if !canReviewSecondPass(person.Role) {
		stageClause = " and s.stage = 1"
	}

	rows, err := a.store.Query(r.Context(), `select s.id, s.external_id, s.title, coalesce(s.maker_name, ''),
			s.status, s.stage, coalesce(s.theme, ''), coalesce(s.thumbnail_url, ''),
			coalesce(s.week, 0), s.version, s.hours_claimed_seconds, s.ingested_at
		from submission s
		where s.program_id = ?
		  and s.status in ('checking','ready','claimed','in_review','needs_attention','stage2_hold')`+stageClause+`
		  and (lower(s.title) like ? escape '\'
		    or lower(coalesce(s.maker_name, '')) like ? escape '\'
		    or lower(s.external_id) like ? escape '\'
		    or lower(coalesce(s.theme, '')) like ? escape '\')
		order by
			case
				when lower(s.title) like ? escape '\' then 0
				when lower(coalesce(s.maker_name, '')) like ? escape '\' then 1
				else 2
			end,
			s.updated_at desc,
			s.id asc
		limit ?`,
		person.ProgramID, needle, needle, needle, needle, prefix, prefix, parseSearchLimit(r))
	if err != nil {
		writeStorage(w, err)

		return
	}
	defer rows.Close()

	response := contract.SearchResponse{Results: []contract.SearchResult{}}

	for rows.Next() {
		var (
			item                    contract.SearchResult
			theme, thumb, makerName string
			week                    int
		)
		if err := rows.Scan(&item.ID, &item.ExternalID, &item.Title, &makerName,
			&item.Status, &item.Stage, &theme, &thumb, &week,
			&item.Version, &item.HoursClaimedSeconds, &item.IngestedAt); err != nil {
			writeStorage(w, err)

			return
		}

		if makerName == "" {
			makerName = "Unknown maker"
		}

		item.MakerName = makerName

		if week > 0 {
			item.Week = &week
		}

		if theme != "" {
			item.Theme = &theme
		}

		if thumb != "" {
			item.ThumbnailURL = &thumb
		}

		response.Results = append(response.Results, item)
	}

	if err := rows.Err(); err != nil {
		writeStorage(w, err)

		return
	}

	app.WriteJSON(w, http.StatusOK, response)
}

// parseSearchLimit reads a small page size for the dropdown.
func parseSearchLimit(r *http.Request) int {
	value := r.URL.Query().Get("limit")
	if value == "" {
		return searchLimit
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return searchLimit
	}

	return min(parsed, maxSearchLimit)
}
