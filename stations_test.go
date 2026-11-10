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

	passed := answerQuestion(t, application, cookie, bundle.Submission.ID, "time", first.Key, "pass", bundle.Submission.Version)
	if passed.Answer.Answer != "pass" {
		t.Fatalf("answer = %+v", passed.Answer)
	}

	for _, question := range timeStation.Questions[1:] {
		if question.Required == 1 {
			answerQuestion(t, application, cookie, bundle.Submission.ID, "time", question.Key, "pass", bundle.Submission.Version)
		}
	}

	afterTime := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/sub_fixture_1", nil, cookie, nil))
	if afterTime.CurrentStation == nil || *afterTime.CurrentStation != "narrative" {
		t.Fatalf("current station after time = %v, want narrative", afterTime.CurrentStation)
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
	}

	if !synthesisBlocked {
		t.Fatalf("synthesis state after fail = %+v, want blocked", failed.StationStates)
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
