package main

import (
	"context"
	"net/http"
	"time"

	"ire/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
)

// openStatuses is the queue membership shared by the list and the overview readout.
const openStatuses = "'checking','ready','claimed','in_review','needs_attention','stage2_hold'"

// overviewWaitPercentile picks the nearest-rank value, so p50 of one ship is that ship.
func overviewWaitPercentile(sorted []int64, percent int) int64 {
	if len(sorted) == 0 {
		return 0
	}

	rank := (percent*len(sorted) + 99) / 100
	if rank < 1 {
		rank = 1
	}

	if rank > len(sorted) {
		rank = len(sorted)
	}

	return sorted[rank-1]
}

// overviewWaitAges loads every open ship oldest-first with its age in milliseconds, returning the ages youngest-first for percentile order.
func (a *ireApp) overviewWaitAges(ctx context.Context, programID string, now time.Time) ([]int64, string, string, error) {
	rows, err := a.store.Query(ctx, `select id, title, ingested_at from submission
		where program_id = ? and status in (`+openStatuses+`) order by ingested_at asc, id asc`, programID)
	if err != nil {
		return nil, "", "", err
	}
	defer rows.Close()

	var ages []int64

	var oldestID, oldestTitle string

	for rows.Next() {
		var id, title, ingested string

		if err := rows.Scan(&id, &title, &ingested); err != nil {
			return nil, "", "", err
		}

		at, err := time.Parse(timestampFormat, ingested)
		if err != nil {
			continue
		}

		ms := now.Sub(at).Milliseconds()
		if ms < 0 {
			ms = 0
		}

		if len(ages) == 0 {
			oldestID, oldestTitle = id, title
		}

		ages = append(ages, ms)
	}

	for i, j := 0, len(ages)-1; i < j; i, j = i+1, j-1 {
		ages[i], ages[j] = ages[j], ages[i]
	}

	return ages, oldestID, oldestTitle, rows.Err()
}

// handleOverview returns the programme health readout behind the overview page.
func (a *ireApp) handleOverview(w http.ResponseWriter, r *http.Request) {
	if _, ok := a.requireReviewer(w, r); !ok {
		return
	}

	program, ok := a.programFor(w, r)
	if !ok {
		return
	}

	now := a.now()
	today := now.Format("2006-01-02")
	monthAgo := a.later(-30 * 24 * time.Hour)
	twoWeeksAgo := a.later(-14 * 24 * time.Hour)

	ctx := r.Context()

	ages, oldestID, oldestTitle, err := a.overviewWaitAges(ctx, program.ID, now)
	if err != nil {
		writeStorage(w, err)

		return
	}

	response := contract.OverviewResponse{
		ByStatus:    []contract.OverviewStatus{},
		Daily:       []contract.OverviewDay{},
		Leaderboard: []contract.OverviewLeader{},
	}

	response.Wait.Count = len(ages)
	response.Wait.P50MS = int(overviewWaitPercentile(ages, 50))
	response.Wait.P95MS = int(overviewWaitPercentile(ages, 95))
	if len(ages) > 0 {
		response.Wait.MaxMS = int(ages[len(ages)-1])
		response.Wait.OldestID = optionalText(oldestID)
		response.Wait.OldestTitle = optionalText(oldestTitle)
	}

	statusRows, err := a.store.Query(ctx, `select status, count(*) from submission
		where program_id = ? and status in (`+openStatuses+`) group by status order by status`, program.ID)
	if err != nil {
		writeStorage(w, err)

		return
	}

	for statusRows.Next() {
		var row contract.OverviewStatus

		if err := statusRows.Scan(&row.Status, &row.Count); err != nil {
			statusRows.Close()
			writeStorage(w, err)

			return
		}

		response.ByStatus = append(response.ByStatus, row)
	}

	if err := statusRows.Err(); err != nil {
		statusRows.Close()
		writeStorage(w, err)

		return
	}

	statusRows.Close()

	if err := a.store.QueryRow(ctx, `select count(*) from submission
		where program_id = ? and status in (`+openStatuses+`)`, program.ID).Scan(&response.NeedsReview); err != nil {
		writeStorage(w, err)

		return
	}

	if err := a.store.QueryRow(ctx, `select count(*) from submission
		where program_id = ? and substr(ingested_at, 1, 10) >= ?`, program.ID, today).Scan(&response.NewToday); err != nil {
		writeStorage(w, err)

		return
	}

	if err := a.store.QueryRow(ctx, `select count(*) from review r join submission s on s.id = r.submission_id
		where s.program_id = ? and r.is_final = 1 and r.invalidated_at is null and substr(r.created_at, 1, 10) >= ?`,
		program.ID, today).Scan(&response.ReviewedToday); err != nil {
		writeStorage(w, err)

		return
	}

	leaderRows, err := a.store.Query(ctx, `select coalesce(max(r.reviewer_name), ''), r.reviewer_email, count(*)
		from review r join submission s on s.id = r.submission_id
		where s.program_id = ? and r.is_final = 1 and r.invalidated_at is null and r.created_at >= ?
		group by r.reviewer_email order by count(*) desc, r.reviewer_email asc limit 10`, program.ID, monthAgo)
	if err != nil {
		writeStorage(w, err)

		return
	}

	for leaderRows.Next() {
		var name, email string

		var count int

		if err := leaderRows.Scan(&name, &email, &count); err != nil {
			leaderRows.Close()
			writeStorage(w, err)

			return
		}

		response.Leaderboard = append(response.Leaderboard, contract.OverviewLeader{
			Count: count,
			Email: optionalText(email),
			Name:  optionalText(name),
		})
	}

	if err := leaderRows.Err(); err != nil {
		leaderRows.Close()
		writeStorage(w, err)

		return
	}

	leaderRows.Close()

	dayCounts := map[string]int{}

	dayRows, err := a.store.Query(ctx, `select substr(r.created_at, 1, 10), count(*)
		from review r join submission s on s.id = r.submission_id
		where s.program_id = ? and r.is_final = 1 and r.invalidated_at is null and r.created_at >= ?
		group by substr(r.created_at, 1, 10) order by substr(r.created_at, 1, 10)`, program.ID, twoWeeksAgo)
	if err != nil {
		writeStorage(w, err)

		return
	}

	for dayRows.Next() {
		var day string

		var count int

		if err := dayRows.Scan(&day, &count); err != nil {
			dayRows.Close()
			writeStorage(w, err)

			return
		}

		dayCounts[day] = count
	}

	if err := dayRows.Err(); err != nil {
		dayRows.Close()
		writeStorage(w, err)

		return
	}

	dayRows.Close()

	for i := 13; i >= 0; i-- {
		day := now.AddDate(0, 0, -i).Format("2006-01-02")
		response.Daily = append(response.Daily, contract.OverviewDay{Date: day, Count: dayCounts[day]})
	}

	app.WriteJSON(w, http.StatusOK, response)
}
