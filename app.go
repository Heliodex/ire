package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/heliodex/better-deploy/wt/packages/v2/app"
	"github.com/heliodex/better-deploy/wt/packages/v2/storage"
)

const (
	programID   = "prg_halflife"
	programSlug = "half-life"
	programName = "Half Life"

	timestampFormat = app.TimestampFormat

	maxNote = 4000

	maxIngestBody = 16 << 20
	maxAPIBody    = 64 << 10

	defaultLimit = 50
	maxLimit     = 100
	maxOffset    = 10000

	claimTTL = 30 * time.Minute

	// reviewerHold keeps a fresh resubmission with its previous reviewer before other reviewers can take it.
	reviewerHold = 5 * 24 * time.Hour

	checkStaleAfter = 10 * time.Minute

	outboundTimeout = 15 * time.Second

	minDescriptionLength = 160
	minJournalText       = 80
	minClipCoverage      = 0.2

	signatureWindow = 300

	secondsPerHour = 3600
	secondsPerDay  = 24 * secondsPerHour
	maxHours       = 30 * secondsPerDay

	sessionTTL = 7 * 24 * time.Hour

	statusChecking       = "checking"
	statusNeedsAttention = "needs_attention"
	statusReady          = "ready"
	statusClaimed        = "claimed"
	statusInReview       = "in_review"
	statusStage2Hold     = "stage2_hold"
	statusDecided        = "decided"
	statusWithdrawn      = "withdrawn"

	stageOne = 1

	decisionApproved = "approved"
	decisionChanges  = "changes"
	decisionRejected = "rejected"

	statusPass  = "pass"
	statusWarn  = "warn"
	statusFail  = "fail"
	statusError = "error"
	statusSkip  = "skip"

	severityInfo  = "info"
	severityWarn  = "warn"
	severityBlock = "block"

	checkOverallPass = "pass"
	checkOverallWarn = "warn"
	checkOverallFail = "fail"

	queueFilterAll       = "all"
	queueFilterEasy      = "easy"
	queueFilterFast      = "fast"
	queueFilterHard      = "hard"
	queueFilterMedium    = "medium"
	queueFilterAttention = "attention"
	queueFilterMine      = "mine"

	roleReviewer  = "reviewer"
	roleOrganizer = "organizer"
	roleAdmin     = "admin"
	roleAuditor   = "auditor"

	// permWildcard grants every permission. Only the locked admin role holds it.
	permWildcard = "*"
	// permReview allows claiming, drafting and deciding ships at stage 1.
	permReview = "review"
	// permSecondPass allows acting on held second-pass ships: stage 2 claims and decisions, send-back and fraud approval.
	permSecondPass = "second_pass"
	// permChecksManage allows rerunning repository checks and refreshing evidence.
	permChecksManage = "checks_manage"
	// permViewPII allows reading unredacted delivery payloads and other identifying details.
	permViewPII = "view_pii"
	// permNoteManage allows removing notes written by someone else.
	permNoteManage = "note_manage"
	// permRosterManage allows inviting members and changing roles and active flags.
	permRosterManage = "roster_manage"
	// permAuditRead allows reading the programme audit log.
	permAuditRead = "audit_read"
	// permSessionRevoke allows revoking another member's sessions.
	permSessionRevoke = "session_revoke"
	// permRoleManage allows creating roles and changing role permissions. Only admins hold it.
	permRoleManage = "role_manage"

	cookieName = "review_session"
)

// config is the process-level configuration that never belongs in SQLite: the shared secrets and the delivery mode.
type config struct {
	IngestSecret, OutboundSecret, OutboundURL, GitHubToken, GitHubAPIBase, HackatimeAPIBase, HCClientID, HCClientSecret, HCRedirectURI, HCAuthBase, BootstrapEmail, BootstrapRole string
	SeedFixtures, AllowDevAccounts, DisableRepoChecks, EnableJobs, OutboundDryRun, Shadow                                                                                         bool
	Enqueue                                                                                                                                                                       func(context.Context, app.JobRequest) (bool, error)
}

// ireApp owns the store, HTTP client and routes. Tests build it around an in-memory storage service and a fake clock.
type ireApp struct {
	store                          *storage.Client
	client                         *http.Client
	mux                            *http.ServeMux
	config                         config
	clock                          func() time.Time
	browserLimiter, machineLimiter *rateLimiter
	forgeCache                     *forgeCache
}

