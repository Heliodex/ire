package main

import (
	"encoding/json/v2"
	"net/http"
	"testing"

	"ire/contract"
)

// stationByKey finds a station in the bundle.
func stationByKey(bundle contract.ReviewBundle, key string) contract.StationDefinition {
	for _, station := range bundle.Stations {
		if station.Key == key {
			return station
		}
	}

	return contract.StationDefinition{}
}

// answerQuestion saves one station answer.
func answerQuestion(t *testing.T, application *ireApp, cookie *http.Cookie, submissionID, station, question, answer string, version int) contract.StationAnswerResult {
	t.Helper()

	body, _ := json.Marshal(contract.StationAnswerRequest{Answer: answer, Evidence: "Checked in the evidence.", Version: version})
	path := testBase + "/submissions/" + submissionID + "/stations/" + station + "/answers/" + question

	recorder := do(t, application, http.MethodPut, path, body, cookie, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("answer %s/%s = %d: %s", station, question, recorder.Code, recorder.Body.String())
	}

	return decode[contract.StationAnswerResult](t, recorder)
}

// TestStationFlowMovesThroughTheQueue covers the stations list, answer storage and station derivation.
func TestStationFlowMovesThroughTheQueue(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	stations := decode[contract.StationsResponse](t, do(t, application, http.MethodGet, testBase+"/stations", nil, cookie, nil))
	if len(stations.Stations) < 5 {
		t.Fatalf("stations = %d, want the five stage 1 stations", len(stations.Stations))
	}

	if triage := stations.Stations[0]; triage.Key != "triage" || triage.Count != 3 {
		t.Fatalf("first station = %+v, want triage with 3 waiting", triage)
	}

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, cookie, nil))
	if bundle.CurrentStation == nil || *bundle.CurrentStation != "triage" {
		t.Fatalf("current station = %v, want triage", bundle.CurrentStation)
	}

	triage := stationByKey(bundle, "triage")
	for _, question := range triage.Questions {
		if question.Required == 1 {
			answerQuestion(t, application, cookie, bundle.Submission.ID, "triage", question.Key, "pass", bundle.Submission.Version)
		}
	}

	afterTriage := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, cookie, nil))
	if afterTriage.CurrentStation == nil || *afterTriage.CurrentStation != "time" {
		t.Fatalf("current station after triage = %v, want time", afterTriage.CurrentStation)
	}

	timeStation := stationByKey(afterTriage, "time")
	first := timeStation.Questions[0]

	failed := answerQuestion(t, application, cookie, bundle.Submission.ID, "time", first.Key, "fail", bundle.Submission.Version)
	if failed.CurrentStation == nil || *failed.CurrentStation != "time" {
		t.Fatalf("current after fail = %v, want time", failed.CurrentStation)
	}

	var blocked bool

	for _, state := range failed.States {
		if state.Key == "time" && state.Status == "blocked" {
			blocked = true
		}
	}

	if !blocked {
		t.Fatalf("time state = %+v, want blocked", failed.States)
	}

	// A recorded finding lets the failed station advance with its flag.
	findingBody, _ := json.Marshal(contract.FindingCreateRequest{
		MakerAction: stringPtr("Count only the productive journal time."),
		Question:    stringPtr(first.Prompt),
		Severity:    "fixable",
		Station:     "time",
		Summary:     "Tutorial consumption in the journals.",
	})

	if created := do(t, application, http.MethodPost, testBase+"/submissions/"+bundle.Submission.ID+"/findings", findingBody, cookie, nil); created.Code != http.StatusOK {
		t.Fatalf("create finding = %d: %s", created.Code, created.Body.String())
	}

	for _, question := range timeStation.Questions[1:] {
		if question.Required == 1 {
			answerQuestion(t, application, cookie, bundle.Submission.ID, "time", question.Key, "pass", bundle.Submission.Version)
		}
	}

	afterFinding := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, cookie, nil))
	if afterFinding.CurrentStation == nil || *afterFinding.CurrentStation != "narrative" {
		t.Fatalf("current station after finding = %v, want narrative", afterFinding.CurrentStation)
	}

	for _, state := range afterFinding.StationStates {
		if state.Key == "time" && state.Status != "done" {
			t.Fatalf("time state = %+v, want done with an open finding", afterFinding.StationStates)
		}
	}
}

