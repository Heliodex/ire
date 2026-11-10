# 10 – Implementation plan

- **Status:** build plan for discussion
- **Date:** 2026-11-09
- **Scope:** the concrete changes that turn [09-review-pipeline.md](./09-review-pipeline.md) into shippable increments
- **Related:** [03-data-model.md](./03-data-model.md), [05-ui.md](./05-ui.md), [06-integration.md](./06-integration.md), [decisions.md](./decisions.md)

## The short version

Do not build the board first. Build the **record** first: the two missing fields, per-entry time reasons, a findings ledger, a return composer and fix verification. That is a deployable milestone that fixes the fines and the 12-return loop without changing the queue.

Then split the queue into three stations: triage, evidence and synthesis. The full pipeline, risk routing and AI pre-answers come after the data exists.

Three constraints shape the plan:

- **The platform stays in the path.** Ire does not write Unified directly. Unified holds PII for tens of thousands of people, and a well-scoped API key is still a risk. Ire sends the record to Half Life, Half Life writes Airtable, and the final Unified submission stays manual for now. The gate's job is to make that manual step a send, not a re-check.
- **Reviewers are generalists.** Every station must be open to every reviewer. Cell mode is the default, and station queues are a way to batch similar work when the volume justifies it. A shipwright role is a future option, not a prerequisite.
- **The current stack stays.** Go Wasm component, per-app SQLite, Valibot contract, static SvelteKit. This is an extension of the existing lifecycle, not a rewrite. Stages 1 and 2 remain the external vocabulary; stations are an internal dimension.

## What the Starbie case actually shows

The 12-return project is **Starbie by Vihaan Negi** (`github.com/shogunxfortnite-cyber/Starbie`). Ari has 14 versions of it. It was returned for changes 12 times, with a 13th change decision when the V8 approval was re-opened, and approved on version 14.

| Version | Decision | What the reviewer asked for |
| --- | --- | --- |
| 1 | changes | README overview, why, BOM table; `.kicad_pcb`; case; repo folders; commit messages; BOM taxes |
| 2 | changes | Move the ESP/XIAO to the board edge |
| 3 | changes | README description, setup and firmware; commit titles; firmware files; ESP edge called "fine atp" |
| 4 | changes | BOM price and quantity in the README; more README detail |
| 5 | changes | BOM in tabular form |
| 6 | changes | Gerber ZIP; BOM URLs; library names; repo folders; journal detail |
| 7 | changes | Gerber ZIP still missing |
| 8 | approved, then changes | Approved on 19 Oct, re-opened on 24 Oct for README detail, journals and repo folders |
| 9 | changes | XIAO edge; README what, why, how and photos; journals; BOM total |
| 10 | changes | README still unclear; outdated images; ordering; uniqueness |
| 11 | changes | README examples, structure and capitalization |
| 12 | changes | README appears AI-written |
| 13 | changes | README still AI; PCB order link; journals |
| 14 | approved | 8h12m approved from 10h42m, with a technical-features and deflation reason |

Counted by topic, the same findings came back again and again:

| Topic | Versions where it appeared | Count |
| --- | --- | --- |
| README quality or AI-written | 1, 3, 4, 8, 9, 10, 11, 12, 13 | 9 |
| BOM table, fields, links or total | 1, 4, 5, 6, 9, 13 | 6 |
| Journal detail | 6, 8, 9, 13 | 4 |
| ESP/XIAO on the board edge | 2, 3, 9 | 3 |
| Repository folders | 1, 6, 8 | 3 |
| Gerber ZIP | 6, 7 | 2 |
| Commit messages | 1, 3 | 2 |
| README AI-generated | 12, 13 | 2 |

What this proves:

1. **No reviewer could see the prior findings.** Nine of the twelve returns asked for README work. Six asked for BOM work. The same problem was reopened because nothing carried it forward.
2. **The advice contradicted itself.** V3 said the ESP edge was "fine atp"; V9 returned the project for the same ESP edge. A ledger with status would have shown that it was either fixed or never accepted.
3. **Fixes were not verified.** V6 asked for the Gerber ZIP, V7 found it missing, and it took another cycle. The reviewer had to rediscover an old finding instead of checking a named one.
4. **An approval did not stick.** V8 was approved on 19 October and re-opened for changes on 24 October. Whatever the cause, the decision record was not the end of the project's story.
5. **The cost was in the loop, not the final review.** Fourteen versions and twelve returns for a week 1 project that was eventually approved at 8h12m. A findings ledger and fix verification would have made this two or three return batches.

