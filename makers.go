package main

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"ire/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

const makerProfileLimit = 200

// handleMakerProfile returns one maker's identity and their latest ship per project.
func (a *ireApp) handleMakerProfile(w http.ResponseWriter, r *http.Request) {
	program, ok := a.programFor(w, r)
	if !ok {
		return
	}

	if _, ok := a.requireReviewer(w, r); !ok {
		return
	}

	email := strings.ToLower(strings.TrimSpace(r.PathValue("email")))
	if email == "" {
		app.WriteError(w, http.StatusNotFound, "unknown maker")
		return
	}

	profile, err := a.makerProfile(r.Context(), program.ID, email)
	if err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			app.WriteError(w, http.StatusNotFound, "unknown maker")
			return
		}

		writeStorage(w, err)

		return
	}

	app.WriteJSON(w, http.StatusOK, profile)
}

// makerProfile reads the maker's latest identity and one row per project, newest first.
func (a *ireApp) makerProfile(ctx context.Context, programID, email string) (contract.MakerProfile, error) {
	profile := contract.MakerProfile{Submissions: []contract.MakerSubmission{}}

	var (
		name, slack string
	)

	err := a.store.QueryRow(ctx, `select maker_email, coalesce(maker_name, ''), coalesce(maker_slack_id, '')
		from submission
		where program_id = ? and lower(maker_email) = lower(?)
		order by updated_at desc, id desc limit 1`, programID, email).
		Scan(&profile.Email, &name, &slack)
	if err != nil {
		return profile, err
	}

	if name != "" {
		profile.Name = &name
	}

	if slack != "" {
		avatar := makerAvatarBase + "/" + url.PathEscape(slack) + "/r"
		profile.AvatarURL = &avatar
	}

	rows, err := a.store.Query(ctx, `select s.id, s.external_id, s.title, s.status, s.stage, s.version, s.updated_at,
			coalesce(s.decided_at, ''), coalesce(s.theme, ''), coalesce(s.thumbnail_url, ''),
			s.hours_claimed_seconds, coalesce(s.hours_approved_seconds, 0),
			coalesce((select r.decision from review r
				where r.submission_id = s.id and r.is_final = 1 and r.invalidated_at is null
				order by r.created_at desc, r.id desc limit 1), '')
		from submission s
		where s.program_id = ? and lower(s.maker_email) = lower(?)
		  and s.version = (select max(s2.version) from submission s2
			where s2.program_id = s.program_id and s2.external_id = s.external_id)
		order by s.updated_at desc, s.id asc limit ?`, programID, email, makerProfileLimit)
	if err != nil {
		return profile, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			item                        contract.MakerSubmission
			decidedAt, theme, thumbnail string
			decision                    string
			hoursApproved               int
		)

		if err := rows.Scan(&item.ID, &item.ExternalID, &item.Title, &item.Status, &item.Stage, &item.Version,
			&item.UpdatedAt, &decidedAt, &theme, &thumbnail, &item.HoursClaimedSeconds, &hoursApproved, &decision); err != nil {
			return profile, err
		}

		if decidedAt != "" {
			item.DecidedAt = &decidedAt
		}

		if theme != "" {
			item.Theme = &theme
		}

		if thumbnail != "" {
			item.ThumbnailURL = &thumbnail
		}

		if hoursApproved > 0 {
			item.HoursApprovedSeconds = &hoursApproved
		}

		// A missing decision is an undecided ship; only a recorded one is shown.
		if decision != "" {
			item.Decision = &decision
		}

		profile.Submissions = append(profile.Submissions, item)
	}

	return profile, rows.Err()
}