// TestStationAnswerClear removes one verdict and reopens its station.
func TestStationAnswerClear(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, cookie, nil))
	triage := stationByKey(bundle, "triage")
	first := triage.Questions[0]
	path := testBase + "/submissions/" + bundle.Submission.ID + "/stations/triage/answers/" + first.Key

	answerQuestion(t, application, cookie, bundle.Submission.ID, "triage", first.Key, "pass", bundle.Submission.Version)

	cleared := do(t, application, http.MethodDelete, path+"?version=1", nil, cookie, nil)
	if cleared.Code != http.StatusOK {
		t.Fatalf("clear = %d: %s", cleared.Code, cleared.Body.String())
	}

	result := decode[contract.StationAnswerDeleteResult](t, cleared)
	if result.CurrentStation == nil || *result.CurrentStation != "triage" {
		t.Fatalf("current = %v, want triage", result.CurrentStation)
	}

	after := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, cookie, nil))
	for _, answer := range after.StationAnswers {
		if answer.StationKey == "triage" && answer.QuestionKey == first.Key {
			t.Fatalf("answer still present = %+v", answer)
		}
	}

	stale := do(t, application, http.MethodDelete, path+"?version=99", nil, cookie, nil)
	if stale.Code != http.StatusConflict {
		t.Fatalf("stale clear = %d, want 409", stale.Code)
	}

	other := login(t, application, "organiser@example.com")
	unclaimed := do(t, application, http.MethodDelete, path+"?version=1", nil, other, nil)
	if unclaimed.Code != http.StatusConflict {
		t.Fatalf("unclaimed clear = %d, want 409", unclaimed.Code)
	}

	missing := do(t, application, http.MethodDelete, testBase+"/submissions/"+bundle.Submission.ID+"/stations/triage/answers/nope?version=1", nil, cookie, nil)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("unknown question clear = %d, want 404", missing.Code)
	}
}

// TestStationQueueFiltersByCurrentStation covers the focused station queues.
func TestStationQueueFiltersByCurrentStation(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	queue := decode[contract.StationQueueResponse](t, do(t, application, http.MethodGet, testBase+"/stations/triage/queue", nil, cookie, nil))
	if queue.Station.Key != "triage" || len(queue.Items) != 3 {
		t.Fatalf("triage queue = %+v, want 3 ships", queue)
	}

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, cookie, nil))
	triage := stationByKey(bundle, "triage")

	for _, question := range triage.Questions {
		if question.Required == 1 {
			answerQuestion(t, application, cookie, bundle.Submission.ID, "triage", question.Key, "pass", bundle.Submission.Version)
		}
	}

	queue = decode[contract.StationQueueResponse](t, do(t, application, http.MethodGet, testBase+"/stations/triage/queue", nil, cookie, nil))
	if len(queue.Items) != 2 {
		t.Fatalf("triage queue after one ship = %d, want 2", len(queue.Items))
	}

	timeQueue := decode[contract.StationQueueResponse](t, do(t, application, http.MethodGet, testBase+"/stations/time/queue", nil, cookie, nil))
	if len(timeQueue.Items) != 1 || timeQueue.Items[0].ID != bundle.Submission.ID {
		t.Fatalf("time queue = %+v", timeQueue.Items)
	}
}

// TestStationQueueSearchAndSort covers the filter bar query on a focused station queue.
func TestStationQueueSearchAndSort(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	miss := decode[contract.StationQueueResponse](t, do(t, application, http.MethodGet, testBase+"/stations/triage/queue?q=no-such-ship", nil, cookie, nil))
	if len(miss.Items) != 0 {
		t.Fatalf("filtered queue = %d items, want 0", len(miss.Items))
	}

	sorted := decode[contract.StationQueueResponse](t, do(t, application, http.MethodGet, testBase+"/stations/triage/queue?sort=maker&dir=desc", nil, cookie, nil))
	if len(sorted.Items) != 3 {
		t.Fatalf("sorted queue = %d items, want 3", len(sorted.Items))
	}

	for index := 1; index < len(sorted.Items); index++ {
		if sorted.Items[index-1].MakerName < sorted.Items[index].MakerName {
			t.Fatalf("queue not maker-descending: %+v", sorted.Items)
		}
	}

	bad := do(t, application, http.MethodGet, testBase+"/stations/triage/queue?sort=bogus", nil, cookie, nil)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("bad sort = %d, want 400", bad.Code)
	}
}

