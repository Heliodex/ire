package main

import (
	"context"
	"errors"
	"strings"

	"github.com/heliodex/better-deploy/wt/packages/v2/outbox"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

// schemaVersion is bumped when the prototype schema changes. The storage service records it and refuses to apply an older version twice.
const schemaVersion = 19

// stationSortHardware keeps the hardware station after the shared narrative station.
const stationSortHardware = 40

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

		`create table if not exists user_identity (
			user_id text not null references user(id) on delete cascade,
			provider text not null,
			subject text not null,
			created_at text not null,
			primary key (provider, subject),
			unique (user_id, provider)
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
			sort integer not null default 0,
			approved_seconds integer,
			reviewer_note text,
			deflation_reason text
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
			technical_features text not null default '',
			deflation_reason text not null default '',
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

		`create table if not exists station_definition (
			program_id text not null references program(id) on delete cascade,
			key text not null,
			label text not null,
			description text not null default '',
			stage integer not null default 1,
			sort integer not null default 0,
			themes text not null default '[]',
			active integer not null default 1,
			primary key (program_id, key)
		)`,

		`create table if not exists station_question (
			program_id text not null,
			station_key text not null,
			key text not null,
			prompt text not null,
			evidence_kind text not null default 'text',
			required integer not null default 1,
			sort integer not null default 0,
			primary key (program_id, station_key, key),
			foreign key (program_id, station_key) references station_definition(program_id, key) on delete cascade
		)`,

		`create table if not exists station_answer (
			id text primary key,
			submission_id text not null references submission(id) on delete cascade,
			station_key text not null,
			question_key text not null,
			answer text not null check (answer in ('pass','fail','na')),
			evidence text not null default '',
			artifact_hash text,
			reviewer_id text not null,
			created_at text not null,
			updated_at text not null,
			unique (submission_id, station_key, question_key, reviewer_id)
		)`,

		`create index if not exists station_answer_submission_idx on station_answer(submission_id, station_key)`,

		`create table if not exists finding (
			id text primary key,
			program_id text not null references program(id) on delete cascade,
			external_id text not null,
			opened_submission_id text not null references submission(id) on delete cascade,
			opened_version integer not null,
			station text not null default 'synthesis',
			question text not null default '',
			severity text not null check (severity in ('block','fixable','note','hold')),
			status text not null check (status in ('open','fixed','accepted','disputed','withdrawn')),
			summary text not null,
			detail text,
			maker_action text,
			evidence text not null default '{}',
			opened_by text,
			resolved_by text,
			resolved_submission_id text,
			resolved_version integer,
			resolution text,
			created_at text not null,
			updated_at text not null
		)`,

		`create index if not exists finding_project_idx on finding(program_id, external_id, status)`,

		`create index if not exists finding_opened_idx on finding(opened_submission_id, status)`,

		`create table if not exists finding_return (
			review_id text not null references review(id) on delete cascade,
			finding_id text not null references finding(id) on delete cascade,
			submission_id text not null references submission(id) on delete cascade,
			version integer not null,
			created_at text not null,
			primary key (review_id, finding_id)
		)`,

		`create table if not exists artifact_ref (
			submission_id text not null references submission(id) on delete cascade,
			kind text not null,
			key text not null,
			hash text not null,
			url text,
			created_at text not null,
			primary key (submission_id, kind, key)
		)`,

		`create table if not exists time_adjustment (
			id text primary key,
			submission_id text not null references submission(id) on delete cascade,
			kind text not null check (kind in ('bonus','cut')),
			seconds integer not null,
			reason text not null,
			note text,
			reviewer_id text,
			source text not null default '',
			created_at text not null
		)`,

		`create index if not exists time_adjustment_submission_idx on time_adjustment(submission_id)`,

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

		`create table if not exists review_note (
			id text primary key,
			program_id text not null references program(id) on delete cascade,
			subject_type text not null check (subject_type in ('submission','project','maker')),
			subject_id text not null,
			author_id text references user(id) on delete set null,
			author_email text not null,
			author_name text,
			body text not null,
			created_at text not null
		)`,

		`create index if not exists review_note_subject_idx on review_note(program_id, subject_type, subject_id, created_at desc)`,

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

		// Role definitions name the permission set behind each roster role. Admins edit
		// them and add new roles; the membership role check below is rebuilt to allow
		// custom names, so role validity is enforced by the app against this table.
		`create table if not exists role_definition (
			id text primary key,
			program_id text not null references program(id) on delete cascade,
			name text not null,
			permissions text not null default '[]',
			locked integer not null default 0,
			created_at text not null,
			unique (program_id, name)
		)`,

		// The original membership table limited roles with a check constraint. Custom
		// roles need free-form names, so the table is rebuilt without the check. Role
		// existence is enforced by the app: roles cannot be deleted while assigned.
		`create table if not exists membership_new (
			id text primary key,
			program_id text not null references program(id) on delete cascade,
			user_id text not null references user(id) on delete cascade,
			role text not null,
			tracks text not null default '["hardware"]',
			permissions text not null default '[]',
			active integer not null default 1,
			created_at text not null,
			created_by text,
			unique (program_id, user_id)
		)`,
		`insert or ignore into membership_new
			(id, program_id, user_id, role, tracks, permissions, active, created_at, created_by)
			select id, program_id, user_id, role, tracks, permissions, active, created_at, created_by from membership`,
		`drop table membership`,
		`alter table membership_new rename to membership`,
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

// lateColumns are columns added to a table after it first shipped. Their create statement carries them, which keeps a replay safe, but `create table if not exists` cannot add them to a database that already has the table. migrate asks the live schema which are missing and appends one alter for each, so an older database upgrades and a current one is left alone.
//
// Warning: an alter runs exactly once per database, so the versioned statement set changes after the first apply. The supervisor checksums the set per version, and any redeploy of that version afterwards mismatches. Prefer the ensure* fixups below (outside the versioned checksum) for additive columns, and follow a late-column deploy with another version bump to re-stabilise the set.
var lateColumns = []struct {
	table  string
	column string
	sql    string
}{
	{
		table:  "journal_entry",
		column: "approved_seconds",
		sql:    `alter table journal_entry add column approved_seconds integer`,
	},
	{
		table:  "journal_entry",
		column: "reviewer_note",
		sql:    `alter table journal_entry add column reviewer_note text`,
	},
	{
		table:  "journal_entry",
		column: "deflation_reason",
		sql:    `alter table journal_entry add column deflation_reason text`,
	},
	{
		table:  "review",
		column: "technical_features",
		sql:    `alter table review add column technical_features text not null default ''`,
	},
	{
		table:  "review",
		column: "deflation_reason",
		sql:    `alter table review add column deflation_reason text not null default ''`,
	},
}

// lateColumnStatements returns the alters the live database still needs. A table that is not there yet is skipped: its create statement already holds every column, and adding them again would fail.
func lateColumnStatements(ctx context.Context, store *storage.Client) ([]storage.Statement, error) {
	known := map[string]map[string]bool{}
	out := make([]storage.Statement, 0, len(lateColumns))

	for _, late := range lateColumns {
		columns, seen := known[late.table]
		if !seen {
			rows, err := store.Query(ctx, `select name from pragma_table_info(?)`, late.table)
			if err != nil {
				return nil, err
			}

			columns = map[string]bool{}

			for rows.Next() {
				var name string
				if err := rows.Scan(&name); err != nil {
					_ = rows.Close()
					return nil, err
				}

				columns[name] = true
			}

			err = rows.Err()
			_ = rows.Close()

			if err != nil {
				return nil, err
			}

			known[late.table] = columns
		}

		if len(columns) > 0 && !columns[late.column] {
			out = append(out, storage.Statement{SQL: late.sql})
		}
	}

	return out, nil
}

// migrate applies the prototype schema and then seeds the programme and demo ships.
func (a *ireApp) migrate(ctx context.Context) error {
	statements := schemaStatements()

	late, err := lateColumnStatements(ctx, a.store)
	if err != nil {
		return err
	}

	if err := a.store.Migrate(ctx, schemaVersion, append(statements, late...)...); err != nil {
		return err
	}

	if err := a.ensureStationAnswerReviewers(ctx); err != nil {
		return err
	}

	if err := a.ensureSubmittedAtColumn(ctx); err != nil {
		return err
	}

	return a.seed(ctx)
}

// ensureStationAnswerReviewers rebuilds the answer table from the shared per-question
// uniqueness to one answer per reviewer. The old table cannot express the new key, so
// the migration copies it across instead of altering it.
func (a *ireApp) ensureStationAnswerReviewers(ctx context.Context) error {
	var definition string

	err := a.store.QueryRow(ctx, `select sql from sqlite_master where type = 'table' and name = 'station_answer'`).Scan(&definition)
	if errors.Is(err, storage.ErrNoRows) {
		return nil
	}

	if err != nil {
		return err
	}

	if strings.Contains(definition, "unique (submission_id, station_key, question_key, reviewer_id)") {
		return nil
	}

	statements := []storage.TransactionStatement{
		{SQL: `create table station_answer_new (
			id text primary key,
			submission_id text not null references submission(id) on delete cascade,
			station_key text not null,
			question_key text not null,
			answer text not null check (answer in ('pass','fail','na')),
			evidence text not null default '',
			artifact_hash text,
			reviewer_id text not null,
			created_at text not null,
			updated_at text not null,
			unique (submission_id, station_key, question_key, reviewer_id)
		)`},
		{SQL: `insert into station_answer_new (id, submission_id, station_key, question_key, answer, evidence, artifact_hash, reviewer_id, created_at, updated_at)
			select id, submission_id, station_key, question_key, answer, evidence, artifact_hash, coalesce(nullif(reviewer_id, ''), 'legacy'), created_at, updated_at
			from station_answer`},
		{SQL: `drop table station_answer`},
		{SQL: `alter table station_answer_new rename to station_answer`},
		{SQL: `create index if not exists station_answer_submission_idx on station_answer(submission_id, station_key)`},
	}

	_, err = a.store.Transaction(ctx, statements...)

	return err
}

// ensureSubmittedAtColumn adds the upstream submission timestamp column when it is missing.
// It runs outside the versioned migration checksum so existing deployments can pick it up safely.
func (a *ireApp) ensureSubmittedAtColumn(ctx context.Context) error {
	rows, err := a.store.Query(ctx, `select name from pragma_table_info('submission') where name = 'submitted_at'`)
	if err != nil {
		return err
	}
	defer rows.Close()

	if rows.Next() {
		return rows.Err()
	}
	if err := rows.Err(); err != nil {
		return err
	}

	_, err = a.store.Exec(ctx, `alter table submission add column submitted_at text`)
	return err
}

// seed creates the Half Life programme and, when explicitly enabled, its dev reviewers and fixture ships. Every write is idempotent, so a partial seed is repaired on the next start. When dev accounts or fixtures are disabled, any leftover placeholder rows are removed so a production copy holds only invited reviewers and real Half Life ingests.
func (a *ireApp) seed(ctx context.Context) error {
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
	} else if err := a.removePlaceholderAccounts(ctx); err != nil {
		return err
	}

	if err := a.seedRoleDefinitions(ctx, now); err != nil {
		return err
	}

	if err := a.seedStations(ctx); err != nil {
		return err
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
		return a.removeFixtureShips(ctx)
	}

	return a.seedFixtures(ctx)
}

// removePlaceholderAccounts deletes the three development accounts and their memberships, sessions and identities. Memberships, sessions and identities cascade from the user rows; the login form no longer offers them once AllowDevAccounts is off.
func (a *ireApp) removePlaceholderAccounts(ctx context.Context) error {
	_, err := a.store.Exec(ctx, `delete from user where email in ('reviewer@example.com', 'organiser@example.com', 'admin@example.com')`)

	return err
}

// removeFixtureShips deletes the three demo ships reseeded by seedFixtures, so disabling fixtures leaves an empty queue ready for real Half Life ingests. Real submissions never use these ids or external ids.
func (a *ireApp) removeFixtureShips(ctx context.Context) error {
	_, err := a.store.Exec(ctx, `delete from submission where id in ('sub_fixture_1', 'sub_fixture_2', 'sub_fixture_3') or external_id in ('fix-clean:design', 'fix-warned:design', 'fix-blocked:design')`)

	return err
}

// seedPeople creates the three development accounts. They are placeholders for Hack Club Auth and must not survive a real deployment.
func (a *ireApp) seedPeople(ctx context.Context, now string) error {
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

// seedRoleDefinitions creates the built-in programme roles. Rows are never
// overwritten, so admin edits to permissions survive restarts.
func (a *ireApp) seedRoleDefinitions(ctx context.Context, now string) error {
	defaults := defaultRolePermissions()
	names := []string{roleAdmin, roleOrganizer, roleReviewer, roleAuditor}

	for _, name := range names {
		locked := 0
		if name == roleAdmin {
			locked = 1
		}

		if _, err := a.store.Exec(ctx, `insert into role_definition (id, program_id, name, permissions, locked, created_at)
			values (?, ?, ?, ?, ?, ?) on conflict(program_id, name) do nothing`,
			"rol_"+name, programID, name, marshalStrings(defaults[name]), locked, now); err != nil {
			return err
		}
	}

	return nil
}

// seedStations creates the Half Life review stations and their questions. Rows are never overwritten, so a programme edit survives restarts.
func (a *ireApp) seedStations(ctx context.Context) error {
	const evidenceText = "text"

	stations := []struct {
		key, label, description, themes string
		stage, sort                     int
		questions                       []struct {
			key, prompt, evidence string
			required              int
		}
	}{
		{
			key: "triage", label: "Triage", stage: 1, sort: 10, themes: "[]",
			description: "Cheap completeness checks before the deeper review.",
			questions: []struct {
				key, prompt, evidence string
				required              int
			}{
				{"cover_screenshot", "Is the cover a screenshot of this project?", "image", 1},
				{"description_real", "Does the description describe the project, not the template?", evidenceText, 1},
				{"repo_right", "Does the repository link lead to the right project?", "link", 1},
				{"images_present", "Are the required images present?", "image", 1},
				{"profile_valid", "Is the maker profile usable for payout?", evidenceText, 1},
			},
		},
		{
			key: "time", label: "Journals and time", stage: 1, sort: 20, themes: "[]",
			description: "Read each entry once and decide what time is productive.",
			questions: []struct {
				key, prompt, evidence string
				required              int
			}{
				{"entries_descriptive", "Does every journal entry describe what was worked on?", evidenceText, 1},
				{"time_productive", "Is the logged time productive work, not tutorial consumption?", evidenceText, 1},
				{"deflation_explained", "Is every cut explained with a reason and a benchmark?", evidenceText, 1},
			},
		},
		{
			key: "narrative", label: "README and claims", stage: 1, sort: 30, themes: "[]",
			description: "One reading of the README, commits and claims.",
			questions: []struct {
				key, prompt, evidence string
				required              int
			}{
				{"readme_explains", "Does the README explain the build, problems and solutions?", evidenceText, 1},
				{"commits_authored", "Do the commits belong to the maker, with sync commits ignored?", evidenceText, 1},
				{"claims_match", "Do the README and BOM claims match the files?", evidenceText, 1},
				{"features", "Are the specific technical features named?", evidenceText, 1},
			},
		},
		{
			key: "hardware", label: "Hardware and firmware", stage: 1, sort: stationSortHardware, themes: `["pcb","cad","synth","displays"]`,
			description: "A plausible design and firmware, with note-only issues.",
			questions: []struct {
				key, prompt, evidence string
				required              int
			}{
				{"design_plausible", "Is the design plausible, and are issues noted for the maker?", evidenceText, 1},
				{"bom_matches", "Does the BOM match the design and the parts list?", evidenceText, 1},
				{"firmware_matches", "Does the firmware match the claimed features?", evidenceText, 1},
			},
		},
		{
			key: "synthesis", label: "Synthesis and decision", stage: 1, sort: 50, themes: "[]",
			description: "Turn the answers into one record and one decision.",
			questions: []struct {
				key, prompt, evidence string
				required              int
			}{
				{"findings_resolved", "Are all open findings resolved or included in the return?", evidenceText, 1},
				{"hours_reasonable", "Are the approved hours and deflation reason defensible?", evidenceText, 1},
				{"notes_complete", "Are the maker note and internal note complete?", evidenceText, 1},
			},
		},
		{
			key: "verify", label: "Verify record", stage: 2, sort: 10, themes: "[]",
			description: "Stage 2 checks the record and samples the evidence.",
			questions: []struct {
				key, prompt, evidence string
				required              int
			}{
				{"record_complete", "Are the answers, evidence and time ledger complete?", evidenceText, 1},
				{"evidence_sample", "Does a sample of the evidence support the answers?", evidenceText, 1},
			},
		},
	}

	for _, station := range stations {
		if _, err := a.store.Exec(ctx, `insert into station_definition (program_id, key, label, description, stage, sort, themes, active)
			values (?, ?, ?, ?, ?, ?, ?, 1) on conflict(program_id, key) do nothing`,
			programID, station.key, station.label, station.description, station.stage, station.sort, station.themes); err != nil {
			return err
		}

		for index, question := range station.questions {
			if _, err := a.store.Exec(ctx, `insert into station_question (program_id, station_key, key, prompt, evidence_kind, required, sort)
				values (?, ?, ?, ?, ?, ?, ?) on conflict(program_id, station_key, key) do nothing`,
				programID, station.key, question.key, question.prompt, question.evidence, question.required, index*10); err != nil {
				return err
			}
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
