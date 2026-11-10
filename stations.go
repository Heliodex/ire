package main

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"ire/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// stationQueuePageLimit is the page size for a focused station queue.
const stationQueuePageLimit = 50

// stationStage reads the optional stage filter, defaulting to the first pass.
func stationStage(r *http.Request) int {
	if r.URL.Query().Get("stage") == "2" {
		return 2
	}

	return stageOne
}

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
				  and (
					not exists (
						select 1 from station_answer sa
						where sa.submission_id = s.id and sa.station_key = sd.key and sa.question_key = sq.key
					)
					or (
						select count(distinct sa.answer) from station_answer sa
						where sa.submission_id = s.id and sa.station_key = sd.key and sa.question_key = sq.key
					) > 1
					or exists (
						select 1 from station_answer sa
						where sa.submission_id = s.id and sa.station_key = sd.key and sa.question_key = sq.key
						  and sa.answer = 'fail'
						  and not exists (
							select 1 from finding f
							where f.program_id = s.program_id and f.external_id = s.external_id
							  and f.station = sd.key and f.question = sq.prompt
							  and f.status in ('open','disputed')
						)
					)
				  )
			)
		  )
		order by sd.sort, sd.key
		limit 1)`
}

// stationDefinitions loads the active stations for one stage, with their questions.
func (a *ireApp) stationDefinitions(ctx context.Context, programID string, stage int) ([]contract.StationDefinition, error) {
	return a.stationDefinitionsWith(ctx, programID, stage, false)
}

// stationDefinitionsWith loads one stage's stations, optionally including inactive rows for the editor.
func (a *ireApp) stationDefinitionsWith(ctx context.Context, programID string, stage int, includeInactive bool) ([]contract.StationDefinition, error) {
	query := `select key, label, description, stage, sort, themes, active
		from station_definition where program_id = ? and stage = ?`

	if !includeInactive {
		query += ` and active = 1`
	}

	query += ` order by sort, key`

	rows, err := a.store.Query(ctx, query, programID, stage)
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
	rows, err := a.store.Query(ctx, `select a.id, a.submission_id, a.station_key, a.question_key, a.answer, a.evidence,
			coalesce(a.artifact_hash, ''), coalesce(a.reviewer_id, ''), coalesce(u.name, ''), coalesce(u.email, ''), a.updated_at
		from station_answer a left join user u on u.id = a.reviewer_id
		where a.submission_id = ? order by a.station_key, a.question_key, a.updated_at`, submissionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []contract.StationAnswer{}

	for rows.Next() {
		var (
			item                                  contract.StationAnswer
			artifactHash, reviewerID, name, email string
		)

		if err := rows.Scan(&item.ID, &item.SubmissionID, &item.StationKey, &item.QuestionKey, &item.Answer, &item.Evidence, &artifactHash,
			&reviewerID, &name, &email, &item.UpdatedAt); err != nil {
			return nil, err
		}

		item.ArtifactHash = optionalText(artifactHash)
		item.ReviewerID = optionalText(reviewerID)

		switch {
		case name != "":
			item.ReviewerName = stringPtr(name)
		case email != "":
			item.ReviewerName = stringPtr(email)
		}

		out = append(out, item)
	}

	return out, rows.Err()
}

// stationStates derives the progress for every applicable station and returns the first unfinished one.
func stationStates(ship submissionRow, definitions []contract.StationDefinition, answers []contract.StationAnswer, findings []contract.Finding) ([]contract.StationState, *string) {
	answersAt := map[string]map[string]bool{}

	for _, answer := range answers {
		key := answer.StationKey + "\x00" + answer.QuestionKey
		if answersAt[key] == nil {
			answersAt[key] = map[string]bool{}
		}

		answersAt[key][answer.Answer] = true
	}

	openAt := map[string]int{}

	for _, finding := range findings {
		if finding.Status == "open" && (finding.Severity == "block" || finding.Severity == "fixable") {
			openAt[finding.Station]++
		}
	}

	// foundAt marks the questions with a finding on record. A failed answer only
	// holds its station until its finding is recorded; open findings accumulate
	// down the line and gate the synthesis decision instead.
	foundAt := map[string]bool{}

	for _, finding := range findings {
		if findingActive(finding.Status) {
			foundAt[finding.Station+"\x00"+stringValue(finding.Question)] = true
		}
	}

	out := []contract.StationState{}
	current := -1

	for _, station := range definitions {
		if !stationApplies(station, ship) {
			continue
		}

		var required, answered, resolved, conflicts int

		failed := false

		for _, question := range station.Questions {
			if question.Required != 1 {
				continue
			}

			required++

			values := answersAt[station.Key+"\x00"+question.Key]
			if len(values) == 0 {
				continue
			}

			answered++

			switch {
			case len(values) > 1:
				conflicts++
				failed = true
			case values["fail"] && !foundAt[station.Key+"\x00"+question.Prompt]:
				failed = true
			default:
				resolved++
			}
		}

		done := required == resolved
		status := "todo"

		switch {
		case done:
			status = "done"
		case failed:
			status = "blocked"

			if current == -1 {
				current = len(out)
			}
		case current == -1:
			current = len(out)
			status = "current"
		}

		out = append(out, contract.StationState{
			Key:          station.Key,
			Label:        station.Label,
			Sort:         station.Sort,
			Status:       status,
			Answered:     answered,
			Required:     required,
			Conflicts:    conflicts,
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

	stage := stationStage(r)

	definitions, err := a.stationDefinitionsWith(r.Context(), person.ProgramID, stage, r.URL.Query().Get("all") == "1")
	if err != nil {
		writeStorage(w, err)

		return
	}

	counts, err := a.stationCounts(r.Context(), person.ProgramID, stage)
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
			Stage:       station.Stage,
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

	stage := stationStage(r)

	if !found || station.Stage != stage {
		app.WriteError(w, http.StatusNotFound, "unknown station")
		return
	}

	items, hasMore, err := a.stationQueueItems(r.Context(), person, stage, station.Key)
	if err != nil {
		writeStorage(w, err)

		return
	}

	counts, err := a.stationCounts(r.Context(), person.ProgramID, stage)
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
		on conflict(submission_id, station_key, question_key, reviewer_id) do update set
			answer = excluded.answer, evidence = excluded.evidence, artifact_hash = excluded.artifact_hash,
			updated_at = excluded.updated_at`,
		id, ship.ID, station.Key, questionKey, request.Answer, request.Evidence, nullString(stringValue(request.ArtifactHash)), person.ID, now, now); err != nil {
		writeStorage(w, err)

		return
	}

	if _, err := a.store.Exec(r.Context(), `insert into submission_event (id, submission_id, kind, actor_id, actor_email, data, created_at) values (?, ?, 'station_answered', ?, ?, ?, ?)`,
		newID("evt"), ship.ID, person.ID, person.Email,
		marshalMap(map[string]any{"station": station.Key, "question": questionKey, "answer": request.Answer}), now); err != nil {
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

	answer, found := answerAt(answers, station.Key, questionKey, person.ID)
	if !found {
		app.WriteError(w, http.StatusInternalServerError, "could not read the saved answer")
		return
	}

	app.WriteJSON(w, http.StatusOK, contract.StationAnswerResult{Answer: answer, States: states, CurrentStation: current})
}