// newIreApp builds the routes around one store and outbound client.
func newIreApp(store *storage.Client, client *http.Client, cfg config) *ireApp {
	if client == nil {
		client = http.DefaultClient
	}

	if cfg.EnableJobs && cfg.Enqueue == nil {
		cfg.Enqueue = func(ctx context.Context, request app.JobRequest) (bool, error) {
			_, created, err := app.EnqueueJob(ctx, client, request)
			return created, err
		}
	}

	a := &ireApp{store: store, client: client, mux: http.NewServeMux(), config: cfg, clock: time.Now}
	a.browserLimiter = newRateLimiter(browserRateLimit, a.now)
	a.machineLimiter = newRateLimiter(machineRateLimit, a.now)
	a.forgeCache = newForgeCache()
	a.routes()

	return a
}

// handler exposes the routes to the wasm wiring or a test.
func (a *ireApp) handler() http.Handler {
	return a.withCompression(a.withRateLimit(a.mux))
}

// now returns the app's clock in UTC.
func (a *ireApp) now() time.Time {
	return a.clock().UTC()
}

// nowText formats the current time with the contract's fixed-width layout.
func (a *ireApp) nowText() string {
	return a.now().Format(timestampFormat)
}

// later returns the current time plus d, formatted for storage.
func (a *ireApp) later(d time.Duration) string {
	return a.now().Add(d).Format(timestampFormat)
}

