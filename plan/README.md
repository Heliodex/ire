# Ire platform plan

- **Status:** draft for review
- **Date:** 2026-11-03
- **Scope:** a replacement for Ari as Half Life's review surface
- **Working name:** Ire (the repository is already `review`)

## The short version

Build a standalone review service in the `wt` stack: a Go API compiled to a Wasm component, a prerendered SvelteKit frontend, typed Valibot-to-Go contracts, and per-app SQLite. Half Life keeps owning participants, hours, tiers, the ledger and maker notifications. Ire owns the review queue, claims, evidence snapshots, automatic checks, stage transitions, reviewer decisions and the audit trail. It sends final decisions back to Half Life exactly as Ari does today.

The product is deliberately familiar to Ari reviewers: the same queue, claim, evidence tabs, hours rail, decision buttons, keyboard shortcuts and 2nd pass. The differences are where Ari hurts:

- **Stage 2 is a real, configurable stage.** Fraud review only runs at stage 2. Stage 1 can be final for low-risk work.
- **Stage 1 is built for speed.** One screen, no wizard, optional notes on approval, auto-claim, prefetch, auto-advance and a fast lane for clean submissions.
- **The API tells the truth.** Every submission's stage, checks, decision, review and timeline is available through `GET`; webhooks are a push optimisation, not the only way to learn what happened.
- **Checks are preserved, not reinvented.** The open-source check pipeline in `ari-webhooks` and the repo checks in Half Life are ported behind a small, tested interface.
- **Configuration is live and auditable.** Stage rules, fraud placement, check policy, forms and snippets are database rows, not env vars or code changes.

## Key decisions

| Decision | Choice | Why |
| --- | --- | --- |
| Where review state lives | Ire's own SQLite database | Jamstack-style, `wt`-native, no cross-service Postgres coupling |
| Who writes money | Half Life only | The ledger, tiers and grants already have one correct implementation |
| Integration shape | Ari-compatible edge first, native API second | Half Life can switch by changing secrets and base URL; no big-bang rewrite |
| Automatic checks | Port `ari-webhooks` and Half Life's repo checks | Ari's thoroughness is the main thing to keep; a rewrite would lose it |
| Ire stages | Stage 0 checks, Stage 1 reviewer, Stage 2 organiser/fraud | Makes "fraud only at stage 2" explicit and testable |
| Fraud | External provider, called only from stage 2 | Keeps private detection logic out of the public repo |
| Reviewer auth | Hack Club Auth via OAuth | Reviewers already have HCA accounts; Ari uses it today |
| Frontend | Static SvelteKit, no server load/actions | The only supported `wt` shape |
| Background work | Host schedules plus durable jobs | A Wasm component cannot run a background goroutine |
| Time | Integer seconds everywhere | Matches Ari and Half Life; no floats in storage or transport |
| Program scope | Program-scoped from day one, one program configured | Avoids hardcoding Half Life while keeping v1 small |

The reasoning behind each is in [decisions.md](./decisions.md).

## What success looks like

- A clean hardware design or build can be reviewed and approved in **under two minutes**, with no required typing if the reviewer has nothing to add.
- A reviewer who knows Ari can work the queue **without training**: same layout, same vocabulary, same shortcuts.
- An organiser can see **exactly why** a decision is held, what fraud returned, and who decided at each stage.
- Half Life can answer "what happened to this hand-in?" with one `GET`, not by resending webhooks.
- The automatic checks are **at least as thorough as Ari's** on a shadow sample, with differences explained.
- A submission that fails a blocking check never silently disappears; it lands in a human queue with the reason.

## Non-goals for v1

- Reviewer VMs, NDA-gated evidence, collaborative ships and the software track.
- A second implementation of the ledger, tiers, grants, shop or printer award.
- A participant-facing submission or status UI. Half Life already has one.
- Maker email or Slack delivery from Ire. Half Life already sends those from its decision path.
- A public priority-review form, MCP server, or agent-facing API.
- Replacing Half Life's `/review` console immediately. It can stay as a fallback until cutover is proven.

## Reading order

1. [01-product.md](./01-product.md) – users, stages, workflows, Ari parity, what changes.
2. [02-architecture.md](./02-architecture.md) – system shape, `wt` constraints, components, deployment.
3. [03-data-model.md](./03-data-model.md) – SQLite schema, invariants, indexes, migrations.
4. [04-checks.md](./04-checks.md) – check catalogue, porting map, jobs, policy, fraud boundary.
5. [05-ui.md](./05-ui.md) – routes, review screen, stage 1 speed, stage 2, shortcuts.
6. [06-integration.md](./06-integration.md) – ingest, status, outbound events, Ari compatibility, cutover.
7. [07-delivery.md](./07-delivery.md) – phases, testing, deployment, risks, open questions.
8. [09-review-pipeline.md](./09-review-pipeline.md) – review pipeline models, findings ledger, return gate, recommended model.
9. [10-implementation.md](./10-implementation.md) – the Starbie case, the fines, milestones, schema, API, first pull requests.
10. [decisions.md](./decisions.md) – the architectural decisions and their consequences.
11. [todo.md](./todo.md) – what is built, what is stubbed, and the next four implementation steps.

## Current repository state

The vertical slice is built. The notes scaffold is gone: the app now has signed ingest, the queue, claims, drafts, stage 1 decisions, stage 2 holds, the signed outbox, fixtures and the Ari-styled responsive UI. [`todo.md`](./todo.md) records what is done, what is deliberately fake and the next four steps. The plan below remains the design source.

## The first thing to build

Do not start with the review screen. Start with the **wire contract and the submission lifecycle**:

1. Replace the notes schema with `program`, `submission`, `submission_version`, `claim`, `review`, `outbox` and `audit_log`.
2. Implement signed ingest, `GET status`, `GET reviews?since` and signed outbound delivery.
3. Run a synthetic ship through ingest → checks stub → stage 1 decision → outbound and assert the exact bytes Half Life receives.
4. Only then build the queue and review screen on top of that lifecycle.

That order makes the integration real early, and it keeps the UI from inventing a data model that the wire cannot support.