Use this case as the first regression fixture, with the names and free text replaced. The test should assert that a fixed finding is never returned again, that the return note lists every open finding in the envelope, and that the no-change version is not reviewed.

## What the fines actually demand

The three fines in `FINES.md` point at specific holes, not a general "review harder":

| Case | What went wrong | What the tool must enforce |
| --- | --- | --- |
| 31.5h, fined | No justification for the deflation; the note to the maker was used instead; the reviewer defended 33h but submitted 31.5h; no benchmark for why the time is reasonable | A deflation reason is required whenever approved < captured; it is internal, separate from the maker note, and answers "why is this amount reasonable for this maker?" with a benchmark |
| 10h, fined | Deflation justification was a note to the user; the maker's invalid last name had no explanatory note | Field separation is enforced; the record includes a profile-validity question and a note when the profile is not usable |
| 9h, needs changes | Most time was watching YouTube tutorials without evidence of application | The time station has a tutorial-consumption reason and asks for the specific tutorial and where it was used; time is cut when the application is not shown |

The fine model is $12 per submitted hour if the project is rejected entirely and $3.50 per submitted hour if changes are requested. The payout rate is $8.50 per submitted hour, so a rejection also loses the payout, and some changes fines can be reclaimed.

| Hours | Rejected fine | Changes fine | Payout at stake |
| --- | --- | --- | --- |
| 9 | $108 | $31.50 | $76.50 |
| 10 | $120 | $35 | $85 |
| 20 | $240 | $70 | $170 |
| 30 | $360 | $105 | $255 |
| 31.5 | $378 | $110.25 | $267.75 |

This is why review effort should follow hours and risk, not be equal per project. It is also why the record must be defensible: a missing sentence can cost more than the review saves.

## Milestone 0 – Make the record defensible

This is the first shippable milestone. No stations, no board. It fixes the fines and the repeated-return loop in the existing review page.

### 0.1 The fields end to end

The outbound contract already has `justification.technical_features` and `justification.deflation_reason`, but the app never fills them; `outbound_event.go` generates "Reviewed down to X from Y" and leaves technical features empty. Fix that path first.

- `web/src/lib/contract.ts`
  - Add `technicalFeatures` and `deflationReason` to `decisionRequestSchema` and `amendDecisionRequestSchema`, both `maxNote`.
  - Regenerate with `wt contract --dir .`.
- `schema.go` (version 13)
  - Add `technical_features text not null default ''` and `deflation_reason text not null default ''` to `review`.
  - Add `deflation_reason text` to `journal_entry`.
  - Add `time_adjustment` and `artifact_ref` tables (below).
- `decision.go`
  - Trim and length-check both fields.
  - When `decision == approved` or `hours < ship.HoursClaimed`, require `deflationReason` (policy flag, default on).
  - When `decision == approved` and the programme requires it, require `technicalFeatures`.
  - Store both on the review row.
- `outbound_event.go`
  - Pass both fields into `outboundJustification`; do not generate the deflation sentence from the numbers alone.
  - Keep the generated fallback only for legacy decisions made before the pipeline is enabled.
- `amendDecision` carries the same fields and validation.
- UI: add the fields to `decisionForm` with explicit "Internal" labels, a counter and the prompt "Why is this amount reasonable for this maker's skill level?"; keep the note to the maker separate.

**Acceptance.** An approval with `hours < claimed` and no deflation reason returns `422 deflation_reason_required`. The outbound event contains exactly what the reviewer wrote in both fields. The maker note and the deflation reason are not interchangeable.

### 0.2 Per-entry time and a generated ledger

- Extend `journalReview` (Go) and `journalReviewRequestSchema` (contract) with `deflationReason` and use the existing `reviewerNote` as the explanation.
- `JournalTime.svelte` gets a reason select: tutorial consumption without application, starter build, idle gap, duplicate with Hackatime, non-project work, unverifiable, unreasonably long, other. Choosing a reason when the approved amount is below the entry's seconds is required before the entry saves.
- Add `time_adjustment` for project-level cuts or bonuses: `id`, `submission_id`, `kind` (bonus or cut), `seconds`, `reason`, `note`, `reviewer_id`, `created_at`.
- The rail shows `captured`, `journals`, `hackatime`, `adjustments`, `approved` and the delta. The deflation reason is generated from the per-entry reasons plus adjustments, then edited by the reviewer. The server rejects an approval where `hoursApprovedSeconds` does not equal the ledger sum unless an adjustment with a reason exists.
- The benchmark prompt is part of the deflation reason template: "An intermediate maker would take about X for this; the claim is reasonable because Y."

