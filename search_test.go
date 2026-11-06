package main

import (
	"net/http"
	"testing"

	"ire/contract"
)

// TestSearchFindsProjectByTitleMakerAndExternalID covers the header search matching and escaping.
func TestSearchFindsProjectByTitleMakerAndExternalID(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	cases := []struct{ query, want string }{
		{query: "starbie", want: "sub_fixture_1"},
		{query: "shubhayu", want: "sub_fixture_2"},
		{query: "fix-blocked", want: "sub_fixture_3"},
	}

	for _, test := range cases {
		recorder := do(t, application, http.MethodGet, testBase+"/search?q="+test.query, nil, cookie, nil)
		if recorder.Code != http.StatusOK {
			t.Fatalf("search %q = %d: %s", test.query, recorder.Code, recorder.Body.String())
		}

		response := decode[contract.SearchResponse](t, recorder)
		if len(response.Results) != 1 || response.Results[0].ID != test.want {
			t.Errorf("search %q = %+v, want %s", test.query, response.Results, test.want)
		}
	}

	escaped := do(t, application, http.MethodGet, testBase+"/search?q=%25", nil, cookie, nil)
	if escaped.Code != http.StatusOK {
		t.Fatalf("escaped search = %d: %s", escaped.Code, escaped.Body.String())
	}

	if response := decode[contract.SearchResponse](t, escaped); len(response.Results) != 0 {
		t.Errorf("escaped search returned %+v, want none", response.Results)
	}

	anonymous := do(t, application, http.MethodGet, testBase+"/search?q=starbie", nil, nil, nil)
	if anonymous.Code != http.StatusUnauthorized {
		t.Errorf("anonymous search = %d, want 401", anonymous.Code)
	}
}

// TestSearchReportsClaimHolder covers the claimed-by email the header search uses to lock claimed ships.
func TestSearchReportsClaimHolder(t *testing.T) {
	application := newTestApp(t)
	holder := login(t, application, "reviewer@example.com")
	other := login(t, application, "organiser@example.com")

	if claim := do(t, application, http.MethodPost, testBase+"/submissions/sub_fixture_1/claim", nil, holder, nil); claim.Code != http.StatusOK {
		t.Fatalf("claim = %d: %s", claim.Code, claim.Body.String())
	}

	recorder := do(t, application, http.MethodGet, testBase+"/search?q=starbie", nil, other, nil)
	if recorder.Code != http.StatusOK {
		t.Fatalf("search = %d: %s", recorder.Code, recorder.Body.String())
	}

	response := decode[contract.SearchResponse](t, recorder)
	if len(response.Results) != 1 || response.Results[0].ID != "sub_fixture_1" {
		t.Fatalf("search = %+v, want sub_fixture_1", response.Results)
	}

	if response.Results[0].ClaimedBy == nil || *response.Results[0].ClaimedBy != "reviewer@example.com" {
		t.Errorf("claimedBy = %+v, want reviewer@example.com", response.Results[0].ClaimedBy)
	}

	free := do(t, application, http.MethodGet, testBase+"/search?q=shubhayu", nil, other, nil)
	if free.Code != http.StatusOK {
		t.Fatalf("search = %d: %s", free.Code, free.Body.String())
	}

	if results := decode[contract.SearchResponse](t, free).Results; len(results) != 1 || results[0].ClaimedBy != nil {
		t.Errorf("unclaimed search = %+v, want no claimedBy", results)
	}
}
