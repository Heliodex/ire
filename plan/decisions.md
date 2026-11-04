# Decisions

Architecture decisions for the review platform. Each one records the choice, the reason, and what it costs. If a decision is reversed, change it here in the same pull request as the code.

---

## ADR-001: Review owns review state, Half Life owns money

**Context.** Half Life already has `finalizeReview`, the credit ledger, tier pricing, grant cards, printer awards and maker notifications. Ari does not write any of that; it sends a decision and Half Life applies it. Duplicating any of it in Review would create a second source of truth for money.

**Decision.** Review stores the queue, evidence snapshots, checks, claims, stage transitions, decisions and audit trail. It never writes a ledger row, tier, grant or printer award. A final decision is delivered to Half Life as `approved` / `changes` / `rejected`, with the approved time in seconds and the reviewer's note. Half Life remains the only writer of credit and the only sender of maker notifications.

**Consequences.**

- Review can be rebuilt or replaced without touching the economy.
- A decision is not "done" until Half Life accepts the delivery; Review must show a failed delivery and retry it.
- Review cannot answer "what is this maker's balance?" and should not try. It links to Half Life where that question belongs.
- The outbound contract is the most important interface in the system. It gets contract tests, signatures and a replay path.

**Rejected alternative.** Read Half Life's Postgres directly. It would couple Review to another service's schema, break the `wt`/SQLite shape, and give two services write access to money-adjacent state.

---

## ADR-002: Ari-compatible edge first, native API second

**Context.** Half Life's Ari integration is real and working: `@hackclub/ari` sends signed ships, receives signed webhooks, polls `ships.status`, and has retry and reconciliation jobs. A big-bang rewrite of Half Life's integration would put the programme at risk for no reviewer-visible benefit.

**Decision.** Review exposes Ari's ingest, withdraw, status and outbound event shapes at the edge. Half Life can switch by changing `ARI_BASE_URL`, `ARI_PROGRAM_ID`, `ARI_SIGNING_SECRET` and `ARI_WEBHOOK_SECRET`. The same service also exposes a richer native API (`/api/v1/...`) that returns stage, checks, fraud and timeline. Half Life migrates to the native API after the compatibility path has proven itself.

**Consequences.**

- Cutover is a configuration change, not a deployment of two services.
- The Ari-compatible layer is a translation boundary and must be tested against the actual SDK and Half Life handler, not just the docs.
- The native API must not be constrained by Ari's vocabulary. It can be richer; the adapter flattens it.
- The compatibility layer stays for other programmes that already integrate with Ari, but it is not the product's internal model.

**Rejected alternative.** Only a native API. It is cleaner, but it makes the first cutover a coordinated change across two repositories, which is the riskiest possible first step.

---

## ADR-003: Port the checks, do not reinvent them

**Context.** Ari's automatic checks are the main reason it is trusted: repo existence and public visibility, README text and images, required file kinds per hardware theme, README BOM tables, EasyEDA design detection, commit history and authorship, Hackatime time, Lapse clips and demos. Half Life also has a strong repo-check implementation in TypeScript. A fresh implementation would be worse for months.

**Decision.** Extract the check logic from `ari-webhooks` (Go: forge clients, Hackatime, Lapse, file hours, demo probe) and the theme policy from Half Life's `src/lib/repo-checks.ts` into a `internal/checks` package with no database dependency. Keep the same rules and fixtures. Where the old code is coupled to Ari's Postgres schema, rewrite only the persistence adapter, not the rule.

**Consequences.**

- The first check phase is a porting and parity-testing exercise, not a research project.
- The public repo contains no private fraud logic. Fraud is a provider interface (ADR-008).
- Check results must be snapshots tied to a submission version, or a reviewer cannot reproduce what they saw.
- A shadow comparison against Ari is a phase gate, not a nice-to-have.

**Rejected alternative.** Call Ari's check service. It would preserve the dependency on the service we are replacing, and Ari's internal endpoints are not a public contract.

---

## ADR-004: Stage 0 checks, Stage 1 review, Stage 2 confirm/fraud

**Context.** Ari's two passes are entangled with fraud and second-pass configuration. The programme wants a fast first review and a deliberate second stage, with fraud only at the second stage.

**Decision.** Model the pipeline explicitly:

- **Stage 0** is automatic preflight. It is not a human queue. Checks run and decide whether a submission is ready, needs attention, or is still being checked.
- **Stage 1** is the reviewer queue. A reviewer can make a final decision when policy allows.
- **Stage 2** is the organiser queue. It holds the decisions policy says need a second look, and it is the only stage that can call the fraud provider.

A stage transition is a row, not an inferred status. The UI always says which stage a submission is in and why.

**Consequences.**

- "Fraud only at stage 2" is a validation rule (`fraud.stage == 2`), not a convention.
- The same review screen can serve both stages with different policy and fields.
- Stage 2 can be off entirely; stage 1 is then final.
- The policy engine is small, pure and testable. It is the first thing to unit test.

**Rejected alternative.** Keep Ari's boolean `secondPass` plus fraud modes. It makes the stage visible only through side effects and cannot express "hold only high-risk approvals".

---

## ADR-005: Hack Club Auth via OAuth, not platform identity

