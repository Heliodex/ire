package main

import (
	"encoding/json/v2"
	"net/http"
	"testing"

	"review/contract"
)

// TestReviewerProfile covers identity, permissions, decision stats and the recent review list.
func TestReviewerProfile(t *testing.T) {
	application := newTestApp(t)
	reviewer := login(t, application, "reviewer@example.com")

	if unauthorized := do(t, application, http.MethodGet, testBase+"/reviewers/reviewer@example.com", nil, nil, nil); unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated profile = %d: %s", unauthorized.Code, unauthorized.Body.String())
	}

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, reviewer, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	hours := 5 * 3600

	decision, _ := json.Marshal(contract.DecisionRequest{
		Decision:             decisionApproved,
		HoursApprovedSeconds: &hours,
		IdempotencyKey:       "profile-decision",
		NoteToMaker:          "Clean board.",
		Version:              1,
	})

	if made := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/decision", decision, reviewer, nil); made.Code != http.StatusOK {
		t.Fatalf("decision = %d: %s", made.Code, made.Body.String())
	}

	profile := decode[contract.ReviewerProfile](t, do(t, application, http.MethodGet, testBase+"/reviewers/reviewer@example.com", nil, reviewer, nil))
	if profile.Email != "reviewer@example.com" || profile.Role == nil || *profile.Role != roleReviewer {
		t.Fatalf("profile = %+v", profile)
	}

	if profile.Active == nil || !*profile.Active {
		t.Errorf("profile active = %v", profile.Active)
	}

	if len(profile.Tracks) == 0 || profile.Tracks[0] != "hardware" {
		t.Errorf("profile tracks = %+v", profile.Tracks)
	}

	if profile.Stats.Total != 1 || profile.Stats.Approved != 1 || profile.Stats.HoursApprovedSeconds != hours {
		t.Errorf("profile stats = %+v", profile.Stats)
	}

	if profile.ReviewTotal != 1 || len(profile.Reviews) != 1 {
		t.Fatalf("profile reviews = %+v", profile.Reviews)
	}

	if profile.Reviews[0].SubmissionTitle != "Starbie" || profile.Reviews[0].Decision != decisionApproved {
		t.Errorf("profile review = %+v", profile.Reviews[0])
	}

	if profile.Reviews[0].HoursApprovedSeconds == nil || *profile.Reviews[0].HoursApprovedSeconds != hours {
		t.Errorf("profile review hours = %+v", profile.Reviews[0].HoursApprovedSeconds)
	}

	if profile.ID == nil {
		t.Fatal("profile id is nil")
	}

	byID := decode[contract.ReviewerProfile](t, do(t, application, http.MethodGet, testBase+"/reviewers/"+*profile.ID, nil, reviewer, nil))
	if byID.Email != profile.Email || byID.Stats.Total != profile.Stats.Total {
		t.Errorf("profile by id = %+v", byID)
	}

	admin := decode[contract.ReviewerProfile](t, do(t, application, http.MethodGet, testBase+"/reviewers/admin@example.com", nil, reviewer, nil))
	if admin.Role == nil || *admin.Role != roleAdmin {
		t.Errorf("admin profile = %+v", admin)
	}

	if missing := do(t, application, http.MethodGet, testBase+"/reviewers/nobody@example.com", nil, reviewer, nil); missing.Code != http.StatusNotFound {
		t.Errorf("missing profile = %d: %s", missing.Code, missing.Body.String())
	}
}
