package main

import (
	"net/http"
	"testing"

	"ire/contract"
)

// TestQueueCarriesPreviewFields checks the listing has the maker slack id and requested tier the review header shows,
// so the loading preview matches the loaded page.
func TestQueueCarriesPreviewFields(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	queue := decode[contract.QueueResponse](t, do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all&limit=50", nil, cookie, nil))

	for _, item := range queue.Items {
		if item.ID != "sub_fixture_1" {
			continue
		}

		if item.MakerSlackID == nil || *item.MakerSlackID == "" {
			t.Fatalf("%s makerSlackId = %v", item.Title, item.MakerSlackID)
		}

		if item.RequestedTier == nil || *item.RequestedTier == "" {
			t.Fatalf("%s requestedTier = %v", item.Title, item.RequestedTier)
		}

		if item.HoursJournalSeconds <= 0 || item.BomTotalCents <= 0 {
			t.Fatalf("%s stats = journalled %d, bom %d", item.Title, item.HoursJournalSeconds, item.BomTotalCents)
		}

		return
	}

	t.Fatal("sub_fixture_1 not in the queue")
}

// TestSearchCarriesPreviewFields checks search rows can fill the same review header fields as the queue.
func TestSearchCarriesPreviewFields(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	response := decode[contract.SearchResponse](t, do(t, application, http.MethodGet, testBase+"/search?q=starbie", nil, cookie, nil))
	if len(response.Results) != 1 {
		t.Fatalf("results = %+v", response.Results)
	}

	item := response.Results[0]

	if item.MakerEmail == nil || *item.MakerEmail == "" {
		t.Fatalf("makerEmail = %v", item.MakerEmail)
	}

	if item.MakerSlackID == nil || *item.MakerSlackID == "" {
		t.Fatalf("makerSlackId = %v", item.MakerSlackID)
	}

	if item.RequestedTier == nil || *item.RequestedTier == "" {
		t.Fatalf("requestedTier = %v", item.RequestedTier)
	}
}