**Context.** `wt` apps can use the supervisor's platform identity (email and password) or own their login. Reviewers already have Hack Club accounts, and Ari authenticates them with HCA (`openid profile email slack_id`). Creating another password is a support burden and a security downgrade.

**Decision.** Use HCA's authorisation-code flow with PKCE through `packages/v2/oauth`, store opaque sessions in SQLite through `packages/v2/session`, and map the HCA email to a program membership. Keep a dev-only login path for local work.

**Consequences.**

- No new passwords, no password reset email dependency.
- The app needs HCA client credentials and an exact redirect URI per environment.
- Membership is an allowlist: signing in does not grant review rights. An admin invites an email or Slack ID.
- If HCA is down, reviewers cannot sign in. That is acceptable for a staff tool; a read-only break-glass admin token is not.

**Rejected alternative.** Platform identity. It would be simpler to run, but it would create a second identity for the same people and lose the Slack ID that reviewer notifications and avatars use.

---

## ADR-006: Static SvelteKit, Go API, typed contracts

**Context.** The repository is already a `wt` app. `wt` supports only a static SvelteKit frontend with `adapter-static`; there is no Node server in the component. The contract pipeline turns Valibot schemas into JSON Schema and Go types.

**Decision.** Keep the `wt` shape. Every request and response body has a Valibot schema in `web/src/lib/contract.ts` (or a module it re-exports). The generator emits Go structs. The browser parses every response through the same schema, including errors. No `+page.server.ts`, no `+server.ts`, no form actions.

**Consequences.**

- A changed field name fails at compile time in Go and at parse time in the browser.
- The frontend fetches after hydration; loading and error states are explicit.
- Long lists are paginated and hot paths are precomputed because SQLite queries have a five-second ceiling.
- The contract generation step must run in CI and in `wt deploy`; a stale generated file is a build failure.

**Rejected alternative.** A Node/SvelteKit server with Postgres. It would be a different platform and would not deploy with `wt`.

---

## ADR-007: Webhooks are hints; the API is truth

**Context.** Ari's most expensive failure mode is a lost delivery. Half Life has scripts that resend from the admin panel and reconcile by polling `ships.status`; the status response historically omitted the reviewer's words and the approved hours. A review platform should not require archaeology.

**Decision.** Every state change writes a durable row, an outbound delivery and an audit event. Every event is readable through a list endpoint (`GET /api/v1/reviews?since=...`, `GET /api/v1/events?since=...`) and every submission is readable through `GET /api/v1/status?...`. Webhooks are an optimisation for latency. A receiver can reconstruct everything from the API after any gap.

**Consequences.**

- No admin-panel resend is needed to recover a lost event; a resend is only for a receiver that is temporarily down.
- The status response is a superset: stage, checks, fraud, first-pass review, final review, timeline.
- The outbox still exists, because push is useful and receivers expect it. It retries and exposes failures.
- This increases storage slightly; it removes an entire class of operational scripts.

**Rejected alternative.** Webhook-only with resend buttons. That is the current pain.

---

## ADR-008: Public repo, external fraud provider

**Context.** Ari's fraud tooling is partly private. Half Life is public. A public repo cannot contain detection rules, prompts or thresholds.

**Decision.** The public repo defines a `FraudProvider` interface and a `disabled` implementation. The real provider is an external HTTP service reached with a secret. Review calls it only from stage 2 and stores the verdict, score and a generic explanation. No detection logic lives in this repository.

**Consequences.**

- The public build has no private submodule and no build tags.
- The provider contract becomes a stable interface with its own tests and timeout/retry policy.
- Fraud cannot be tested end-to-end without a fake provider; the test suite includes one.
- If the provider is unavailable, the policy decides whether to hold, retry or allow an organiser override with a reason.

**Rejected alternative.** A `private/` submodule with build tags. It is possible, but it adds build complexity and a second repository for a feature that is not core to the review workflow.

---

## ADR-009: One programme configured, many programmes supported

**Context.** The immediate need is Half Life hardware. The code should not become a Half Life-only fork, but multi-programme features (programme switcher, per-programme branding, per-programme tracks) can dominate v1.

**Decision.** Every table that matters is scoped by `program_id`. The API takes a programme. The UI can render a programme switcher when a user has more than one membership. v1 seeds one programme (Half Life) and one track (hardware). Nothing in the domain assumes either.

**Consequences.**

- A future programme is a row and a membership, not a fork.
- Every query must filter by programme; tests should prove a cross-programme ID cannot be read.
- The UI can stay visually simple for one programme without painting itself into a corner.

---

## ADR-010: Integer seconds, one time model

**Context.** Ari and Half Life both learned that floats and minutes in storage cause double-counting and rounding disputes. Ari's rule is: store, compute and pass integer seconds; hours are display only.

**Decision.** All time is integer seconds with a `_seconds` suffix. Hours, minutes and durations are formatting. Journal time, Hackatime time and timelapse coverage are separate concepts; timelapse is evidence, not an additive source. Percentages and splits round once through a shared helper.

**Consequences.**

- The API and database cannot silently disagree about 0.1h.
- The UI formats seconds; it never does arithmetic on floats.
- A reviewer adjustment stores seconds, not a decimal hour.
- Half Life remains authoritative for the final credited number; Review sends the seconds the reviewer saw.

**Rejected alternative.** Minutes, as Ari's older columns used. It preserves a legacy mistake for no benefit.
