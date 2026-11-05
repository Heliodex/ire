package main

import (
	"net/http"
	"testing"

	"review/contract"
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