// handleStationAnswerDelete removes the reviewer's answer so the question is unanswered again.
func (a *ireApp) handleStationAnswerDelete(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requireReviewerWrite(w, r)
	if !ok {
		return
	}

	ship, ok := a.loadShipForReview(w, r)
	if !ok {
		return
	}

	if !a.holdsClaim(r.Context(), ship.ID, person.ID) {
		app.WriteError(w, http.StatusConflict, "claim the ship before clearing a station answer")
		return
	}

	version, err := strconv.Atoi(r.URL.Query().Get("version"))
	if err != nil {
		app.WriteError(w, http.StatusBadRequest, "version must be a number")
		return
	}

	if version != ship.Version {
		app.WriteErrorCode(w, http.StatusConflict, "stale_version", "this ship changed since you opened it; reload before clearing")
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

	now := a.nowText()

	if _, err := a.store.Exec(r.Context(), `delete from station_answer where submission_id = ? and station_key = ? and question_key = ? and reviewer_id = ?`,
		ship.ID, station.Key, questionKey, person.ID); err != nil {
		writeStorage(w, err)

		return
	}

	if _, err := a.store.Exec(r.Context(), `insert into submission_event (id, submission_id, kind, actor_id, actor_email, data, created_at) values (?, ?, 'station_answer_cleared', ?, ?, ?, ?)`,
		newID("evt"), ship.ID, person.ID, person.Email,
		marshalMap(map[string]any{"station": station.Key, "question": questionKey}), now); err != nil {
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

	app.WriteJSON(w, http.StatusOK, contract.StationAnswerDeleteResult{States: states, CurrentStation: current})
}

// answerAt finds the reviewer's own saved answer.
func answerAt(answers []contract.StationAnswer, stationKey, questionKey, reviewerID string) (contract.StationAnswer, bool) {
	for _, answer := range answers {
		if answer.StationKey == stationKey && answer.QuestionKey == questionKey && stringValue(answer.ReviewerID) == reviewerID {
			return answer, true
		}
	}

	return contract.StationAnswer{}, false
}

// handleStationConflictResolve makes every reviewer's answer to one question match
// the organiser's verdict, clearing a merge conflict in one action.
func (a *ireApp) handleStationConflictResolve(w http.ResponseWriter, r *http.Request) {
	person, ok := a.requirePermission(w, r, permSecondPass, "only organisers can resolve a station conflict")
	if !ok {
		return
	}

	ship, ok := a.loadShipForReview(w, r)
	if !ok {
		return
	}

	if !a.holdsClaim(r.Context(), ship.ID, person.ID) {
		app.WriteError(w, http.StatusConflict, "claim the ship before resolving a conflict")
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

	if _, err := a.store.Transaction(r.Context(),
		storage.TransactionStatement{SQL: `update station_answer set answer = ?, updated_at = ?
			where submission_id = ? and station_key = ? and question_key = ?`,
			Args: []any{request.Answer, now, ship.ID, station.Key, questionKey}},
		storage.TransactionStatement{SQL: `insert into station_answer (id, submission_id, station_key, question_key, answer, evidence, artifact_hash, reviewer_id, created_at, updated_at)
			values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			on conflict(submission_id, station_key, question_key, reviewer_id) do update set
				answer = excluded.answer, evidence = excluded.evidence, artifact_hash = excluded.artifact_hash,
				updated_at = excluded.updated_at`,
			Args: []any{newID("sta"), ship.ID, station.Key, questionKey, request.Answer, request.Evidence, nullString(stringValue(request.ArtifactHash)), person.ID, now, now}},
		storage.TransactionStatement{SQL: `insert into submission_event (id, submission_id, kind, actor_id, actor_email, data, created_at) values (?, ?, 'station_conflict_resolved', ?, ?, ?, ?)`,
			Args: []any{newID("evt"), ship.ID, person.ID, person.Email, marshalMap(map[string]any{"station": station.Key, "question": questionKey, "answer": request.Answer}), now}},
	); err != nil {
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

	answer, found := answerAt(answers, station.Key, questionKey, person.ID)
	if !found {
		app.WriteError(w, http.StatusInternalServerError, "could not read the saved answer")
		return
	}

	app.WriteJSON(w, http.StatusOK, contract.StationAnswerResult{Answer: answer, States: states, CurrentStation: current})
}