**Acceptance.** Every cut has a reason; the approved total reconciles to the ledger; the generated sentence includes the benchmark; the FINES case 1 shape is impossible to submit.

### 0.3 The findings ledger

New table, keyed by the stable project identity, not the version:

```text
finding
  id                  text primary key
  program_id          text not null
  external_id         text not null          -- stable across versions
  opened_submission_id text not null
  opened_version      integer not null
  station             text not null          -- triage, time, narrative, hardware, synthesis
  question            text not null
  severity            text not null          -- block, fixable, note, hold
  status              text not null          -- open, fixed, accepted, disputed, withdrawn
  summary             text not null          -- one line the maker can act on
  detail              text                   -- internal explanation
  maker_action        text                   -- what the maker must do; empty for note-only
  evidence            text not null default '{}'
  opened_by           text
  resolved_by         text
  resolved_submission_id text
  resolved_version    integer
  resolution          text
  created_at          text not null
  updated_at          text not null
```

Add `finding_return` so a return is auditable and counts are possible:

```text
finding_return
  review_id, finding_id, submission_id, version, created_at
  primary key (review_id, finding_id)
```

Index `finding(program_id, external_id, status)` and `finding(submission_id, status)` on the opened submission. A new version gets a new `submission` row with the same `external_id` and `previous_submission_id`; the findings follow the external id.

API:

- `GET /api/v1/programs/{program}/submissions/{id}/findings` (also embedded in the review bundle).
- `POST /api/v1/programs/{program}/submissions/{id}/findings`.
- `PATCH /api/v1/programs/{program}/findings/{findingId}` for status, resolution and evidence.
- `POST /api/v1/programs/{program}/submissions/{id}/findings/{findingId}/verify` on a new version (marks fixed or still open with a note).
- `POST .../decision` gains `findingIds: []string` for a changes decision. The server validates that every id is open, belongs to the same `external_id`, and is included in the return envelope.

### 0.4 The return composer

- A new `ReturnComposer.svelte` lists every open finding in the current envelope, grouped by severity and maker action, pre-checked.
- The generated maker note is a list, not a paragraph: each finding's `maker_action` becomes a bullet; the reviewer can edit.
- Removing a finding requires a reason and is audited.
- If a required station in the envelope has not run, show "N stations not run; they may contain more findings" and require a reason to return anyway.
- The `changes` decision requires at least one finding once the pipeline is enabled. A free-text note is still allowed, but it is a note, not the return list.
- The next version's fix verification marks findings fixed, still open or disputed; the return note shows "3 of 5 fixed" and only the remaining findings can be returned again.

**Acceptance.** The Starbie fixture returns in two batches, not twelve. A finding marked fixed cannot be returned again. The generated note is the complete list.

### 0.5 Fix verification and no-change detection

- On ingest of a new version, compare the previous `submission_version.payload_sha256`; if identical, create a "no changes detected" banner and a one-click return with the open findings. Do not consume a reviewer.
- Compute artifact hashes on ingest: README blob, cover URL and content hash, each journal entry's text and seconds, the commit range, the design-file list. Store them in `artifact_ref`: `submission_id`, `kind`, `key`, `hash`, `url`, `created_at`, primary key `(submission_id, kind, key)`.
- Copy or reference station answers for unchanged artifacts; a changed README reopens only the README questions.
- The fix verification station is a queue item for the new version; it checks findings against the diff, not the whole project.
- After three returns, a stuck query surfaces the case to an organiser; after five, policy decides.

**Acceptance.** Resubmitting an unchanged project costs zero review minutes. A fixed finding is closed with a resolution and a version. A changed README does not reopen the hardware or time answers.

### 0.6 Instrument the baseline

Before the station UI, record events in `submission_event` or a small `review_event`:

- time to first answer, time to decision, time per tab;
- evidence re-opens and which artifact;
- return count, findings per return, findings fixed per version;
- deflation amount and reason code;
- fine outcomes imported manually from `FINES.md` for the pilot.

The point is to prove the ledger improves returns per project and findings per return without increasing cycle time.

## Milestone 1 – Three stations

### 1.1 Station policy as data

Add:

