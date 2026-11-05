package main

import (
	"net/http"
	"net/url"
	"testing"

	"ire/contract"
)

// TestMakerProfile covers the maker page feed: identity, latest ship per project and the unknown-maker branches.
func TestMakerProfile(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	profile := decode[contract.MakerProfile](t, do(t, application, http.MethodGet,
		testBase+"/makers/"+url.PathEscape("c6h@example.com"), nil, cookie, nil))
	if profile.Email != "c6h@example.com" || profile.Name == nil || *profile.Name == "" {
		t.Fatalf("maker identity = %+v", profile)
	}

	if len(profile.Submissions) == 0 {
		t.Fatal("maker should have submissions")
	}

	seen := map[string]bool{}

	for _, submission := range profile.Submissions {
		if submission.ID == "" || submission.ExternalID == "" || submission.Title == "" {
			t.Errorf("submission = %+v, want identity and title", submission)
		}

		if seen[submission.ExternalID] {
			t.Errorf("external id %q listed twice, want latest per project", submission.ExternalID)
		}

		seen[submission.ExternalID] = true
	}

	if unknown := do(t, application, http.MethodGet, testBase+"/makers/"+url.PathEscape("nobody@example.com"), nil, cookie, nil); unknown.Code != http.StatusNotFound {
		t.Errorf("unknown maker = %d, want 404", unknown.Code)
	}

	if anonymous := do(t, application, http.MethodGet, testBase+"/makers/"+url.PathEscape("c6h@example.com"), nil, nil, nil); anonymous.Code != http.StatusUnauthorized {
		t.Errorf("anonymous maker = %d, want 401", anonymous.Code)
	}
}
