package main

import (
	"context"
	"slices"

	"ire/contract"
)

// outboundStage is one stage in the outbound history.
type outboundStage struct {
	Stage    int    `json:"stage"`
	Decision string `json:"decision"`
	Reviewer string `json:"reviewer"`
	At       string `json:"at"`
}

// outboundMaker is the maker identity Half Life expects.
type outboundMaker struct {
	Email   string `json:"email"`
	Name    string `json:"name"`
	SlackID string `json:"slack_id"`
}

// outboundShip is the immutable ship snapshot in the event.
type outboundShip struct {
	Title             string   `json:"title"`
	Description       string   `json:"description"`
	Track             string   `json:"track"`
	RepoURL           string   `json:"repo_url"`
	DemoURL           string   `json:"demo_url"`
	ThumbnailURL      string   `json:"thumbnail_url"`
	HackatimeProjects []string `json:"hackatime_projects"`
}

// outboundBreakdown is Ari's seconds breakdown.
type outboundBreakdown struct {
	Hackatime int `json:"hackatime"`
	Journals  int `json:"journals"`
	Lapse     int `json:"lapse"`
	Program   int `json:"program"`
}

// outboundReviewer is the actor on a decision.
type outboundReviewer struct {
	Email   string `json:"email"`
	SlackID string `json:"slack_id"`
}

// outboundJustification matches the fields Half Life's mirror writes back to Airtable.
type outboundJustification struct {
	HackatimeProjects string `json:"hackatime_projects,omitempty"`
	TechnicalFeatures string `json:"technical_features,omitempty"`
	DeflationReason   string `json:"deflation_reason,omitempty"`
}

// outboundReview is the review object Half Life reads.
type outboundReview struct {
	ApprovedSeconds  int                   `json:"approved_seconds"`
	ApprovedMinutes  int                   `json:"approved_minutes"`
	ApprovedHours    float64               `json:"approved_hours"`
	SecondsBreakdown outboundBreakdown     `json:"seconds_breakdown"`
	NoteToMaker      string                `json:"note_to_maker"`
	AuditNote        string                `json:"audit_note"`
	OverrideReason   string                `json:"override_reason,omitempty"`
	Reviewer         outboundReviewer      `json:"reviewer"`
	ReviewerEmail    string                `json:"reviewerEmail"`
	Justification    outboundJustification `json:"justification"`
}

// outboundCheck is one check summary in the event.
type outboundCheck struct {
	Key     string `json:"key"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
}

// outboundChecks is the check block in the event.
type outboundChecks struct {
	Overall string          `json:"overall"`
	Results []outboundCheck `json:"results"`
}

// outboundEvent is the exact webhook body. created_at is additive so the recovery feed carries a cursor-able time.
type outboundEvent struct {
	Event        string          `json:"event"`
	ID           string          `json:"id"`
	ExternalID   string          `json:"external_id"`
	Decision     string          `json:"decision"`
	Stage        int             `json:"stage"`
	StageHistory []outboundStage `json:"stage_history"`
	Maker        outboundMaker   `json:"maker"`
	Ship         outboundShip    `json:"ship"`
	Review       outboundReview  `json:"review"`
	Checks       outboundChecks  `json:"checks"`
	CreatedAt    string          `json:"createdAt"`
}

// outboundEventName maps a decision to the Ari event name.
func outboundEventName(decision string) string {
	switch decision {
	case decisionApproved:
		return "review.approved"
	case decisionChanges:
		return "review.changes"
	default:
		return "review.rejected"
	}
}

// outboundPayload builds the final event. A held decision never reaches here because no outbox row is written.
func (a *ireApp) outboundPayload(ship submissionRow, person reviewer, checks []contract.CheckResult, decision string, hours int, note, internalNote, overrideReason, now string, history []outboundStage, hackatimeProjects []string) outboundEvent {
	results := make([]outboundCheck, 0, len(checks))
	for _, check := range checks {
		results = append(results, outboundCheck{Key: check.Key, Status: check.Status, Summary: check.Summary})
	}

	justification := outboundJustification{}
	if hours < ship.HoursClaimed {
		justification.DeflationReason = "Reviewed down to " + formatHours(hours) + " from " + formatHours(ship.HoursClaimed) + "."
	}

	return outboundEvent{
		Event:        outboundEventName(decision),
		ID:           ship.ID,
		ExternalID:   ship.ExternalID,
		Decision:     decision,
		Stage:        ship.Stage,
		StageHistory: history,
		Maker:        outboundMaker{Email: ship.MakerEmail, Name: ship.MakerName, SlackID: ship.MakerSlack},
		Ship: outboundShip{
			Title:             ship.Title,
			Description:       ship.Description,
			Track:             ship.Track,
			RepoURL:           ship.RepoURL,
			DemoURL:           ship.DemoURL,
			ThumbnailURL:      ship.ThumbnailURL,
			HackatimeProjects: hackatimeProjects,
		},
		Review: outboundReview{
			ApprovedSeconds: hours,
			ApprovedMinutes: hours / 60,
			ApprovedHours:   float64(hours) / secondsPerHour,
			SecondsBreakdown: outboundBreakdown{
				Hackatime: ship.HoursHackatim,
				Journals:  ship.HoursJournal,
			},
			NoteToMaker:    note,
			AuditNote:      internalNote,
			OverrideReason: overrideReason,
			Reviewer:       outboundReviewer{Email: person.Email},
			ReviewerEmail:  person.Email,
			Justification:  justification,
		},
		Checks:    outboundChecks{Overall: summaryOverall(ship.CheckSummary), Results: results},
		CreatedAt: now,
	}
}

// stageHistory builds the stage list including the decision being made.
func (a *ireApp) stageHistory(ctx context.Context, submissionID string, person reviewer, decision, now string, stage int) []outboundStage {
	var out []outboundStage

	reviews, err := a.loadReviews(ctx, submissionID)
	if err == nil {
		for _, review := range slices.Backward(reviews) {
			if review.InvalidatedAt != nil {
				continue
			}

			out = append(out, outboundStage{Stage: review.Stage, Decision: review.Decision, Reviewer: review.ReviewerEmail, At: review.CreatedAt})
		}
	}

	return append(out, outboundStage{Stage: stage, Decision: decision, Reviewer: person.Email, At: now})
}
