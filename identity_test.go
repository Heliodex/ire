package main

import (
	"encoding/json/v2"
	"net/http"
	"strings"
	"testing"

	"ire/contract"
)

// TestDuplicateAndIdentityChecks covers cross-ship reuse warnings.
func TestDuplicateAndIdentityChecks(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	first := ingestBody("identity-a:design")
	first = []byte(strings.Replace(string(first), "example/line-follower", "Shubhayu-Banerjee/Open-Trace", 1))
	first = []byte(strings.Replace(string(first), `"email":"maker@example.com"`, `"email":"a@example.com","hackatime_id":"U1","slack_id":"S1"`, 1))

	firstCreated := decode[contract.IngestAccepted](t, do(t, application, http.MethodPost, "/api/ingest/half-life", first, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", first),
	}))
	if firstCreated.ID == "" {
		t.Fatal("first ingest failed")
	}

	second := ingestBody("identity-b:design")
	second = []byte(strings.Replace(string(second), "example/line-follower", "Shubhayu-Banerjee/Open-Trace", 1))
	second = []byte(strings.Replace(string(second), `"email":"maker@example.com"`, `"email":"b@example.com","hackatime_id":"U1","slack_id":"S1"`, 1))

	secondCreated := decode[contract.IngestAccepted](t, do(t, application, http.MethodPost, "/api/ingest/half-life", second, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", second),
	}))

	bundle := decode[contract.ReviewBundle](t, do(t, application, http.MethodGet, testBase+"/submissions/"+secondCreated.ID, nil, cookie, nil))
	statuses := checkStatuses(bundle.Checks)

	if statuses["duplicate.project"] != statusWarn {
		t.Errorf("duplicate.project = %q", statuses["duplicate.project"])
	}

	if statuses["identity.match"] != statusWarn {
		t.Errorf("identity.match = %q", statuses["identity.match"])
	}

	var detail string

	for _, check := range bundle.Checks {
		if check.Key == "duplicate.project" && check.Detail != nil {
			detail = *check.Detail
		}
	}

	if !strings.Contains(detail, "identity-a") {
		t.Errorf("duplicate detail = %q", detail)
	}

	if _, err := json.Marshal(bundle.Checks); err != nil {
		t.Fatal(err)
	}
}
