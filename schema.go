package main

import (
	"context"

	"github.com/heliodex/better-deploy/wt/packages/v2/outbox"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// schemaVersion is bumped when the prototype schema changes. The storage service records it and refuses to apply an older version twice.
const schemaVersion = 8

// schemaStatements is the prototype subset of plan/03-data-model.md: programmes, people, sessions, ships, evidence, checks, claims, reviews, drafts, timeline, audit, outbox and settings. Fraud, forms, counters and retention jobs come later.
//
//nolint:funlen // One statement per table keeps the migration reviewable next to the plan.
func schemaStatements() []storage.Statement {
	statements := []string{
		`create table if not exists program (
			id text primary key,
			slug text not null unique,
			name text not null,
			timezone text not null default 'UTC',
			active integer not null default 1,
			created_at text not null,
			updated_at text not null,
			outbound_url text not null default '',
			outbound_enabled integer not null default 1,
			signing_secret_last4 text not null default '',
			outbound_secret_last4 text not null default '',
			policy_version integer not null default 1
		)`,

		`create table if not exists user (
			id text primary key,
			email text not null unique,
			name text,
			slack_id text,
			avatar_url text,
			created_at text not null,
			last_login_at text
		)`,

		`create table if not exists membership (
			id text primary key,
			program_id text not null references program(id) on delete cascade,
			user_id text not null references user(id) on delete cascade,
			role text not null check (role in ('admin','organizer','reviewer','auditor')),
			tracks text not null default '["hardware"]',
			permissions text not null default '[]',
			active integer not null default 1,
			created_at text not null,
			created_by text,
			unique (program_id, user_id)
		)`,

		`create table if not exists login_flow (
			state text primary key,
			verifier text not null,
			nonce text not null,
			redirect_uri text not null,
			created_at text not null
		)`,

		`create table if not exists session (
			id text primary key,
			user_id text not null references user(id) on delete cascade,
			expires_at text not null,
			created_at text not null,
			last_seen_at text not null,
			user_agent text,
			ip text
		)`,

		`create table if not exists submission (
			id text primary key,
			program_id text not null references program(id) on delete cascade,
			external_id text not null,
			version integer not null default 1,
			status text not null,
			stage integer not null default 0,
			priority integer not null default 0,
			held_reason text,
			title text not null,
			description text,
			track text not null default 'hardware',
			theme text,
			phase text,
			week integer,
			maker_email text not null,
			maker_name text,
			maker_slack_id text,
			maker_hackatime_id text,
			repo_url text,
			demo_url text,
			thumbnail_url text,
			requested_tier integer,
			grant_cap_cents integer,
			bom_total_cents integer,
			hours_claimed_seconds integer not null default 0,
			hours_journal_seconds integer not null default 0,
			hours_hackatime_seconds integer not null default 0,
			hours_approved_seconds integer,
			check_summary text,
			ingested_at text not null,
			first_opened_at text,
			decided_at text,
			previous_submission_id text,
			created_at text not null,
			updated_at text not null,
			unique (program_id, external_id, version)
		)`,

		`create unique index if not exists submission_open_external_idx
			on submission(program_id, external_id)
			where status in ('checking','needs_attention','ready','claimed','in_review','stage2_hold')`,

		`create index if not exists submission_queue_idx
			on submission(program_id, stage, status, priority desc, ingested_at asc, id)`,

		`create index if not exists submission_reviewed_idx
			on submission(program_id, decided_at desc)`,

		`create table if not exists submission_version (
			submission_id text not null references submission(id) on delete cascade,
			version integer not null,
			payload text not null,
			payload_sha256 text not null,
			received_at text not null,
			delivery_id text,
			primary key (submission_id, version)
		)`,

		`create unique index if not exists submission_version_delivery_idx
			on submission_version(delivery_id)
			where delivery_id is not null`,

		`create table if not exists ingest_delivery (
			delivery_id text primary key,
			program_id text not null references program(id) on delete cascade,
			external_id text,
			version integer,
			submission_id text,
			status text not null,
			http_status integer not null,
			payload_sha256 text,
			received_at text not null,
			processed_at text
		)`,

		`create table if not exists journal_entry (
			id text primary key,
			submission_id text not null references submission(id) on delete cascade,
			external_id text,
			at text not null,
			seconds integer not null default 0,
			title text,
			text text,
			markdown text,
			media text,
			timelapses text,
			sort integer not null default 0
		)`,

		`create index if not exists journal_submission_idx on journal_entry(submission_id, at)`,

		`create table if not exists hackatime_link (
			submission_id text not null references submission(id) on delete cascade,
			project text not null,
			seconds integer not null default 0,
			fetched_at text,
			error text,
			primary key (submission_id, project)
		)`,

		`create table if not exists bom_line (
			id text primary key,
			submission_id text not null references submission(id) on delete cascade,
			name text not null,
			purpose text,
			quantity integer not null default 1,
			unit_cost_cents integer not null default 0,
			vendor text,
			link text,
			sort integer not null default 0
		)`,

		`create index if not exists bom_submission_idx on bom_line(submission_id, sort)`,

		`create table if not exists check_result (
			id text primary key,
			submission_id text not null references submission(id) on delete cascade,
			version integer not null,
			check_key text not null,
			label text not null,
			group_key text not null,
			status text not null,
			severity text not null,
			summary text not null,
			detail text,
			data text,
			checked_at text not null,
			duration_ms integer not null default 0
		)`,

		`create index if not exists check_result_latest_idx
			on check_result(submission_id, version, check_key, checked_at desc)`,

		`create table if not exists repo_snapshot (
			submission_id text not null references submission(id) on delete cascade,
			version integer not null,
			forge text not null,
			full_name text not null,
			default_branch text,
			head_sha text,
			file_count integer not null default 0,
			truncated integer not null default 0,
			readme_text text,
			files text not null default '[]',
			commits text not null default '[]',
			fetched_at text not null,
			error text,
			primary key (submission_id, version)
		)`,

		`create table if not exists check_run (
			submission_id text not null references submission(id) on delete cascade,
			version integer not null,
			status text not null default 'queued',
			attempts integer not null default 0,
			started_at text not null,
			finished_at text,
			error text,
			runner_version text not null default '',
			primary key (submission_id, version)
		)`,

		`create table if not exists check_ack (
			id text primary key,
			submission_id text not null references submission(id) on delete cascade,
			version integer not null,
			check_key text not null,
			reviewer_id text not null references user(id) on delete cascade,
			reason text,
			created_at text not null,
			unique (submission_id, version, check_key, reviewer_id)
		)`,

		`create table if not exists claim (
			submission_id text primary key references submission(id) on delete cascade,
			reviewer_id text not null references user(id) on delete cascade,
			stage integer not null,
			expires_at text not null,
			heartbeat_at text not null,
			created_at text not null
		)`,

		`create index if not exists claim_expiry_idx on claim(expires_at)`,

		`create table if not exists review (
			id text primary key,
			submission_id text not null references submission(id) on delete cascade,
			version integer not null,
			stage integer not null,
			reviewer_id text,
			reviewer_email text not null,
			reviewer_name text,
			reviewer_role text not null,
			decision text not null check (decision in ('approved','changes','rejected')),
			note_to_maker text not null default '',
			internal_note text,
			hours_approved_seconds integer,
			hours_adjustments text,
			tier_override integer,
			grant_override_cents integer,
			fields text,
			checklist text,
			acknowledgements text,
			policy_version integer not null,
			held integer not null default 0,
			held_reason text,
			is_final integer not null default 1,
			supersedes_review_id text,
			invalidated_at text,
			invalidated_by text,
			invalidation_reason text,
			idempotency_key text,
			created_at text not null,
			unique (submission_id, idempotency_key)
		)`,

		`create table if not exists review_override (
			review_id text primary key references review(id) on delete cascade,
			reason text not null,
			created_at text not null
		)`,

		`create unique index if not exists review_final_idx
			on review(submission_id)
			where is_final = 1 and invalidated_at is null`,

		`create index if not exists review_submission_idx on review(submission_id, created_at desc)`,

		`create table if not exists draft (
			submission_id text not null references submission(id) on delete cascade,
			reviewer_id text not null references user(id) on delete cascade,
			stage integer not null,
			note text not null default '',
			internal_note text not null default '',
			"values" text not null default '{}',
			checklist text not null default '[]',
			adjustments text not null default '{}',
			updated_at text not null,
			primary key (submission_id, reviewer_id)
		)`,

		`create table if not exists submission_event (
			id text primary key,
			submission_id text not null references submission(id) on delete cascade,
			kind text not null,
			actor_id text,
			actor_email text,
			data text,
			created_at text not null
		)`,

		`create index if not exists submission_event_idx on submission_event(submission_id, created_at)`,

		`create table if not exists audit_log (
			id text primary key,
			program_id text not null references program(id) on delete cascade,
			actor_id text,
			actor_email text,
			actor_ip text,
			actor_user_agent text,
			action text not null,
			target_type text,
			target_id text,
			before_json text,
			after_json text,
			reason text,
			request_id text,
			created_at text not null
		)`,

		`create index if not exists audit_program_idx on audit_log(program_id, created_at desc)`,

		`create table if not exists outbox (
			id text primary key,
			program_id text not null references program(id) on delete cascade,
			submission_id text,
			event text not null,
			dedupe_key text not null unique,
			payload text not null,
			url text not null,
			status text not null,
			attempts integer not null default 0,
			next_attempt_at text not null,
			last_http_status integer,
			last_error text,
			delivery_id text,
			created_at text not null,
			delivered_at text
		)`,

		`create index if not exists outbox_due_idx on outbox(status, next_attempt_at)`,

		`create table if not exists program_setting (
			program_id text not null references program(id) on delete cascade,
			key text not null,
			value text not null,
			updated_at text not null,
			updated_by text,
			primary key (program_id, key)
		)`,

		`create table if not exists fake_receiver_delivery (
			id text primary key,
			delivery_id text not null unique,
			event text not null,
			timestamp text not null,
			body text not null,
			signature text not null,
			verified integer not null default 0,
			received_at text not null
		)`,
	}

	out := make([]storage.Statement, 0, len(statements)+8)
	for _, statement := range statements {
		out = append(out, storage.Statement{SQL: statement})
	}

	boxStatements, err := outbox.Outbox{Table: outboxTable}.Schema()
	if err == nil {
		out = append(out, boxStatements...)
	}

	// Move the prototype's first outbox table into the shared package table. insert or ignore keeps a replay harmless.
	out = append(out, storage.Statement{SQL: `insert or ignore into webhook_outbox
		(id, event, dedupe_key, payload, url, headers, metadata, status, attempts, max_attempts, next_attempt_at,
			last_http_status, last_error, delivery_id, created_at, delivered_at)
		select id, event, dedupe_key, payload, url, '{}',
			json_object('program_id', program_id, 'submission_id', coalesce(submission_id, '')),
			status, attempts, 8, next_attempt_at, coalesce(last_http_status, 0), coalesce(last_error, ''),
			coalesce(delivery_id, ''), created_at, delivered_at
		from outbox`})

	return out
}

// migrate applies the prototype schema and then seeds the programme and demo ships.
func (a *reviewApp) migrate(ctx context.Context) error {
	if err := a.store.Migrate(ctx, schemaVersion, schemaStatements()...); err != nil {
		return err
	}

	return a.seed(ctx)
}

// seed creates the Half Life programme, its dev reviewers and, when missing, the three fixture ships. Every write is idempotent, so a partial seed is repaired on the next start.
func (a *reviewApp) seed(ctx context.Context) error {
	now := a.nowText()

	outboundURL := a.config.OutboundURL
	if outboundURL == "" {
		outboundURL = "fake://receiver"
	}

	if _, err := a.store.Exec(ctx, `insert into program (id, slug, name, timezone, active, created_at, updated_at, outbound_url, outbound_enabled, signing_secret_last4, outbound_secret_last4, policy_version)
		values (?, ?, ?, 'Europe/London', 1, ?, ?, ?, 1, ?, ?, 1)
		on conflict(id) do nothing`,
		programID, programSlug, "Half Life", now, now, outboundURL,
		last4(a.config.IngestSecret), last4(a.config.OutboundSecret)); err != nil {
		return err
	}

	if a.config.AllowDevAccounts {
		if err := a.seedPeople(ctx, now); err != nil {
			return err
		}
	}

	settings := map[string]string{
		"review_policy":          `{"holdOnWarn":true,"holdMinApprovedSeconds":0,"holdTiers":[2,3],"noteRequiredForChanges":true,"fraudMinStage":2}`,
		"allow_legacy_signature": "true",
	}
	for key, value := range settings {
		if _, err := a.store.Exec(ctx, `insert into program_setting (program_id, key, value, updated_at) values (?, ?, ?, ?)
			on conflict(program_id, key) do update set value = excluded.value, updated_at = excluded.updated_at`,
			programID, key, value, now); err != nil {
			return err
		}
	}

	if !a.config.SeedFixtures {
		return nil
	}

	return a.seedFixtures(ctx)
}

// seedPeople creates the three development accounts. They are placeholders for Hack Club Auth and must not survive a real deployment.
func (a *reviewApp) seedPeople(ctx context.Context, now string) error {
	people := []struct {
		id, email, name, role string
	}{
		{"usr_reviewer", "reviewer@example.com", "Rae Reviewer", "reviewer"},
		{"usr_organiser", "organiser@example.com", "Oli Organiser", "organizer"},
		{"usr_admin", "admin@example.com", "Ada Admin", "admin"},
	}

	for _, person := range people {
		if _, err := a.store.Exec(ctx, `insert into user (id, email, name, created_at) values (?, ?, ?, ?)
			on conflict do update set name = excluded.name`,
			person.id, person.email, person.name, now); err != nil {
			return err
		}

		if _, err := a.store.Exec(ctx, `insert into membership (id, program_id, user_id, role, tracks, permissions, active, created_at) values (?, ?, ?, ?, '["hardware"]', '[]', 1, ?)
			on conflict(program_id, user_id) do update set role = excluded.role, active = 1`,
			"mem_"+person.id, programID, person.id, person.role, now); err != nil {
			return err
		}
	}

	return nil
}

// last4 returns the final four characters of a secret for display, never the secret itself.
func last4(secret string) string {
	if len(secret) <= 4 {
		return secret
	}

	return secret[len(secret)-4:]
}
