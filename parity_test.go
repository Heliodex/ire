package main

import (
	"net/http"
	"testing"

	"review/contract"
)

// TestParityFeed covers the machine guard, the open queue and the values the Half Life script compares.
func TestParityFeed(t *testing.T) {
	application := newTestApp(t)

	if unauthorized := do(t, application, http.MethodGet, testBase+"/parity", nil, nil, nil); unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("parity without a token = %d: %s", unauthorized.Code, unauthorized.Body.String())
	}

	recorder := do(t, application, http.MethodGet, testBase+"/parity", nil, nil, map[string]string{
		"Authorization": "Bearer test-ingest",
	})
	response := decode[contract.ParityResponse](t, recorder)

	if len(response.Ships) != 3 || response.GeneratedAt == "" {
		t.Fatalf("parity = %+v", response)
	}

	var found bool

	for _, ship := range response.Ships {
		if ship.ExternalID != "fix-clean:design" {
			continue
		}

		found = true

		if ship.HoursClaimedSeconds <= 0 || ship.BomTotalCents <= 0 || ship.Phase == "" {
			t.Errorf("clean ship = %+v", ship)
		}
	}

	if !found {
		t.Error("clean fixture missing from parity")
	}

	body := []byte(`{"external_id":"fix-clean:design"}`)

	withdraw := do(t, application, http.MethodPost, "/api/ingest/half-life/withdraw", body, nil, map[string]string{
		"X-Ari-Signature": hmacHex("test-ingest", body),
	})
	if withdraw.Code != http.StatusOK {
		t.Fatalf("withdraw = %d: %s", withdraw.Code, withdraw.Body.String())
	}

	after := decode[contract.ParityResponse](t, do(t, application, http.MethodGet, testBase+"/parity", nil, nil, map[string]string{
		"Authorization": "Bearer test-ingest",
	}))

	if len(after.Ships) != 2 {
		t.Errorf("open ships after withdraw = %d", len(after.Ships))
	}
}