// routes registers the Ari-compatible edge, the native API and the supervised tick endpoint.
func (a *ireApp) routes() {
	a.mux.HandleFunc("POST /api/ingest/{program}", a.handleIngest)
	a.mux.HandleFunc("POST /api/ingest/{program}/withdraw", a.handleWithdraw)
	a.mux.HandleFunc("GET /api/ingest/{program}/status", a.handleStatus)

	a.mux.HandleFunc("POST /api/v1/ingest/{program}", a.handleIngest)
	a.mux.HandleFunc("POST /api/v1/withdraw/{program}", a.handleWithdraw)
	a.mux.HandleFunc("GET /api/v1/status/{program}", a.handleStatus)
	a.mux.HandleFunc("GET /api/v1/reviews/{program}", a.handleReviews)
	a.mux.HandleFunc("GET /api/v1/events/{program}", a.handleEvents)

	a.mux.HandleFunc("GET /api/v1/me", a.handleMe)
	a.mux.HandleFunc("POST /api/v1/dev/login", a.handleDevLogin)
	a.mux.HandleFunc("POST /api/v1/dev/logout", a.handleLogout)
	a.mux.HandleFunc("POST /api/v1/logout", a.handleLogout)
	a.mux.HandleFunc("GET /api/v1/auth/config", a.handleAuthConfig)
	a.mux.HandleFunc("GET /", a.handlePage)
	a.mux.HandleFunc("GET /auth/login", a.handleHCAStart)
	a.mux.HandleFunc("GET /auth/callback", a.handleHCACallback)
	a.mux.HandleFunc("POST /api/v1/dev/reset", a.handleDevReset)

	a.mux.HandleFunc("GET /api/v1/programs/{program}/queue", a.handleQueue)
	a.mux.HandleFunc("GET /api/v1/programs/{program}/stations", a.handleStations)
	a.mux.HandleFunc("GET /api/v1/programs/{program}/stations/{station}/queue", a.handleStationQueue)
	a.mux.HandleFunc("GET /api/v1/programs/{program}/stations/{station}", a.handleStation)
	a.mux.HandleFunc("PUT /api/v1/programs/{program}/stations/{station}", a.handleSaveStation)
	a.mux.HandleFunc("DELETE /api/v1/programs/{program}/stations/{station}", a.handleDeleteStation)
	a.mux.HandleFunc("POST /api/v1/programs/{program}/import-reviewed", a.handleImportReviewed)
	a.mux.HandleFunc("GET /api/v1/programs/{program}/overview", a.handleOverview)
	a.mux.HandleFunc("GET /api/v1/programs/{program}/reviewed", a.handleReviewed)
	a.mux.HandleFunc("GET /api/v1/programs/{program}/search", a.handleSearch)
	a.mux.HandleFunc("GET /api/v1/programs/{program}/submissions/{id}", a.handleBundle)
	a.mux.HandleFunc("POST /api/v1/programs/{program}/submissions/{id}/claim", a.handleClaim)
	a.mux.HandleFunc("POST /api/v1/programs/{program}/submissions/{id}/heartbeat", a.handleHeartbeat)
	a.mux.HandleFunc("DELETE /api/v1/programs/{program}/submissions/{id}/claim", a.handleRelease)
	a.mux.HandleFunc("PUT /api/v1/programs/{program}/submissions/{id}/draft", a.handleDraft)
	a.mux.HandleFunc("PATCH /api/v1/programs/{program}/submissions/{id}/journal/{entry}", a.handleJournalReview)
	a.mux.HandleFunc("PUT /api/v1/programs/{program}/submissions/{id}/stations/{station}/answers/{question}", a.handleStationAnswer)
	a.mux.HandleFunc("GET /api/v1/programs/{program}/submissions/{id}/findings", a.handleFindings)
	a.mux.HandleFunc("POST /api/v1/programs/{program}/submissions/{id}/findings", a.handleFindingCreate)
	a.mux.HandleFunc("PATCH /api/v1/programs/{program}/submissions/{id}/findings/{finding}", a.handleFindingUpdate)
	a.mux.HandleFunc("POST /api/v1/programs/{program}/submissions/{id}/notes", a.handleNoteCreate)
	a.mux.HandleFunc("DELETE /api/v1/programs/{program}/notes/{id}", a.handleNoteDelete)
	a.mux.HandleFunc("POST /api/v1/programs/{program}/submissions/{id}/decision", a.handleDecision)
	a.mux.HandleFunc("POST /api/v1/programs/{program}/submissions/{id}/decision/revert", a.handleRevertDecision)
	a.mux.HandleFunc("POST /api/v1/programs/{program}/submissions/{id}/decision/amend", a.handleAmendDecision)
	a.mux.HandleFunc("POST /api/v1/programs/{program}/submissions/{id}/send-back", a.handleSendBack)
	a.mux.HandleFunc("POST /api/v1/programs/{program}/submissions/{id}/evidence/refresh", a.handleEvidenceRefresh)
	a.mux.HandleFunc("GET /api/v1/programs/{program}/submissions/{id}/file", a.handleSubmissionFile)
	a.mux.HandleFunc("POST /api/v1/programs/{program}/submissions/{id}/checks/rerun", a.handleChecksRerun)
	a.mux.HandleFunc("GET /api/v1/programs/{program}/submissions/{id}/checks/head", a.handleChecksHead)
	a.mux.HandleFunc("GET /api/v1/programs/{program}/outbox", a.handleOutbox)
	a.mux.HandleFunc("GET /api/v1/programs/{program}/parity", a.handleParity)
	a.mux.HandleFunc("POST /api/v1/programs/{program}/members/{id}/revoke-sessions", a.handleRevokeSessions)
	a.mux.HandleFunc("GET /api/v1/programs/{program}/members", a.handleMembers)
	a.mux.HandleFunc("POST /api/v1/programs/{program}/members", a.handleInviteMember)
	a.mux.HandleFunc("PATCH /api/v1/programs/{program}/members/{id}", a.handleUpdateMember)
	a.mux.HandleFunc("GET /api/v1/programs/{program}/roles", a.handleListRoles)
	a.mux.HandleFunc("POST /api/v1/programs/{program}/roles", a.handleCreateRole)
	a.mux.HandleFunc("PATCH /api/v1/programs/{program}/roles/{name}", a.handleUpdateRole)
	a.mux.HandleFunc("DELETE /api/v1/programs/{program}/roles/{name}", a.handleDeleteRole)
	a.mux.HandleFunc("GET /api/v1/programs/{program}/reviewers/{key}", a.handleReviewerProfile)
	a.mux.HandleFunc("GET /api/v1/programs/{program}/makers/{email}", a.handleMakerProfile)
	a.mux.HandleFunc("GET /api/v1/programs/{program}/audit", a.handleAudit)
	a.mux.HandleFunc("GET /api/v1/dev/outbox/received", a.handleFakeDeliveries)

	a.mux.HandleFunc("GET /metrics", a.handleMetrics)

	a.mux.HandleFunc("POST /jobs/tick", a.handleTick)
	a.mux.HandleFunc("POST /jobs/checks", a.handleJobChecks)
}