```text
station_definition
  program_id, key, version, label, description, sort, themes, active, required
station_question
  program_id, station_key, key, prompt, evidence_kind, required, sort
station_answer
  id, submission_id, station_key, question_key, answer, evidence,
  artifact_hash, source, ai_model, ai_confidence, reviewer_id, created_at
  unique (submission_id, station_key, question_key)
```

Seed the initial stations for Half Life:

| Key | Questions | Applies to |
| --- | --- | --- |
| `triage` | Cover is a screenshot; description describes the project; repo link is the right project; required images; profile usable | All |
| `time` | Entry is descriptive; time is productive; cut reason; benchmark | All |
| `narrative` | README explains the project and problems; commits are the maker's and filtered; claims match files; technical features; AI-likeness flag | All |
| `hardware` | Design is plausible; BOM matches; firmware matches the claim; note-only issues | PCB, CAD, SYNTH, DISPLAYS |
| `synthesis` | Open findings resolved; hours; note to maker; internal note; decision | All |
| `verify` | Record complete; sample of evidence; confirm, override or send back | Held approvals |

The station of a submission is derived: the earliest required station with an unanswered required question or an open returnable finding. Do not store a mutable `station` field on `submission`.

### 1.2 The queue

- `GET /api/v1/programs/{program}/stations` returns definitions, counts and the caller's permissions.
- `GET /api/v1/programs/{program}/stations/{key}/queue` returns items with project header, risk flags, age and prior answers. The existing queue filters stay on the overview.
- `/queue` shows station tabs and counts plus the existing list. `/stations/[key]` is the focused queue.
- Auto-advance when the last required question is answered. A card moves itself; drag is only for reassigning or escalating.
- The review page gets a **Review sheet** rail: stations, answers, findings and progress. Cell mode runs S1 to S5 in sequence with the same components.

### 1.3 Permissions

- Add station permissions: `station.triage`, `station.time`, `station.narrative`, `station.hardware`, `station.synthesis`, `station.verify`, `station.audit`.
- Seed the reviewer role with all work stations, the organiser role with synthesis and verify. The station check is per action, not per page.
- A future `shipwright` role can hold only `station.triage` and receive a bundle without maker email or other PII. Do not build the role until Half Life asks for it; design the check so it is possible.

**Acceptance.** A generalist can take a project from triage to decision in cell mode without leaving the page. A station queue is optional. Triage takes under 60 seconds, and the synthesis screen is the current review page plus the sheet.

## Milestone 2 – Split, pre-answer, route

- Split the evidence station into `time`, `narrative` and `hardware` queues. They can run in parallel when staffing allows because they read different artifacts; item-level claims prevent collisions.
- Add AI pre-answers: cover-is-a-screenshot, description-is-project-specific, starter-project diff, README extraction, duplicate text, tutorial links. Store `source`, `ai_model` and `ai_confidence`; a human confirms before it counts.
- Add simple risk routing: theme prerequisites, hours thresholds (for example 10h and 20h), flags, prior returns. High-risk gets more stations and 100% audit; low-risk gets sampling. Do not build a score before the data exists.
- Add the `audit_sample` table: `submission_id`, `station_key`, `reviewer_id`, `verdict`, `notes`, `created_at`.

**Acceptance.** AI agreement and station error rates are measured. High-hour projects get the time station first and a full audit. A clean low-hour project can skip stations that do not apply to its theme.

## Milestone 3 – The record and the gate

- Add `unified_record`: `submission_id`, `version`, `payload`, `validated_at`, `sent_at`, `errors`.
- Define the Airtable-facing record: technical features, deflation reason and benchmark, time ledger, findings and resolutions, decision, notes, links, images, BOM, identity.
- `POST .../record/validate` returns field-level errors; the UI shows them in the synthesis rail.
- `POST .../record/send` sends the validated record with the decision to the platform. Keep the Ari-compatible event shape; the platform maps it to Airtable.
- The final Unified submission stays manual; the gate removes the field checks, not the send.
- Import fine and spot-check outcomes manually for the pilot; add `escaped_defect` records and a weekly review.

**Acceptance.** No routine manual check of screenshots, justifications or repo links after Airtable. Every record that reaches Airtable validates against the schema. A spot-check defect is traceable to the station that missed it.

## Schema and migration details

`schemaVersion` goes from 12 to 13 in `schema.go`.

**Milestone 0 tables and columns.**

