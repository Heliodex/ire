package main

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"ire/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
)

// stationQueuePageLimit is the page size for a focused station queue.
const stationQueuePageLimit = 50

// currentStationSQL returns the first unfinished station for the submission alias s.
func currentStationSQL() string {
	return `(select sd.key
		from station_definition sd
		where sd.program_id = s.program_id and sd.stage = s.stage and sd.active = 1
		  and (json_array_length(sd.themes) = 0 or s.theme is null or s.theme = ''
			or exists (select 1 from json_each(sd.themes) theme where lower(cast(theme.value as text)) = lower(s.theme)))
		  and (
			exists (
				select 1 from station_question sq
				where sq.program_id = sd.program_id and sq.station_key = sd.key and sq.required = 1
				  and not exists (
					select 1 from station_answer sa
					where sa.submission_id = s.id and sa.station_key = sd.key and sa.question_key = sq.key
					  and sa.answer in ('pass','na')
				  )
			)
			or exists (
				select 1 from finding f
				where f.program_id = s.program_id and f.external_id = s.external_id
				  and f.station = sd.key and f.status = 'open' and f.severity in ('block','fixable')
			)
		  )
		order by sd.sort, sd.key
		limit 1)`
}

// stationDefinitions loads the active stations for one stage, with their questions.
func (a *ireApp) stationDefinitions(ctx context.Context, programID string, stage int) ([]contract.StationDefinition, error) {
	rows, err := a.store.Query(ctx, `select key, label, description, stage, sort, themes, active
		from station_definition where program_id = ? and stage = ? and active = 1 order by sort, key`, programID, stage)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []contract.StationDefinition{}
	index := map[string]int{}

	for rows.Next() {
		var (
			item       contract.StationDefinition
			themesJSON string
		)

		if err := rows.Scan(&item.Key, &item.Label, &item.Description, &item.Stage, &item.Sort, &themesJSON, &item.Active); err != nil {
			return nil, err
		}

		item.Themes = unmarshalStrings(themesJSON)
		index[item.Key] = len(out)
		out = append(out, item)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	questionRows, err := a.store.Query(ctx, `select station_key, key, prompt, evidence_kind, required, sort
		from station_question where program_id = ? order by station_key, sort, key`, programID)
	if err != nil {
		return nil, err
	}
	defer questionRows.Close()

	for questionRows.Next() {
		var (
			stationKey string
			question   contract.StationQuestion
		)

		if err := questionRows.Scan(&stationKey, &question.Key, &question.Prompt, &question.EvidenceKind, &question.Required, &question.Sort); err != nil {
			return nil, err
		}

		if position, ok := index[stationKey]; ok {
			out[position].Questions = append(out[position].Questions, question)
		}
	}

	return out, questionRows.Err()
}

// stationDefinition loads one station and its questions.
func (a *ireApp) stationDefinition(ctx context.Context, programID, key string) (contract.StationDefinition, bool, error) {
	definitions, err := a.stationDefinitions(ctx, programID, 1)
	if err != nil {
		return contract.StationDefinition{}, false, err
	}

	verify, err := a.stationDefinitions(ctx, programID, 2)
	if err != nil {
		return contract.StationDefinition{}, false, err
	}

	for _, station := range append(definitions, verify...) {
		if station.Key == key {
			return station, true, nil
		}
	}

	return contract.StationDefinition{}, false, nil
}

// stationApplies reports whether a station covers this ship's theme.
func stationApplies(station contract.StationDefinition, ship submissionRow) bool {
	if len(station.Themes) == 0 || ship.Theme == "" {
		return true
	}

	for _, theme := range station.Themes {
		if strings.EqualFold(theme, ship.Theme) {
			return true
		}
	}

	return false
}

// stationAnswers loads every answer recorded for one version.
func (a *ireApp) stationAnswers(ctx context.Context, submissionID string) ([]contract.StationAnswer, error) {
	rows, err := a.store.Query(ctx, `select id, submission_id, station_key, question_key, answer, evidence, coalesce(artifact_hash, ''),
			coalesce(reviewer_id, ''), updated_at
		from station_answer where submission_id = ? order by station_key, question_key`, submissionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []contract.StationAnswer{}

	for rows.Next() {
		var (
			item                     contract.StationAnswer
			artifactHash, reviewerID string
		)

		if err := rows.Scan(&item.ID, &item.SubmissionID, &item.StationKey, &item.QuestionKey, &item.Answer, &item.Evidence, &artifactHash,
			&reviewerID, &item.UpdatedAt); err != nil {
			return nil, err
		}

		item.ArtifactHash = optionalText(artifactHash)
		item.ReviewerID = optionalText(reviewerID)
		out = append(out, item)
	}

	return out, rows.Err()
}

// stationStates derives the progress for every applicable station and returns the first unfinished one.
func stationStates(ship submissionRow, definitions []contract.StationDefinition, answers []contract.StationAnswer, findings []contract.Finding) ([]contract.StationState, *string) {
	answerAt := map[string]string{}

	for _, answer := range answers {
		answerAt[answer.StationKey+"\x00"+answer.QuestionKey] = answer.Answer
	}

	openAt := map[string]int{}

	for _, finding := range findings {
		if finding.Status == "open" && (finding.Severity == "block" || finding.Severity == "fixable") {
			openAt[finding.Station]++
		}
	}

	out := []contract.StationState{}
	current := -1

	for _, station := range definitions {
		if !stationApplies(station, ship) {
			continue
		}

		var required, answered int

		failed := false

		for _, question := range station.Questions {
			if question.Required != 1 {
				continue
			}

			required++

			value, ok := answerAt[station.Key+"\x00"+question.Key]
			switch {
			case ok && (value == "pass" || value == "na"):
				answered++
			case ok && value == "fail":
				failed = true
			}
		}

		done := required == answered && openAt[station.Key] == 0
		status := "todo"

		switch {
		case done:
			status = "done"
		case current == -1:
			current = len(out)

			if failed || openAt[station.Key] > 0 {
				status = "blocked"
			} else {
				status = "current"
			}
		}

		out = append(out, contract.StationState{
			Key:          station.Key,
			Label:        station.Label,
			Sort:         station.Sort,
			Status:       status,
			Answered:     answered,
			Required:     required,
			OpenFindings: openAt[station.Key],
		})
	}

	if current == -1 {
		return out, nil
	}

	return out, stringPtr(out[current].Key)
}

// stationProgress loads the definitions, answers and findings and derives the states for one ship.
func (a *ireApp) stationProgress(ctx context.Context, ship submissionRow, findings []contract.Finding) ([]contract.StationDefinition, []contract.StationAnswer, []contract.StationState, *string, error) {
	definitions, err := a.stationDefinitions(ctx, ship.ProgramID, ship.Stage)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	answers, err := a.stationAnswers(ctx, ship.ID)
	if err != nil {
		return nil, nil, nil, nil, err
	}

	states, current := stationStates(ship, definitions, answers, findings)

	return definitions, answers, states, current, nil
}

// stationCounts counts the open ships whose current station is each one.
func (a *ireApp) stationCounts(ctx context.Context, programID string, stage int) (map[string]int, error) {
	rows, err := a.store.Query(ctx, `select current.key, count(*) from (
		select `+currentStationSQL()+` as key
		from submission s
		where s.program_id = ? and s.stage = ?
		  and s.status in ('ready','claimed','in_review','needs_attention')
	) current where current.key is not null group by current.key`, programID, stage)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	counts := map[string]int{}

	for rows.Next() {
		var (
			key   string
			count int
		)

		if err := rows.Scan(&key, &count); err != nil {
			return nil, err
		}

		counts[key] = count
	}

	return counts, rows.Err()
}

// handleStations lists the stations and the number of open ships whose current station is each one.
func (a *ireApp) handleStations(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewer(w, r)
	if !ok {
		return
	}

	definitions, err := a.stationDefinitions(r.Context(), person.ProgramID, stageOne)
	if err != nil {
		writeStorage(w, err)

		return
	}

	counts, err := a.stationCounts(r.Context(), person.ProgramID, stageOne)
	if err != nil {
		writeStorage(w, err)

		return
	}

	out := make([]contract.StationSummary, 0, len(definitions))

	for _, station := range definitions {
		out = append(out, contract.StationSummary{
			Key:         station.Key,
			Label:       station.Label,
			Description: station.Description,
			Sort:        station.Sort,
			Count:       counts[station.Key],
		})
	}

	app.WriteJSON(w, http.StatusOK, contract.StationsResponse{Stations: out})
}

// handleStationQueue lists the open ships whose current station is the requested one.
func (a *ireApp) handleStationQueue(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewer(w, r)
	if !ok {
		return
	}

	station, found, err := a.stationDefinition(r.Context(), person.ProgramID, r.PathValue("station"))
	if err != nil {
		writeStorage(w, err)

		return
	}

	if !found || station.Stage != stageOne {
		app.WriteError(w, http.StatusNotFound, "unknown station")
		return
	}

	items, hasMore, err := a.stationQueueItems(r.Context(), person, stageOne, station.Key)
	if err != nil {
		writeStorage(w, err)

		return
	}

	counts, err := a.stationCounts(r.Context(), person.ProgramID, stageOne)
	if err != nil {
		writeStorage(w, err)

		return
	}

	app.WriteJSON(w, http.StatusOK, contract.StationQueueResponse{Station: station, Items: items, HasMore: hasMore, Total: counts[station.Key]})
}

// stationQueueItems returns one page of ships whose current station is the requested one.
func (a *ireApp) stationQueueItems(ctx context.Context, person reviewer, stage int, station string) ([]contract.QueueItem, bool, error) {
	query, args := a.queueListQuery(queueParams{stage: stage, filter: queueFilterAll, station: station}, person, stationQueuePageLimit+1, 0)

	rows, err := a.store.Query(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	out := []contract.QueueItem{}

	for rows.Next() {
		item, err := scanQueueItem(rows)
		if err != nil {
			return nil, false, err
		}

		out = append(out, item)
	}

	hasMore := len(out) > stationQueuePageLimit
	if hasMore {
		out = out[:stationQueuePageLimit]
	}

	return out, hasMore, rows.Err()
}

// handleStationAnswer records one station question for the current version.
func (a *ireApp) handleStationAnswer(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewerWrite(w, r)
	if !ok {
		return
	}

	ship, ok := a.loadShipForReview(w, r)
	if !ok {
		return
	}

	if !a.holdsClaim(r.Context(), ship.ID, person.ID) {
		app.WriteError(w, http.StatusConflict, "claim the ship before answering a station")
		return
	}

	var request contract.StationAnswerRequest
	if !app.ReadJSON(w, r, maxAPIBody, "answer", &request) {
		return
	}

	if request.Version != ship.Version {
		app.WriteErrorCode(w, http.StatusConflict, "stale_version", "this ship changed since you opened it; reload before answering")
		return
	}

	station, found, err := a.stationDefinition(r.Context(), ship.ProgramID, r.PathValue("station"))
	if err != nil {
		writeStorage(w, err)

		return
	}

	if !found || station.Stage != ship.Stage || !stationApplies(station, ship) {
		app.WriteError(w, http.StatusNotFound, "unknown station")
		return
	}

	questionKey := r.PathValue("question")

	if !slices.ContainsFunc(station.Questions, func(question contract.StationQuestion) bool { return question.Key == questionKey }) {
		app.WriteError(w, http.StatusNotFound, "unknown station question")
		return
	}

	if request.Answer != "pass" && request.Answer != "fail" && request.Answer != "na" {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "answer", "choose pass, fail or not applicable")
		return
	}

	if len(request.Evidence) > maxNote {
		app.WriteErrorField(w, http.StatusUnprocessableEntity, "invalid", "evidence", "evidence is limited to 4000 characters")
		return
	}

	now := a.nowText()
	id := newID("sta")

	if _, err := a.store.Exec(r.Context(), `insert into station_answer (id, submission_id, station_key, question_key, answer, evidence, artifact_hash, reviewer_id, created_at, updated_at)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		on conflict(submission_id, station_key, question_key) do update set
			answer = excluded.answer, evidence = excluded.evidence, artifact_hash = excluded.artifact_hash,
			reviewer_id = excluded.reviewer_id, updated_at = excluded.updated_at`,
		id, ship.ID, station.Key, questionKey, request.Answer, request.Evidence, nullString(stringValue(request.ArtifactHash)), person.ID, now, now); err != nil {
		writeStorage(w, err)

		return
	}

	answers, err := a.stationAnswers(r.Context(), ship.ID)
	if err != nil {
		writeStorage(w, err)

		return
	}

	findings, err := a.loadFindings(r.Context(), ship.ProgramID, ship.ExternalID)
	if err != nil {
		writeStorage(w, err)

		return
	}

	definitions, err := a.stationDefinitions(r.Context(), ship.ProgramID, ship.Stage)
	if err != nil {
		writeStorage(w, err)

		return
	}

	states, current := stationStates(ship, definitions, answers, findings)

	answer, found := answerAt(answers, station.Key, questionKey)
	if !found {
		app.WriteError(w, http.StatusInternalServerError, "could not read the saved answer")
		return
	}

	app.WriteJSON(w, http.StatusOK, contract.StationAnswerResult{Answer: answer, States: states, CurrentStation: current})
}

// answerAt finds one saved answer.
func answerAt(answers []contract.StationAnswer, stationKey, questionKey string) (contract.StationAnswer, bool) {
	for _, answer := range answers {
		if answer.StationKey == stationKey && answer.QuestionKey == questionKey {
			return answer, true
		}
	}

	return contract.StationAnswer{}, false
}
