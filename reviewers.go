package main

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"review/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

const reviewerReviewLimit = 50

// handleReviewerProfile returns one reviewer's identity, permissions, stats and recent decisions.
func (a *reviewApp) handleReviewerProfile(w http.ResponseWriter, r *http.Request) {
	program, ok := a.programFor(w, r)
	if !ok {
		return
	}

	if _, ok := a.requireReviewer(w, r); !ok {
		return
	}

	key := strings.TrimSpace(r.PathValue("key"))
	if key == "" {
		app.WriteError(w, http.StatusNotFound, "unknown reviewer")
		return
	}

	profile, err := a.reviewerProfile(r.Context(), program.ID, key)
	if err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			app.WriteError(w, http.StatusNotFound, "unknown reviewer")
			return
		}

		writeStorage(w, err)

		return
	}

	app.WriteJSON(w, http.StatusOK, profile)
}

// reviewerProfile reads the roster entry, falling back to a historical review email for former reviewers.
func (a *reviewApp) reviewerProfile(ctx context.Context, programID, key string) (contract.ReviewerProfile, error) {
	profile := contract.ReviewerProfile{Email: key, Permissions: []string{}, Reviews: []contract.ReviewerReview{}, Tracks: []string{}}

	var (
		id, email, name, role, tracks, permissions, joined, lastLogin string
		active                                                        int
	)

	err := a.store.QueryRow(ctx, `select u.id, u.email, coalesce(u.name, ''), coalesce(m.role, ''),
			coalesce(m.active, 0), coalesce(m.tracks, '[]'), coalesce(m.permissions, '[]'),
			u.created_at, coalesce(u.last_login_at, '')
		from user u
		left join membership m on m.user_id = u.id and m.program_id = ?
		where u.id = ? or lower(u.email) = lower(?)
		limit 1`, programID, key, key).Scan(&id, &email, &name, &role, &active, &tracks, &permissions, &joined, &lastLogin)
	if err != nil && !errors.Is(err, storage.ErrNoRows) {
		return profile, err
	}

	if err == nil {
		profile.ID = optionalText(id)
		profile.Email = email
		profile.Name = optionalText(name)
		profile.JoinedAt = optionalText(joined)
		profile.LastLoginAt = optionalText(lastLogin)
		profile.Tracks = unmarshalStrings(tracks)
		profile.Permissions = unmarshalStrings(permissions)

		if role != "" {
			isActive := active == 1
			profile.Role = optionalText(role)
			profile.Active = &isActive
		}
	} else {
		// A prior reviewer may no longer have an account; their decisions still carry the email and name.
		var reviewName string

		if err := a.store.QueryRow(ctx, `select r.reviewer_email, coalesce(r.reviewer_name, '')
			from review r join submission s on s.id = r.submission_id
			where s.program_id = ? and lower(r.reviewer_email) = lower(?)
			limit 1`, programID, key).Scan(&email, &reviewName); err != nil {
			return profile, err
		}

		profile.Email = email
		profile.Name = optionalText(reviewName)
	}

	condition := "s.program_id = ?"
	args := []any{programID}

	if profile.ID != nil {
		condition += " and (r.reviewer_id = ? or (r.reviewer_id is null and lower(r.reviewer_email) = lower(?)))"
		args = append(args, *profile.ID, profile.Email)
	} else {
		condition += " and lower(r.reviewer_email) = lower(?)"
		args = append(args, profile.Email)
	}

	var (
		total, approved, changes, rejected, held, invalidated, firstPass, secondPass, hours int
		lastReview                                                                          string
	)

	if err := a.store.QueryRow(ctx, `select count(*),
			coalesce(sum(case when r.decision = 'approved' then 1 else 0 end), 0),
			coalesce(sum(case when r.decision = 'changes' then 1 else 0 end), 0),
			coalesce(sum(case when r.decision = 'rejected' then 1 else 0 end), 0),
			coalesce(sum(case when r.held = 1 then 1 else 0 end), 0),
			coalesce(sum(case when r.invalidated_at is not null then 1 else 0 end), 0),
			coalesce(sum(case when r.stage = 1 then 1 else 0 end), 0),
			coalesce(sum(case when r.stage = 2 then 1 else 0 end), 0),
			coalesce(sum(coalesce(r.hours_approved_seconds, 0)), 0),
			coalesce(max(r.created_at), '')
		from review r join submission s on s.id = r.submission_id
		where `+condition, args...).Scan(&total, &approved, &changes, &rejected, &held, &invalidated, &firstPass, &secondPass, &hours, &lastReview); err != nil {
		return profile, err
	}

	// A user with no membership in this programme and no decisions here is not a reviewer of it.
	if profile.Role == nil && total == 0 {
		return profile, storage.ErrNoRows
	}

	profile.ReviewTotal = total
	profile.Stats = contract.ReviewerStats{
		Approved:             approved,
		Changes:              changes,
		FirstPass:            firstPass,
		Held:                 held,
		HoursApprovedSeconds: hours,
		Invalidated:          invalidated,
		LastReviewAt:         optionalText(lastReview),
		Rejected:             rejected,
		SecondPass:           secondPass,
		Total:                total,
	}

	rows, err := a.store.Query(ctx, `select r.id, r.submission_id, coalesce(s.title, ''), r.decision, r.stage,
			r.held, coalesce(r.hours_approved_seconds, 0), coalesce(r.invalidated_at, ''), r.created_at
		from review r join submission s on s.id = r.submission_id
		where `+condition+`
		order by r.created_at desc, r.id desc limit ?`, append(args, reviewerReviewLimit)...)
	if err != nil {
		return profile, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			item            contract.ReviewerReview
			approvedSeconds int
			invalidatedAt   string
		)

		if err := rows.Scan(&item.ID, &item.SubmissionID, &item.SubmissionTitle, &item.Decision, &item.Stage,
			&item.Held, &approvedSeconds, &invalidatedAt, &item.CreatedAt); err != nil {
			return profile, err
		}

		if approvedSeconds > 0 {
			item.HoursApprovedSeconds = &approvedSeconds
		}

		item.InvalidatedAt = optionalText(invalidatedAt)
		profile.Reviews = append(profile.Reviews, item)
	}

	return profile, rows.Err()
}