- `alter table review add column technical_features text not null default ''`
- `alter table review add column deflation_reason text not null default ''`
- `alter table journal_entry add column deflation_reason text`
- `create table finding (...)` as in 0.3, with indexes on `(program_id, external_id, status)` and `(opened_submission_id, status)`.
- `create table finding_return (...)`.
- `create table artifact_ref (...)`.
- `create table time_adjustment (...)`.

**Milestone 1 tables.** `station_definition`, `station_question`, `station_answer`.

**Milestone 2.** Add `source`, `ai_model`, `ai_confidence` to `station_answer`; add `audit_sample`.

**Milestone 3.** Add `unified_record` and `escaped_defect`.

Backfill:

- Existing reviews get empty fields; keep the outbound fallback for decisions made before the feature flag.
- Existing open submissions stay in the legacy queue until a programme setting enables the pipeline; `pipeline_enabled` and `pipeline_cutoff_at` in `program_setting`.
- `IRE_SEED_FIXTURES` seeds the three existing ships plus a multi-version Starbie-style fixture behind a test flag. Do not put production PII in fixtures.

The new version is a **new submission row** with the same `external_id` and a `previous_submission_id`. Findings follow the external id; answers and artifacts follow the submission id. This is the most important schema detail: do not key findings by submission id, or the ledger dies at the first resubmission.

## API and contract surface

New contract schemas:

- `finding`, `findingCreateRequest`, `findingUpdateRequest`, `findingVerifyRequest`;
- `stationDefinition`, `stationQueueResponse`, `stationAnswerRequest`;
- `returnComposerResponse` (or derive it in the UI from the bundle);
- `recordValidateResponse`.

Changed:

- `decisionRequest` and `amendDecisionRequest`: `technicalFeatures`, `deflationReason`, `findingIds`.
- `journalReviewRequest`: `deflationReason`.
- `reviewBundle`: `findings`, `stationAnswers`, `stations`, `timeLedger`, `returnPreview`, `artifacts`.
- `reviewPolicy`: `requireTechnicalFeatures`, `requireDeflationReason`, `pipelineEnabled`, `riskHoursThresholds`.

Keep the Ari-compatible status and event shapes. The new fields are additive in `justification` and already exist in the outbound schema. The native API can be richer; the adapter flattens it.

## UI map

| Surface | Change |
| --- | --- |
| `routes/queue/+page.svelte` | Station tabs and counts; existing filters remain; board link for organisers |
| `routes/stations/[key]/+page.svelte` | Focused station queue; one card at a time; auto-advance |
| `routes/review/+page.svelte` | Review sheet rail, findings, return composer, time ledger, record summary |
| `components/review/JournalTime.svelte` | Reason select, benchmark, reconciliation |
| `components/review/TimeEvidenceBar.svelte` | Adjustments and ledger delta |
| `components/review/DecisionForm` (extract) | Technical features, deflation reason, separate maker note |
| `components/review/ReturnComposer.svelte` | Finding checklist, generated note, unrun-station warning |
| `components/review/FixVerification.svelte` | Fixed, still open, disputed, no-change banner |
| `components/review/StationSheet.svelte` | Cell-mode progress and answers |
| `lib/queue-state.ts` | Station filters, counts and auto-advance |
| `lib/api.gen.ts` | Regenerate after the contract change |

Keyboard: `1`/`2`/`3` answer, `f` finding, `e` escalate, `j`/`k` next or previous, `Enter` save and advance, `Cmd/Ctrl+Enter` decision (existing), `A`/`R` decisions (existing). The station view should be usable without a mouse.

## Tests and fixtures

**Go unit tests.**

- Finding lifecycle: open, return, fixed, disputed, accepted, withdrawn; a fixed finding cannot be returned again.
- Return gate: every open finding in the envelope is included; a removal requires a reason; unrun stations warn; a hard block bypasses.
- Time ledger: per-entry sums, adjustments, reconciliation, required reason on cut, benchmark present.
- Station derivation: theme prerequisites, unanswered questions, open findings, no-change version.
- Artifact hashing and carry-forward: unchanged README keeps answers; changed README reopens only narrative questions.
- Decision validation: missing deflation reason, missing technical features when required, open fixable findings block approval.

**Contract tests.** `wt contract --dir .` generates and checks; `go test ./...` covers the generated structs; `bun test` covers the Valibot parsing and queue helpers.

**Fixture tests.** A reduced four-version Starbie case:

