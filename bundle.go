package main

import (
	"context"

	"ire/contract"
)

// reviewBundle assembles the seven indexed reads behind the review screen.
//
//nolint:funlen // The payload is one mapping of the ship and its evidence.
func (a *ireApp) reviewBundle(ctx context.Context, ship submissionRow, person reviewer) (contract.ReviewBundle, error) {
	checks, err := a.latestChecks(ctx, ship.ID, ship.Version)
	if err != nil {
		return contract.ReviewBundle{}, err
	}

	journals, err := a.loadJournals(ctx, ship.ID)
	if err != nil {
		return contract.ReviewBundle{}, err
	}

	notes, err := a.loadNotes(ctx, ship)
	if err != nil {
		return contract.ReviewBundle{}, err
	}

	bom, err := a.loadBom(ctx, ship.ID)
	if err != nil {
		return contract.ReviewBundle{}, err
	}

	reviews, err := a.loadReviews(ctx, ship.ID)
	if err != nil {
		return contract.ReviewBundle{}, err
	}

	findings, err := a.loadFindings(ctx, ship.ProgramID, ship.ExternalID)
	if err != nil {
		return contract.ReviewBundle{}, err
	}

	stationDefs, stationAnswers, stationStates, currentStation, err := a.stationProgress(ctx, ship, findings)
	if err != nil {
		return contract.ReviewBundle{}, err
	}

	events, err := a.loadEvents(ctx, ship.ID)
	if err != nil {
		return contract.ReviewBundle{}, err
	}

	snapshot, hasEvidence, err := a.loadEvidence(ctx, ship.ID, ship.Version)
	if err != nil {
		return contract.ReviewBundle{}, err
	}

	run, hasRun, err := a.loadCheckRun(ctx, ship.ID, ship.Version)
	if err != nil {
		return contract.ReviewBundle{}, err
	}

	var checkRun *contract.CheckRun
	if hasRun {
		checkRun = &run
	}

	var evidence *contract.Evidence
	if hasEvidence {
		evidence = &snapshot
	}

	policy := a.policyFor(ctx, ship.ProgramID)
	claim := a.claimFor(ctx, ship.ID, person)

	submission := contract.Submission{
		ID:                    ship.ID,
		ExternalID:            ship.ExternalID,
		Version:               ship.Version,
		Status:                ship.Status,
		Stage:                 ship.Stage,
		Title:                 ship.Title,
		Track:                 ship.Track,
		HoursClaimedSeconds:   ship.HoursClaimed,
		HoursJournalSeconds:   ship.HoursJournal,
		HoursHackatimeSeconds: ship.HoursHackatim,
		MakerEmail:            ship.MakerEmail,
		IngestedAt:            ship.IngestedAt,
		UpdatedAt:             ship.UpdatedAt,
	}

	if ship.MakerSlack != "" {
		submission.MakerSlackID = &ship.MakerSlack
	}

	if ship.Description != "" {
		submission.Description = &ship.Description
	}

	if ship.Theme != "" {
		submission.Theme = &ship.Theme
	}

	if ship.Phase != "" {
		submission.Phase = &ship.Phase
	}

	if ship.Week > 0 {
		submission.Week = &ship.Week
	}

	if ship.MakerName != "" {
		submission.MakerName = &ship.MakerName
	}

	if ship.RepoURL != "" {
		submission.RepoURL = &ship.RepoURL
	}

	if ship.DemoURL != "" {
		submission.DemoURL = &ship.DemoURL
	}

	if ship.ThumbnailURL != "" {
		submission.ThumbnailURL = &ship.ThumbnailURL
	}

	if ship.RequestedTier > 0 {
		submission.RequestedTier = &ship.RequestedTier
	}

	if ship.GrantCapCents > 0 {
		submission.GrantCapCents = &ship.GrantCapCents
	}

	if ship.BomTotalCents > 0 {
		submission.BomTotalCents = &ship.BomTotalCents
	}

	if ship.HoursApproved > 0 {
		submission.HoursApprovedSeconds = &ship.HoursApproved
	}

	if ship.HeldReason != "" {
		submission.HeldReason = &ship.HeldReason
	}

	if ship.DecidedAt != "" {
		submission.DecidedAt = &ship.DecidedAt
	}

	resubmission, err := a.resubmissionDiff(ctx, ship, evidence, journals)
	if err != nil {
		return contract.ReviewBundle{}, err
	}

	bundle := contract.ReviewBundle{
		Submission:       submission,
		Checks:           checks,
		Findings:         findings,
		Journals:         journals,
		Bom:              bom,
		Notes:            notes,
		Reviews:          reviews,
		Events:           events,
		Evidence:         evidence,
		CheckRun:         checkRun,
		PriorReviews:     payloadPriorReviews(ctx, a.store, ship.ID, ship.Version),
		Claim:            claim,
		Draft:            a.draftFor(ctx, ship.ID, person.ID),
		Acknowledgements: a.acknowledgementsFor(ctx, ship.ID, ship.Version, person.ID),
		Policy:           contractPolicy(policy),
		Resubmission:     resubmission,
		CurrentStation:   currentStation,
		Stations:         stationDefs,
		StationAnswers:   stationAnswers,
		StationStates:    stationStates,
	}

	return bundle, nil
}