// TestStationStatusReflectsEachSection covers the per-section badge while an earlier station is still current.
func TestStationStatusReflectsEachSection(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, cookie, nil))
	synthesis := stationByKey(bundle, "synthesis")

	for _, question := range synthesis.Questions {
		if question.Required == 1 {
			answerQuestion(t, application, cookie, bundle.Submission.ID, "synthesis", question.Key, "pass", bundle.Submission.Version)
		}
	}

	after := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, cookie, nil))
	if after.CurrentStation == nil || *after.CurrentStation != "triage" {
		t.Fatalf("current = %v, want triage", after.CurrentStation)
	}

	var synthesisDone bool

	for _, state := range after.StationStates {
		if state.Key == "synthesis" && state.Status == "done" {
			synthesisDone = true
		}
	}

	if !synthesisDone {
		t.Fatalf("synthesis state = %+v, want done", after.StationStates)
	}

	answerQuestion(t, application, cookie, bundle.Submission.ID, "synthesis", synthesis.Questions[0].Key, "fail", bundle.Submission.Version)

	failed := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, cookie, nil))

	var synthesisBlocked bool

	for _, state := range failed.StationStates {
		if state.Key == "synthesis" && state.Status == "blocked" {
			synthesisBlocked = true
		}

		if state.Key == "synthesis" && state.Answered != 3 {
			t.Fatalf("synthesis answered = %d, want 3 including the fail", state.Answered)
		}
	}

	if !synthesisBlocked {
		t.Fatalf("synthesis state after fail = %+v, want blocked", failed.StationStates)
	}

	recordBody, _ := json.Marshal(contract.FindingCreateRequest{
		MakerAction: stringPtr("Record one decision with its justification."),
		Question:    stringPtr(synthesis.Questions[0].Prompt),
		Severity:    "fixable",
		Station:     "synthesis",
		Summary:     "The synthesis record is missing.",
	})

	if created := do(t, application, http.MethodPost, testBase+"/submissions/"+bundle.Submission.ID+"/findings", recordBody, cookie, nil); created.Code != http.StatusOK {
		t.Fatalf("create finding = %d: %s", created.Code, created.Body.String())
	}

	recorded := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, cookie, nil))

	for _, state := range recorded.StationStates {
		if state.Key == "synthesis" && state.Status != "done" {
			t.Fatalf("synthesis state after finding = %+v, want done", recorded.StationStates)
		}

		if state.Key == "synthesis" && state.OpenFindings != 1 {
			t.Fatalf("synthesis open findings = %d, want 1", state.OpenFindings)
		}
	}
}

// TestOpenFindingsDoNotPinStations covers flags that travel with the ship: an open
// finding keeps its station done and gates the synthesis decision instead.
func TestOpenFindingsDoNotPinStations(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, cookie, nil))
	triage := stationByKey(bundle, "triage")

	for _, question := range triage.Questions {
		if question.Required == 1 {
			answerQuestion(t, application, cookie, bundle.Submission.ID, "triage", question.Key, "pass", bundle.Submission.Version)
		}
	}

	flagBody, _ := json.Marshal(contract.FindingCreateRequest{
		MakerAction: stringPtr("Add a real cover screenshot."),
		Question:    stringPtr(triage.Questions[0].Prompt),
		Severity:    "fixable",
		Station:     "triage",
		Summary:     "The cover looks like a template.",
	})

	if created := do(t, application, http.MethodPost, testBase+"/submissions/"+bundle.Submission.ID+"/findings", flagBody, cookie, nil); created.Code != http.StatusOK {
		t.Fatalf("create finding = %d: %s", created.Code, created.Body.String())
	}

	after := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, cookie, nil))
	if after.CurrentStation == nil || *after.CurrentStation != "time" {
		t.Fatalf("current = %v, want time with an open finding behind", after.CurrentStation)
	}

	for _, state := range after.StationStates {
		if state.Key == "triage" && (state.Status != "done" || state.OpenFindings != 1) {
			t.Fatalf("triage state = %+v, want done with 1 open finding", after.StationStates)
		}
	}
}

