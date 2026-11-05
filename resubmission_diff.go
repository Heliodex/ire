package main

import (
	"context"
	"errors"
	"slices"
	"strings"

	"review/contract"

	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// resubmissionDiff compares a ship with the version it replaced: the previous decision, new commits, file changes, journal entries and BOM changes.
//
//nolint:nilnil // A first version has no diff and is not an error.
func (a *reviewApp) resubmissionDiff(ctx context.Context, ship submissionRow, current *contract.Evidence, journals []contract.JournalEntry) (*contract.Resubmission, error) {
	if ship.PreviousID == "" {
		return nil, nil
	}

	previous, err := a.loadSubmission(ctx, ship.PreviousID)
	if err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			return nil, nil
		}

		return nil, err
	}

	diff := &contract.Resubmission{
		PreviousVersion: previous.Version,
		Commits:         []contract.EvidenceCommit{},
		AddedFiles:      []string{},
		RemovedFiles:    []string{},
		NewEntries:      []contract.ResubmissionEntry{},
		ChangedEntries:  []contract.ResubmissionEntry{},
		BomAdded:        []string{},
		BomRemoved:      []string{},
		BomPriceChanges: []contract.ResubmissionPrice{},
	}

	previousEvidence, hasPreviousEvidence, err := a.loadEvidence(ctx, previous.ID, previous.Version)
	if err != nil {
		return nil, err
	}

	if current != nil {
		diff.Commits = current.Commits

		if hasPreviousEvidence {
			diff.AddedFiles, diff.RemovedFiles = changedPaths(previousEvidence, *current)
		}
	}

	previousJournals, err := a.loadJournals(ctx, previous.ID)
	if err != nil {
		return nil, err
	}

	diff.NewEntries, diff.ChangedEntries = changedJournals(previousJournals, journals)

	previousBom, err := a.loadBom(ctx, previous.ID)
	if err != nil {
		return nil, err
	}

	currentBom, err := a.loadBom(ctx, ship.ID)
	if err != nil {
		return nil, err
	}

	diff.BomAdded, diff.BomRemoved, diff.BomPriceChanges = changedBom(previousBom, currentBom)

	a.fillPreviousReview(ctx, previous.ID, diff)

	return diff, nil
}

// changedPaths lists the files added and removed since the previous snapshot.
func changedPaths(previous, current contract.Evidence) (added, removed []string) {
	before := map[string]bool{}
	for _, file := range previous.Files {
		before[file.Path] = true
	}

	after := map[string]bool{}
	for _, file := range current.Files {
		after[file.Path] = true

		if !before[file.Path] {
			added = append(added, file.Path)
		}
	}

	for _, file := range previous.Files {
		if !after[file.Path] {
			removed = append(removed, file.Path)
		}
	}

	slices.Sort(added)
	slices.Sort(removed)

	return added, removed
}

// changedJournals lists entries that are new or whose time or text changed since the previous version.
func changedJournals(previous, current []contract.JournalEntry) (added, changed []contract.ResubmissionEntry) {
	before := make(map[string]contract.JournalEntry, len(previous))
	for _, entry := range previous {
		before[journalKey(entry)] = entry
	}

	for _, entry := range current {
		summary := contract.ResubmissionEntry{At: entry.At, Seconds: entry.Seconds, Title: entry.Title}

		match, ok := before[journalKey(entry)]
		if !ok {
			added = append(added, summary)
			continue
		}

		if match.Seconds != entry.Seconds || stringValue(match.Text) != stringValue(entry.Text) {
			changed = append(changed, summary)
		}
	}

	return added, changed
}

// journalKey identifies an entry across versions by its date and title.
func journalKey(entry contract.JournalEntry) string {
	return entry.At + "\x00" + stringValue(entry.Title)
}

// changedBom compares parts by name, reporting additions, removals and price changes.
func changedBom(previous, current []contract.BomLine) (added, removed []string, prices []contract.ResubmissionPrice) {
	before := make(map[string]int, len(previous))
	for _, line := range previous {
		before[line.Name] = line.UnitCostCents
	}

	seen := make(map[string]bool, len(current))
	for _, line := range current {
		seen[line.Name] = true

		cents, ok := before[line.Name]
		if !ok {
			added = append(added, line.Name)
			continue
		}

		if cents != line.UnitCostCents {
			prices = append(prices, contract.ResubmissionPrice{Name: line.Name, BeforeCents: cents, AfterCents: line.UnitCostCents})
		}
	}

	for _, line := range previous {
		if !seen[line.Name] {
			removed = append(removed, line.Name)
		}
	}

	slices.Sort(added)
	slices.Sort(removed)
	slices.SortFunc(prices, func(x, y contract.ResubmissionPrice) int {
		return strings.Compare(x.Name, y.Name)
	})

	return added, removed, prices
}

// fillPreviousReview adds the last final decision on the previous version.
func (a *reviewApp) fillPreviousReview(ctx context.Context, previousID string, diff *contract.Resubmission) {
	var reviewerEmail, decision, note, createdAt string

	err := a.store.QueryRow(ctx, `select reviewer_email, decision, note_to_maker, created_at
		from review where submission_id = ? and is_final = 1 and invalidated_at is null
		order by created_at desc, id desc limit 1`, previousID).
		Scan(&reviewerEmail, &decision, &note, &createdAt)
	if err != nil {
		return
	}

	diff.PreviousDecision = optionalText(decision)
	diff.PreviousNote = optionalText(note)
	diff.PreviousReviewedAt = optionalText(createdAt)
	diff.PreviousReviewer = optionalText(reviewerEmail)
}
