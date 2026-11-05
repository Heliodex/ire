package main

import (
	"net/http"
	"testing"

	"review/contract"
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

	paged := append(first.Items, second.Items...)
	for i := range paged {
		if paged[i].ID != full.Items[i].ID {
			t.Fatalf("paged order = %+v, want %+v", paged, full.Items)
		}
	}

	desc := decode[contract.QueueResponse](t, do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all&sort=ship&dir=desc&limit=50&offset=0", nil, cookie, nil))
	if len(desc.Items) != 3 || desc.Items[0].Title != "Starbie" || desc.Items[2].Title != "OpenTrace" {
		t.Fatalf("descending fetch = %+v", desc.Items)
	}

	claimed := decode[contract.QueueResponse](t, do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all&sort=claimed&dir=asc&limit=50&offset=0", nil, cookie, nil))
	if len(claimed.Items) != 3 || claimed.Items[0].Title != "Starbie" || claimed.Items[2].Title != "Smart Alarm Clock" {
		t.Fatalf("claimed sort = %+v", claimed.Items)
	}

	checks := decode[contract.QueueResponse](t, do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all&sort=checks&dir=asc&limit=50&offset=0", nil, cookie, nil))
	if len(checks.Items) != 3 || checks.Items[0].Title != "Starbie" || checks.Items[1].Title != "Smart Alarm Clock" || checks.Items[2].Title != "OpenTrace" {
		t.Fatalf("checks sort = %+v", checks.Items)
	}

	invalid := do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all&sort=bogus", nil, cookie, nil)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid sort = %d, want 400", invalid.Code)
	}

	invalidDir := do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all&sort=ship&dir=sideways", nil, cookie, nil)
	if invalidDir.Code != http.StatusBadRequest {
		t.Fatalf("invalid dir = %d, want 400", invalidDir.Code)
	}
}