// handlePage gates every HTML navigation before the static build is served.
func (a *ireApp) handlePage(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	signedIn := a.hasSession(r)

	switch {
	case path == "/":
		if signedIn {
			http.Redirect(w, r, "/stations", http.StatusFound)
			return
		}

		app.StaticPage(w, "/")
	case path == "/login":
		target := "/"
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}

		if signedIn {
			target = "/stations"
		}

		//nolint:gosec // The target is a fixed local path plus the request query.
		http.Redirect(w, r, target, http.StatusFound)
	case path == "/review" && r.URL.Query().Get("id") != "":
		http.Redirect(w, r, "/review/"+url.PathEscape(r.URL.Query().Get("id")), http.StatusFound)
	case path == "/makers" && r.URL.Query().Get("email") != "":
		http.Redirect(w, r, "/makers/"+url.PathEscape(r.URL.Query().Get("email")), http.StatusFound)
	case protectedPage(path):
		if !signedIn {
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}

		page := path
		if strings.HasPrefix(path, "/review/") {
			// Every review id shares the prerendered loading shell; the client router hydrates the real id.
			page = "/review/loading"
		}

		if strings.HasPrefix(path, "/station/") {
			// Station keys are dynamic; every key shares the prerendered shell.
			page = "/station/loading"
		}

		if strings.HasPrefix(path, "/reviewers/") {
			page = "/reviewers"
		}

		if strings.HasPrefix(path, "/makers/") {
			page = "/makers"
		}

		app.StaticPage(w, page)
	default:
		http.NotFound(w, r)
	}
}

// protectedPage reports the prerendered routes that need a session.
func protectedPage(path string) bool {
	for _, prefix := range []string{"/overview", "/queue", "/reviewed", "/review", "/reviewers", "/makers", "/deliveries", "/admin", "/stations", "/station"} {
		if path == prefix || strings.HasPrefix(path, prefix+"/") {
			return true
		}
	}

	return false
}

// programFor resolves the path's programme slug, writing a 404 when it does not exist.
func (a *ireApp) programFor(w http.ResponseWriter, r *http.Request) (programRow, bool) {
	slug := r.PathValue("program")

	var row programRow

	row.Slug = slug

	if err := a.store.QueryRow(r.Context(), `select id, slug, name, outbound_url, created_at, coalesce(policy_version, 1) from program where slug = ? and active = 1`, slug).
		Scan(&row.ID, &row.Slug, &row.Name, &row.OutboundURL, &row.CreatedAt, &row.PolicyVersion); err != nil {
		if errors.Is(err, storage.ErrNoRows) {
			app.WriteError(w, http.StatusNotFound, "unknown program")
			return programRow{}, false
		}

		app.WriteError(w, http.StatusInternalServerError, err.Error())

		return programRow{}, false
	}

	return row, true
}

// programByID loads a programme by its opaque id.
func (a *ireApp) programByID(ctx context.Context, id string) (programRow, error) {
	var row programRow

	row.ID = id

	err := a.store.QueryRow(ctx, `select id, slug, name, outbound_url, created_at, coalesce(policy_version, 1) from program where id = ?`, id).
		Scan(&row.ID, &row.Slug, &row.Name, &row.OutboundURL, &row.CreatedAt, &row.PolicyVersion)
	if err != nil {
		return programRow{}, err
	}

	return row, nil
}

// newID returns a short prefixed identifier, for example evt_9f2c....
func newID(prefix string) string {
	var raw [12]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return prefix + "_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	}

	return prefix + "_" + hex.EncodeToString(raw[:])
}

// sha256Hex returns the lowercase hex digest of data.
func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}

// parseLimit reads a bounded limit query parameter.
func parseLimit(r *http.Request) int {
	value := r.URL.Query().Get("limit")
	if value == "" {
		return defaultLimit
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 1 {
		return defaultLimit
	}

	return min(parsed, maxLimit)
}

// parseOffset reads a bounded offset, so a deep page cannot scan without limit.
func parseOffset(r *http.Request) int {
	value := r.URL.Query().Get("offset")
	if value == "" {
		return 0
	}

	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0
	}

	return min(parsed, maxOffset)
}

// parseSince reads the optional since cursor as an RFC 3339 timestamp.
func parseSince(r *http.Request) (string, bool) {
	value := strings.TrimSpace(r.URL.Query().Get("since"))
	if value == "" {
		return "", true
	}

	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return "", false
	}

	return parsed.UTC().Format(timestampFormat), true
}

// writeStorage writes a storage failure as a 500 without leaking SQL.
func writeStorage(w http.ResponseWriter, err error) {
	app.WriteError(w, http.StatusInternalServerError, err.Error())
}

// programRow is the subset of a programme the prototype reads.
type programRow struct {
	ID, Slug, Name, OutboundURL, CreatedAt string
	PolicyVersion                          int
}
