package main

import (
	"context"
	"net/http"
	"time"

	"ire/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
)

const (
	// openStatuses is the queue membership shared by the list and the overview readout.
	openStatuses               = "'checking','ready','claimed','in_review','needs_attention','stage2_hold'"
	overviewPercentileScale    = 100
	overviewP50                = 50
	overviewP95                = 95
	overviewLeaderLimit        = 10
	overviewDailyDays          = 14
	overviewRecentMonthDays    = 30
	overviewRecentTwoWeeksDays = 14
)

// overviewTotals holds the programme-wide counters from the overview header.
type overviewTotals struct {
	needsReview, newToday, reviewedToday int
}

// overviewWaitPercentile picks the nearest-rank value, so p50 of one ship is that ship.
func overviewWaitPercentile(sorted []int64, percent int) int64 {
	if len(sorted) == 0 {
		return 0
	}

	rank := (percent*len(sorted) + overviewPercentileScale - 1) / overviewPercentileScale
	rank = max(rank, 1)
	rank = min(rank, len(sorted))

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

		ms := max(now.Sub(at).Milliseconds(), 0)

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

// overviewWait builds the oldest-first wait summary, including the oldest open ship.
func (a *ireApp) overviewWait(ctx context.Context, programID string, now time.Time) (contract.OverviewWait, error) {
	ages, oldestID, oldestTitle, err := a.overviewWaitAges(ctx, programID, now)
	if err != nil {
		return contract.OverviewWait{}, err
	}

	wait := contract.OverviewWait{
		Count: len(ages),
		P50MS: int(overviewWaitPercentile(ages, overviewP50)),
		P95MS: int(overviewWaitPercentile(ages, overviewP95)),
	}

	if len(ages) > 0 {
		wait.MaxMS = int(ages[len(ages)-1])
		wait.OldestID = optionalText(oldestID)
		wait.OldestTitle = optionalText(oldestTitle)
	}

	return wait, nil
}

// overviewStatuses counts the open queue by status.
func (a *ireApp) overviewStatuses(ctx context.Context, programID string) ([]contract.OverviewStatus, error) {
	rows, err := a.store.Query(ctx, `select status, count(*) from submission
		where program_id = ? and status in (`+openStatuses+`) group by status order by status`, programID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	statuses := []contract.OverviewStatus{}

	for rows.Next() {
		var row contract.OverviewStatus

		if err := rows.Scan(&row.Status, &row.Count); err != nil {
			return nil, err
		}

		statuses = append(statuses, row)
	}

	return statuses, rows.Err()
}

// overviewCounts loads the open, new-today and reviewed-today totals.
func (a *ireApp) overviewCounts(ctx context.Context, programID, today string) (overviewTotals, error) {
	var totals overviewTotals

	if err := a.store.QueryRow(ctx, `select count(*) from submission
		where program_id = ? and status in (`+openStatuses+`)`, programID).Scan(&totals.needsReview); err != nil {
		return totals, err
	}

	if err := a.store.QueryRow(ctx, `select count(*) from submission
		where program_id = ? and substr(ingested_at, 1, 10) >= ?`, programID, today).Scan(&totals.newToday); err != nil {
		return totals, err
	}

	if err := a.store.QueryRow(ctx, `select count(*) from review r join submission s on s.id = r.submission_id
		where s.program_id = ? and r.is_final = 1 and r.invalidated_at is null and substr(r.created_at, 1, 10) >= ?`,
		programID, today).Scan(&totals.reviewedToday); err != nil {
		return totals, err
	}

	return totals, nil
}

// overviewLeaderboard ranks reviewers by final decisions in the recent month.
func (a *ireApp) overviewLeaderboard(ctx context.Context, programID, monthAgo string) ([]contract.OverviewLeader, error) {
	rows, err := a.store.Query(ctx, `select coalesce(max(r.reviewer_name), ''), r.reviewer_email, count(*)
		from review r join submission s on s.id = r.submission_id
		where s.program_id = ? and r.is_final = 1 and r.invalidated_at is null and r.created_at >= ?
		group by r.reviewer_email order by count(*) desc, r.reviewer_email asc limit ?`, programID, monthAgo, overviewLeaderLimit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	leaders := []contract.OverviewLeader{}

	for rows.Next() {
		var name, email string

		var count int

		if err := rows.Scan(&name, &email, &count); err != nil {
			return nil, err
		}

		leaders = append(leaders, contract.OverviewLeader{
			Count: count,
			Email: optionalText(email),
			Name:  optionalText(name),
		})
	}

	return leaders, rows.Err()
}

// overviewDaily fills the last two weeks of completed reviews, including empty days.
func (a *ireApp) overviewDaily(ctx context.Context, programID, twoWeeksAgo string, now time.Time) ([]contract.OverviewDay, error) {
	rows, err := a.store.Query(ctx, `select substr(r.created_at, 1, 10), count(*)
		from review r join submission s on s.id = r.submission_id
		where s.program_id = ? and r.is_final = 1 and r.invalidated_at is null and r.created_at >= ?
		group by substr(r.created_at, 1, 10) order by substr(r.created_at, 1, 10)`, programID, twoWeeksAgo)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	dayCounts := map[string]int{}

	for rows.Next() {
		var day string

		var count int

		if err := rows.Scan(&day, &count); err != nil {
			return nil, err
		}

		dayCounts[day] = count
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	days := []contract.OverviewDay{}

	for i := range overviewDailyDays {
		day := now.AddDate(0, 0, i-overviewDailyDays+1).Format("2006-01-02")
		days = append(days, contract.OverviewDay{Date: day, Count: dayCounts[day]})
	}

	return days, nil
}

// overviewResponse gathers the programme health readout behind the overview page.
func (a *ireApp) overviewResponse(ctx context.Context, programID string) (contract.OverviewResponse, error) {
	now := a.now()
	today := now.Format("2006-01-02")
	monthAgo := a.later(-overviewRecentMonthDays * 24 * time.Hour)
	twoWeeksAgo := a.later(-overviewRecentTwoWeeksDays * 24 * time.Hour)

	response := contract.OverviewResponse{
		ByStatus:    []contract.OverviewStatus{},
		Daily:       []contract.OverviewDay{},
		Leaderboard: []contract.OverviewLeader{},
	}

	wait, err := a.overviewWait(ctx, programID, now)
	if err != nil {
		return response, err
	}

	statuses, err := a.overviewStatuses(ctx, programID)
	if err != nil {
		return response, err
	}

	totals, err := a.overviewCounts(ctx, programID, today)
	if err != nil {
		return response, err
	}

	leaders, err := a.overviewLeaderboard(ctx, programID, monthAgo)
	if err != nil {
		return response, err
	}

	daily, err := a.overviewDaily(ctx, programID, twoWeeksAgo, now)
	if err != nil {
		return response, err
	}

	response.Wait = wait
	response.ByStatus = statuses
	response.NeedsReview = totals.needsReview
	response.NewToday = totals.newToday
	response.ReviewedToday = totals.reviewedToday
	response.Leaderboard = leaders
	response.Daily = daily

	return response, nil
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

	response, err := a.overviewResponse(r.Context(), program.ID)
	if err != nil {
		writeStorage(w, err)

		return
	}

	app.WriteJSON(w, http.StatusOK, response)
}
