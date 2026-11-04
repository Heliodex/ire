package main

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// handleMetrics exposes the pilot's counters in the Prometheus text format. It needs the machine bearer token, like the status feed.
func (a *reviewApp) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if !a.requireMachine(w, r) {
		return
	}

	body, err := a.metricsText(r.Context(), programID)
	if err != nil {
		writeStorage(w, err)

		return
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

// metricsText reads queue, review, check and delivery counters in one pass of small queries.
func (a *reviewApp) metricsText(ctx context.Context, id string) (string, error) {
	var out strings.Builder

	out.WriteString("# HELP review_queue_depth Open ships by stage and status.\n# TYPE review_queue_depth gauge\n")

	rows, err := a.store.Query(ctx, `select stage, status, count(*) from submission
		where program_id = ? and status not in ('decided','withdrawn') group by stage, status`, id)
	if err != nil {
		return "", err
	}

	open := 0

	for rows.Next() {
		var stage, count int

		var status string
		if err := rows.Scan(&stage, &status, &count); err != nil {
			rows.Close()
			return "", err
		}

		open += count
		fmt.Fprintf(&out, "review_queue_depth{stage=%q,status=%q} %d\n", strconv.Itoa(stage), status, count)
	}

	rows.Close()

	if err := rows.Err(); err != nil {
		return "", err
	}

	fmt.Fprintf(&out, "# HELP review_queue_open_total Open ships.\n# TYPE review_queue_open_total gauge\nreview_queue_open_total %d\n", open)

	var claims int
	if err := a.store.QueryRow(ctx, `select count(*) from claim where expires_at > ?`, a.nowText()).Scan(&claims); err == nil {
		fmt.Fprintf(&out, "# HELP review_claims_active Ships claimed right now.\n# TYPE review_claims_active gauge\nreview_claims_active %d\n", claims)
	}

	var firstReview, holds, reviews int

	err = a.store.QueryRow(ctx, `select count(*), coalesce(sum(held), 0) from review where stage = 1 and invalidated_at is null`).Scan(&reviews, &holds)
	if err != nil {
		return "", err
	}

	fmt.Fprintf(&out, "# HELP review_stage1_total Stage 1 reviews with a decision.\n# TYPE review_stage1_total counter\nreview_stage1_total %d\n", reviews)

	rate := 0.0
	if reviews > 0 {
		rate = float64(holds) / float64(reviews)
	}

	fmt.Fprintf(&out, "# HELP review_hold_rate Stage 1 reviews held for the second pass.\n# TYPE review_hold_rate gauge\nreview_hold_rate %.4f\n", rate)

	err = a.store.QueryRow(ctx, `select coalesce(avg((julianday(first.created_at) - julianday(s.ingested_at)) * 86400), 0)
		from submission s join (select submission_id, min(created_at) as created_at from review where stage = 1 and invalidated_at is null group by submission_id) first
		on first.submission_id = s.id where s.program_id = ?`, id).Scan(&firstReview)
	if err != nil {
		return "", err
	}

	fmt.Fprintf(&out, "# HELP review_time_to_first_review_seconds Mean wait for the first stage 1 decision.\n# TYPE review_time_to_first_review_seconds gauge\nreview_time_to_first_review_seconds %d\n", firstReview)

	var checkSeconds float64
	if err := a.store.QueryRow(ctx, `select coalesce(avg((julianday(finished_at) - julianday(started_at)) * 86400), 0)
		from check_run where status = 'succeeded' and finished_at is not null`).Scan(&checkSeconds); err == nil {
		fmt.Fprintf(&out, "# HELP review_check_duration_seconds Mean successful check run duration.\n# TYPE review_check_duration_seconds gauge\nreview_check_duration_seconds %.2f\n", checkSeconds)
	}

	out.WriteString("# HELP review_delivery_total Signed deliveries by status.\n# TYPE review_delivery_total counter\n")

	rows, err = a.store.Query(ctx, `select status, count(*) from webhook_outbox group by status`)
	if err != nil {
		return "", err
	}

	defer rows.Close()

	for rows.Next() {
		var status string

		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return "", err
		}

		fmt.Fprintf(&out, "review_delivery_total{status=%q} %d\n", status, count)
	}

	if err := rows.Err(); err != nil {
		return "", err
	}

	return out.String(), nil
}