// TestStationAnswerGuards covers the claim, question and version checks.
func TestStationAnswerGuards(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")
	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, cookie, nil))

	body, _ := json.Marshal(contract.StationAnswerRequest{Answer: "pass", Evidence: "Checked.", Version: bundle.Submission.Version})
	path := testBase + "/submissions/sub_fixture_1/stations/triage/answers/cover_screenshot"

	if unclaimed := do(t, application, http.MethodPut, path, body, cookie, nil); unclaimed.Code != http.StatusConflict {
		t.Fatalf("unclaimed answer = %d: %s", unclaimed.Code, unclaimed.Body.String())
	}

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, cookie, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	unknown := testBase + "/submissions/sub_fixture_1/stations/triage/answers/not_a_question"

	if missing := do(t, application, http.MethodPut, unknown, body, cookie, nil); missing.Code != http.StatusNotFound {
		t.Fatalf("unknown question = %d: %s", missing.Code, missing.Body.String())
	}

	staleBody, _ := json.Marshal(contract.StationAnswerRequest{Answer: "pass", Evidence: "Checked.", Version: 99})
	if stale := do(t, application, http.MethodPut, path, staleBody, cookie, nil); stale.Code != http.StatusConflict {
		t.Fatalf("stale answer = %d: %s", stale.Code, stale.Body.String())
	}
}

// TestStationAnswersArePerReviewerAndConflict covers the exception path: a second
// reviewer can answer a station, and disagreement blocks it until the answers converge.
func TestStationAnswersArePerReviewerAndConflict(t *testing.T) {
	application := newTestApp(t)
	firstReviewer := login(t, application, "reviewer@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, firstReviewer, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, firstReviewer, nil))
	triage := stationByKey(bundle, "triage")
	firstQuestion := triage.Questions[0]

	for _, question := range triage.Questions {
		if question.Required == 1 {
			answerQuestion(t, application, firstReviewer, bundle.Submission.ID, "triage", question.Key, "pass", bundle.Submission.Version)
		}
	}

	if after := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, firstReviewer, nil)); after.CurrentStation == nil || *after.CurrentStation != "time" {
		t.Fatalf("current after first reviewer = %v, want time", after.CurrentStation)
	}

	if release := do(t, application, http.MethodDelete, testBase+"/submissions/sub_fixture_1/claim", nil, firstReviewer, nil); release.Code != http.StatusNoContent {
		t.Fatalf("release = %d: %s", release.Code, release.Body.String())
	}

	secondReviewer := login(t, application, "organiser@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, secondReviewer, nil); claim.Code != http.StatusOK {
		t.Fatalf("second claim = %d: %s", claim.Code, claim.Body.String())
	}

	failed := answerQuestion(t, application, secondReviewer, bundle.Submission.ID, "triage", firstQuestion.Key, "fail", bundle.Submission.Version)

	var conflicted bool

	for _, state := range failed.States {
		if state.Key == "triage" && state.Status == "blocked" && state.Conflicts == 1 {
			conflicted = true
		}
	}

	if !conflicted {
		t.Fatalf("states = %+v, want triage blocked with one conflict", failed.States)
	}

	if failed.CurrentStation == nil || *failed.CurrentStation != "triage" {
		t.Fatalf("current after conflict = %v, want triage", failed.CurrentStation)
	}

	withAnswers := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, secondReviewer, nil))
	competing := 0

	for _, answer := range withAnswers.StationAnswers {
		if answer.StationKey == "triage" && answer.QuestionKey == firstQuestion.Key {
			competing++

			if answer.ReviewerName == nil || *answer.ReviewerName == "" {
				t.Fatalf("answer = %+v, want a reviewer name", answer)
			}
		}
	}

	if competing != 2 {
		t.Fatalf("competing answers = %d, want 2", competing)
	}

	resolved := answerQuestion(t, application, secondReviewer, bundle.Submission.ID, "triage", firstQuestion.Key, "pass", bundle.Submission.Version)

	for _, state := range resolved.States {
		if state.Key == "triage" && (state.Status != "done" || state.Conflicts != 0) {
			t.Fatalf("resolved triage = %+v, want done without conflicts", state)
		}
	}
}

