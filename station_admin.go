package main

import (
	"context"
	"net/http"
	"strings"

	"ire/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// handleSaveStation creates or replaces one station and its questions.
func (a *ireApp) handleSaveStation(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	var request contract.StationDefinition
	if !app.ReadJSON(w, r, maxAPIBody, "key", &request) {
		return
	}

	key := strings.TrimSpace(r.PathValue("station"))
	if request.Key != "" && request.Key != key {
		app.WriteErrorField(w, http.StatusBadRequest, "invalid", "key", "the station key in the body must match the path")
		return
	}

	if key == "" || len(key) > 120 {
		app.WriteErrorField(w, http.StatusBadRequest, "invalid", "key", "use a short station key")
		return
	}

	if request.Stage != stageOne && request.Stage != 2 {
		app.WriteErrorField(w, http.StatusBadRequest, "invalid", "stage", "choose stage 1 or 2")
		return
	}

	if strings.TrimSpace(request.Label) == "" {
		app.WriteErrorField(w, http.StatusBadRequest, "invalid", "label", "give the station a label")
		return
	}

	if request.Active != 0 && request.Active != 1 {
		app.WriteErrorField(w, http.StatusBadRequest, "invalid", "active", "choose active or inactive")
		return
	}

	seen := map[string]bool{}

	for i := range request.Questions {
		question := &request.Questions[i]
		question.Key = strings.TrimSpace(question.Key)
		question.Prompt = strings.TrimSpace(question.Prompt)
		question.EvidenceKind = strings.TrimSpace(question.EvidenceKind)

		if question.Key == "" || len(question.Key) > 120 {
			app.WriteErrorField(w, http.StatusBadRequest, "invalid", "questions", "every question needs a short key")
			return
		}

		if seen[question.Key] {
			app.WriteErrorField(w, http.StatusBadRequest, "invalid", "questions", "question keys must be unique")
			return
		}

		seen[question.Key] = true

		if question.Prompt == "" {
			app.WriteErrorField(w, http.StatusBadRequest, "invalid", "questions", "every question needs a prompt")
			return
		}

		if question.EvidenceKind == "" {
			question.EvidenceKind = "text"
		}

		if question.Required != 0 && question.Required != 1 {
			app.WriteErrorField(w, http.StatusBadRequest, "invalid", "questions", "required must be 0 or 1")
			return
		}
	}

	statements := []storage.TransactionStatement{
		{SQL: `insert into station_definition (program_id, key, label, description, stage, sort, themes, active)
			values (?, ?, ?, ?, ?, ?, ?, ?)
			on conflict(program_id, key) do update set
				label = excluded.label, description = excluded.description, stage = excluded.stage,
				sort = excluded.sort, themes = excluded.themes, active = excluded.active`,
			Args: []any{person.ProgramID, key, request.Label, request.Description, request.Stage, request.Sort, marshalStrings(request.Themes), request.Active}},
		{SQL: `delete from station_question where program_id = ? and station_key = ?`, Args: []any{person.ProgramID, key}},
	}

	for _, question := range request.Questions {
		statements = append(statements, storage.TransactionStatement{
			SQL: `insert into station_question (program_id, station_key, key, prompt, evidence_kind, required, sort)
				values (?, ?, ?, ?, ?, ?, ?)`,
			Args: []any{person.ProgramID, key, question.Key, question.Prompt, question.EvidenceKind, question.Required, question.Sort},
		})
	}

	if _, err := a.store.Transaction(r.Context(), statements...); err != nil {
		writeStorage(w, err)

		return
	}

	station, found, err := a.stationDefinition(r.Context(), person.ProgramID, key)
	if err != nil {
		writeStorage(w, err)

		return
	}

	if !found {
		app.WriteError(w, http.StatusInternalServerError, "could not read the saved station")
		return
	}

	app.WriteJSON(w, http.StatusOK, station)
}

// handleDeleteStation removes one station and its questions.
func (a *ireApp) handleDeleteStation(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireAdmin(w, r)
	if !ok {
		return
	}

	key := strings.TrimSpace(r.PathValue("station"))
	if key == "" {
		app.WriteErrorField(w, http.StatusBadRequest, "invalid", "station", "choose a station")
		return
	}

	if _, err := a.store.Exec(r.Context(), `delete from station_definition where program_id = ? and key = ?`, person.ProgramID, key); err != nil {
		writeStorage(w, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// stationForEdit returns one station for the admin editor.
func (a *ireApp) stationForEdit(ctx context.Context, programID, key string) (contract.StationDefinition, bool, error) {
	return a.stationDefinition(ctx, programID, key)
}