1. V1 gets a return with README, BOM, Gerber and case findings; assert the composer lists all four.
2. V2 fixes README and BOM, leaves Gerber and case; fix verification closes two and keeps two; the return lists only two.
3. V3 fixes the rest; approval is allowed; the outbound `technical_features` and `deflation_reason` match the input.
4. A V3 resubmission with the same payload is detected as no-change and is not reviewed.

Use synthetic names, text and URLs; the production repo is public, but the fixture must not carry PII.

**Acceptance run.**

```sh
go test ./...
go vet ./...
golangci-lint run ./...
cd web && bun run svelte:check && bun run build
wt contract --dir .
wt build --dir .
```

## First pull requests

1. **Decision justification end to end.** Contract fields, review columns, handler validation, outbound fields, UI, tests. This is the fix for the FINES pattern and can ship alone.
2. **Per-entry time reasons.** Journal reason, adjustment table, ledger reconciliation, generated justification, UI.
3. **Findings ledger and return composer.** Tables, API, bundle, composer, tests. No station UI.
4. **Fix verification and no-change detection.** Artifact hashes, new-version flow, stuck query.
5. **Stations and triage.** Definitions, answers, derived queue, station view, cell mode, permissions.
6. **Split and pre-answer.** Time, narrative and hardware queues; AI pre-answers; risk routing.
7. **Record and gate.** Validation, send, metrics, escaped-defect loop.

Each PR is shippable and has a feature flag. The first four solve the fines and the repeated-return loop without touching the queue layout.

## Definition of done for Milestone 0

- No approval with less than the captured time can be submitted without a deflation reason and a benchmark.
- No approval can be submitted without technical features when the programme requires them.
- The outbound event carries the reviewer's technical features and deflation reason, not a generated sentence.
- Every changes decision has at least one finding, and the maker note is the complete list.
- A fixed finding is closed with a resolution and is never returned again.
- An unchanged resubmission does not consume a review.
- The Starbie fixture produces two return batches and an approval, not twelve returns.
- The fine shapes in `FINES.md` are rejected by validation or caught by the profile and tutorial questions.

## Risks in this plan

- **Two sources of time truth.** `journal_entry.approved_seconds` and the ledger can drift; the server must reconcile on every save and reject a mismatch.
- **Findings keyed to the wrong identity.** The version is a new submission row. Key findings by `external_id`, never by `submission_id`.
- **Contract churn.** Regenerate and commit the generated files; do not hand-edit `contract.gen.go` or `api.gen.ts`.
- **Feature flags in the queue.** Existing open submissions must not suddenly appear in station queues mid-season; gate the pipeline by programme and cutoff.
- **Airtable field drift.** The gate validates against a schema that can change in another repository; keep a checked-in fixture and a checksum, as the plan already does for check policy.
- **Automation bias.** The AI pre-answer is stored as a suggestion with a model and confidence; the human confirmation is the answer that counts.
- **Over-process.** If the station split adds wait time without reducing escaped defects, collapse it back to cell mode. The board is a tool, not the goal.

## Open questions

1. What exactly does the Half Life Airtable record require, field by field? Direct Unified writes are ruled out; the Airtable schema is now the contract that matters.
2. What are the fine and changes rates by reason, and which of the three `FINES.md` patterns are most common? The pilot should classify 50 recent returns before choosing thresholds.
3. Will a shipwright-like triage role ever exist in Half Life, or is triage always a reviewer task? Design the permission either way, but do not build the role yet.
4. May AI read maker code, READMEs and images? Which provider, what retention, what consent?
5. What is the acceptable cycle time per station, and what is the audit sample for a clean low-hour project?
6. Who owns the station definitions, and how often are they reviewed against fines and spot-check outcomes?

## Appendix – The Airtable record checklist

Every record that leaves Ire should answer these without a human opening the project again:

- Is the repository the right one, public and readable?
- Is the cover a screenshot of this project, and are the README images real?
- Does the description describe the project, not the template?
- Is the maker's profile usable, with a name that can be paid?
- Are the required design, firmware, BOM and gerber files present?
- Does the README explain what it is, why, how, and what went wrong?
- Are the technical features specific, with evidence, and do they explain the hours?
- Is every journal entry descriptive, and is each cut justified with a reason and a benchmark?
- Do the commits belong to the maker, with sync commits filtered out?
- Are tutorials linked, and is there evidence they were applied?
- Are all findings resolved or accepted with a reason?
- Does the outbound payload carry the reviewer's own words, not a generated summary?