// TestAdminAuthorsStations covers the station editor API: create, read and delete.
func TestAdminAuthorsStations(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "admin@example.com")

	station := contract.StationDefinition{
		Active:      1,
		Description: "A test station.",
		Key:         "test_station",
		Label:       "Test station",
		Questions: []contract.StationQuestion{
			{EvidenceKind: "text", Key: "first", Prompt: "Is the first thing right?", Required: 1, Sort: 0},
		},
		Sort:   90,
		Stage:  1,
		Themes: []string{},
	}

	body, _ := json.Marshal(station)
	if saved := do(t, application, http.MethodPut, testBase+"/stations/test_station", body, cookie, nil); saved.Code != http.StatusOK {
		t.Fatalf("save station = %d: %s", saved.Code, saved.Body.String())
	}

	loaded := decode[contract.StationDefinition](t, do(t, application, http.MethodGet, testBase+"/stations/test_station", nil, cookie, nil))
	if loaded.Label != station.Label || len(loaded.Questions) != 1 || loaded.Questions[0].Key != "first" {
		t.Fatalf("station = %+v", loaded)
	}

	if deleted := do(t, application, http.MethodDelete, testBase+"/stations/test_station", nil, cookie, nil); deleted.Code != http.StatusNoContent {
		t.Fatalf("delete station = %d: %s", deleted.Code, deleted.Body.String())
	}

	if missing := do(t, application, http.MethodGet, testBase+"/stations/test_station", nil, cookie, nil); missing.Code != http.StatusNotFound {
		t.Fatalf("station after delete = %d, want 404", missing.Code)
	}

	reviewer := login(t, application, "reviewer@example.com")
	if denied := do(t, application, http.MethodPut, testBase+"/stations/test_station", body, reviewer, nil); denied.Code != http.StatusForbidden {
		t.Fatalf("reviewer save station = %d, want 403", denied.Code)
	}
}

// TestOrganiserResolvesStationConflict covers the override path for disagreeing reviewers.
func TestOrganiserResolvesStationConflict(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, reviewer, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, reviewer, nil))
	triage := stationByKey(bundle, "triage")
	firstQuestion := triage.Questions[0]

	for _, question := range triage.Questions {
		if question.Required == 1 {
			answerQuestion(t, application, reviewer, bundle.Submission.ID, "triage", question.Key, "pass", bundle.Submission.Version)
		}
	}

	if release := do(t, application, http.MethodDelete, testBase+"/submissions/sub_fixture_1/claim", nil, reviewer, nil); release.Code != http.StatusNoContent {
		t.Fatalf("release = %d", release.Code)
	}

	organiser := login(t, application, "organiser@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, organiser, nil); claim.Code != http.StatusOK {
		t.Fatalf("organiser claim = %d: %s", claim.Code, claim.Body.String())
	}

	answerQuestion(t, application, organiser, bundle.Submission.ID, "triage", firstQuestion.Key, "fail", bundle.Submission.Version)

	body, _ := json.Marshal(contract.StationAnswerRequest{Answer: "pass", Evidence: "Organiser resolved.", Version: bundle.Submission.Version})
	path := testBase + "/submissions/sub_fixture_1/stations/triage/answers/" + firstQuestion.Key + "/resolve"

	resolved := decode[contract.StationAnswerResult](t, do(t, application, http.MethodPost, path, body, organiser, nil))

	for _, state := range resolved.States {
		if state.Key == "triage" && (state.Status != "done" || state.Conflicts != 0) {
			t.Fatalf("triage = %+v, want done without conflicts", state)
		}
	}

	after := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, organiser, nil))
	for _, answer := range after.StationAnswers {
		if answer.StationKey == "triage" && answer.QuestionKey == firstQuestion.Key && answer.Answer != "pass" {
			t.Fatalf("answer = %+v, want pass", answer)
		}
	}
}
