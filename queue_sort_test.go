package main

import (
	"ire/contract"
	"net/http"
	"slices"
	"testing"
)

// TestQueueSortOrdersPages covers server-side sorting across pages: concatenated pages match one full sorted fetch with no gaps or repeats.
func TestQueueSortOrdersPages(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")
	full := decode[contract.QueueResponse](t, do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all&sort=ship&dir=asc&limit=50&offset=0", nil, cookie, nil))

	if len(full.Items) != 3 || full.Items[0].Title != "OpenTrace" || full.Items[1].Title != "Smart Alarm Clock" || full.Items[2].Title != "Starbie" {
		t.Fatalf("sorted full fetch = %+v", full.Items)
	}

	first := decode[contract.QueueResponse](t, do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all&sort=ship&dir=asc&limit=2&offset=0", nil, cookie, nil))
	second := decode[contract.QueueResponse](t, do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all&sort=ship&dir=asc&limit=2&offset=2", nil, cookie, nil))

	if len(first.Items) != 2 || !first.HasMore {
		t.Fatalf("first page = %+v", first)
	}

	if len(second.Items) != 1 || second.HasMore {
		t.Fatalf("second page = %+v", second)
	}

	paged := slices.Concat(first.Items, second.Items)

	for i := range paged {
		if paged[i].ID != full.Items[i].ID {
			t.Fatalf("paged order = %+v, want %+v", paged, full.Items)
		}
	}

	if desc := decode[contract.QueueResponse](t, do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all&sort=ship&dir=desc&limit=50&offset=0", nil, cookie, nil)); len(desc.Items) != 3 || desc.Items[0].Title != "Starbie" || desc.Items[2].Title != "OpenTrace" {
		t.Fatalf("descending fetch = %+v", desc.Items)
	}

	if claimed := decode[contract.QueueResponse](t, do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all&sort=claimed&dir=asc&limit=50&offset=0", nil, cookie, nil)); len(claimed.Items) != 3 || claimed.Items[0].Title != "Starbie" || claimed.Items[2].Title != "Smart Alarm Clock" {
		t.Fatalf("claimed sort = %+v", claimed.Items)
	}

	if checks := decode[contract.QueueResponse](t, do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all&sort=checks&dir=asc&limit=50&offset=0", nil, cookie, nil)); len(checks.Items) != 3 || checks.Items[0].Title != "Starbie" || checks.Items[1].Title != "Smart Alarm Clock" || checks.Items[2].Title != "OpenTrace" {
		t.Fatalf("checks sort = %+v", checks.Items)
	}

	if invalid := do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all&sort=bogus", nil, cookie, nil); invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid sort = %d, want 400", invalid.Code)
	}

	if invalidDir := do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all&sort=ship&dir=sideways", nil, cookie, nil); invalidDir.Code != http.StatusBadRequest {
		t.Fatalf("invalid dir = %d, want 400", invalidDir.Code)
	}
}
