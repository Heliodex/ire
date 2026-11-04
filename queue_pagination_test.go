package main

import (
	"net/http"
	"testing"

	"review/contract"
)

// TestQueuePagination covers limit, offset and the hasMore flag.
func TestQueuePagination(t *testing.T) {
	application := newTestApp(t)
	cookie := login(t, application, "reviewer@example.com")

	first := decode[contract.QueueResponse](t, do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all&limit=1&offset=0", nil, cookie, nil))
	if len(first.Items) != 1 || !first.HasMore {
		t.Fatalf("first page = %+v", first)
	}

	second := decode[contract.QueueResponse](t, do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all&limit=1&offset=1", nil, cookie, nil))
	if len(second.Items) != 1 || second.Items[0].ID == first.Items[0].ID {
		t.Fatalf("second page = %+v", second)
	}

	last := decode[contract.QueueResponse](t, do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all&limit=50&offset=3", nil, cookie, nil))
	if len(last.Items) != 0 || last.HasMore {
		t.Fatalf("past the end = %+v", last)
	}

	clamped := decode[contract.QueueResponse](t, do(t, application, http.MethodGet, testBase+"/queue?stage=1&filter=all&offset=999999", nil, cookie, nil))
	if len(clamped.Items) != 0 {
		t.Fatalf("clamped offset = %+v", clamped)
	}
}
