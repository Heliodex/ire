package main

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"strings"
	"time"

	"review/contract"

	"github.com/heliodex/better-deploy/wt/packages/app"
	"github.com/heliodex/better-deploy/wt/packages/storage"
)

// A fixture is a real half-life project row reduced to the fields a reviewer needs: the repo and description, the evidence, the claimed hours and a prescribed check outcome. The fixture suite stands in for the network checks until they are ported.
type fixture struct {
	externalID   string
	title        string
	description  string
	repoURL      string
	thumbnailURL string
	theme        string
	makerEmail   string
	makerName    string
	tier         int
	hoursClaimed int
	hoursJournal int
	priority     int
	journals     []fixtureJournal
	bom          []fixtureBom
	checks       []fixtureCheck
	priorReview  *fixturePriorReview
}

type fixtureJournal struct {
	at     string
	hours  int
	title  string
	text   string
	clips  int
	media  int
	coverS int
}

type fixtureBom struct {
	name    string
	purpose string
	qty     int
	cents   int
	vendor  string
	link    string
}

type fixtureCheck struct {
	key      string
	label    string
	group    string
	status   string
	severity string
	summary  string
	detail   string
}

type fixturePriorReview struct {
	result   string
	feedback string
	reviewer string
	at       string
}

type checkCounts struct {
	Overall string `json:"overall"`
	Pass    int    `json:"pass"`
	Warn    int    `json:"warn"`
	Fail    int    `json:"fail"`
	Error   int    `json:"error"`
	Running int    `json:"running"`
}

// checkCountsFor summarises a list of checks. A blocking failure outranks a warning, and a warning outranks a pass.
func checkCountsFor(checks []contract.CheckResult) checkCounts {
	counts := checkCounts{Overall: checkOverallPass}

	for _, check := range checks {
		switch check.Status {
		case statusPass:
			counts.Pass++
		case statusWarn:
			counts.Warn++
			counts.Overall = checkOverallWarn
		case statusFail:
			counts.Fail++

			if check.Severity == severityBlock {
				counts.Overall = checkOverallFail
			} else if counts.Overall != checkOverallFail {
				counts.Overall = checkOverallWarn
			}
		case statusError:
			counts.Error++

			if check.Severity == severityBlock {
				counts.Overall = checkOverallFail
			} else if counts.Overall != checkOverallFail {
				counts.Overall = checkOverallWarn
			}
		}
	}

	return counts
}

// seedFixtures inserts the three demo ships: a clean approval, a ship with warnings, and one the checks refuse.
//
//nolint:dupl,goconst,mnd // The fixture lists are deliberately literal: prices, durations and near-identical check rows are demo data.
func (a *reviewApp) seedFixtures(ctx context.Context) error {
	now := a.now()
	base := now.Add(-3 * time.Hour)

	fixtures := []fixture{
		{
			externalID:   "fix-clean:design",
			title:        "Starbie",
			description:  "A tiny motion-controlled digital pet on a custom star-shaped PCB. The board carries a XIAO ESP32-C3, a 0.96-inch OLED, an MPU6050, a DHT11 and two Cherry MX switches. I drew my own panel art and added an angry expression when the pet's stats run out.",
			repoURL:      "https://github.com/C6-H/Starbie",
			thumbnailURL: "/projects/starbie.png",
			theme:        "starbie",
			makerEmail:   "c6h@example.com",
			makerName:    "C6-H",
			tier:         1,
			// 6.4 hours, matching the approved row in .projects.csv.
			hoursClaimed: 23040,
			hoursJournal: 23040,
			priority:     30,
			journals: []fixtureJournal{
				{at: base.Add(2 * time.Hour).Format(timestampFormat), hours: 3, title: "Schematic and layout", text: "Placed the XIAO, the OLED header, the MPU6050 header, the DHT11 and the two switches. Routed the board on two layers and filled the back copper with GND. The DRC was clean after fixing one stray track.", clips: 2, coverS: 9000},
				{at: base.Add(5 * time.Hour).Format(timestampFormat), hours: 3, title: "Firmware and panel art", text: "Added the angry face for an empty stat, drew the star trail, exported the gerbers and wrote the BOM. The board is 96 by 92 mm and the XIAO's USB port faces the edge.", clips: 1, coverS: 5400},
			},
			bom: []fixtureBom{
				{name: "Seeed XIAO ESP32-C3", purpose: "Microcontroller", qty: 1, cents: 499, vendor: "Seeed", link: "https://www.seeedstudio.com/"},
				{name: "0.96 inch SSD1306 OLED", purpose: "Display", qty: 1, cents: 312, vendor: "AliExpress", link: "https://www.aliexpress.com/"},
				{name: "MPU6050 module", purpose: "Motion sensing", qty: 1, cents: 180, vendor: "AliExpress", link: "https://www.aliexpress.com/"},
				{name: "DHT11", purpose: "Temperature and humidity", qty: 1, cents: 150, vendor: "AliExpress", link: "https://www.aliexpress.com/"},
				{name: "Cherry MX switches", purpose: "Buttons", qty: 2, cents: 60, vendor: "AliExpress", link: "https://www.aliexpress.com/"},
				{name: "10k resistor", purpose: "DHT11 pull-up", qty: 1, cents: 5, vendor: "AliExpress", link: "https://www.aliexpress.com/"},
				{name: "PCB fabrication", purpose: "5 boards", qty: 5, cents: 420, vendor: "JLCPCB", link: "https://jlcpcb.com/"},
			},
			checks: []fixtureCheck{
				{key: "repo.accessible", label: "Repository reachable", group: "repo", status: statusPass, severity: severityInfo, summary: "Public repository found", detail: "github.com/C6-H/Starbie is public."},
				{key: "repo.readme", label: "README present", group: "repo", status: statusPass, severity: severityInfo, summary: "README found (2.4 KB)", detail: "Description, features and build steps are present."},
				{key: "repo.readme.images", label: "README images", group: "repo", status: statusPass, severity: severityInfo, summary: "4 images", detail: "Schematic, board render and two build photos."},
				{key: "repo.bom", label: "Bill of materials", group: "repo", status: statusPass, severity: severityInfo, summary: "BOM.csv with 7 lines", detail: "Prices, vendors and links are present."},
				{key: "repo.design_files", label: "Design files", group: "repo", status: statusPass, severity: severityInfo, summary: "KiCad source and gerbers found", detail: ".kicad_pcb, .kicad_sch and gerbers.zip are present."},
				{key: "repo.commits", label: "Commit history", group: "repo", status: statusPass, severity: severityInfo, summary: "18 commits over 5 days", detail: "Work is spread across the submission window."},
				{key: "evidence.journals", label: "Journal evidence", group: "evidence", status: statusPass, severity: severityInfo, summary: "2 entries with 3 clips", detail: "Entries include progress images and specific mistakes."},
			},
		},
		{
			externalID:   "fix-warned:design",
			title:        "OpenTrace",
			description:  "An 8-channel USB logic analyzer built around an RP2350A. The board has USB-C, a 16 Mbit QSPI flash, an eight-channel buffer and a four-layer stackup. It is my first board with a proper ground plane and a differential USB pair.",
			repoURL:      "https://github.com/Shubhayu-Banerjee/Open-Trace",
			thumbnailURL: "/projects/opentrace.jpg",
			theme:        "pcb",
			makerEmail:   "opentrace@example.com",
			makerName:    "Shubhayu-Banerjee",
			tier:         1,
			hoursClaimed: 37800,
			hoursJournal: 37800,
			priority:     20,
			journals: []fixtureJournal{
				{at: base.Add(90 * time.Minute).Format(timestampFormat), hours: 5, title: "Schematic and QSPI flash", text: "Wired the RP2350A, the 16 Mbit flash and the USB-C port. I read the RP2350 datasheet for the QSPI pin mapping and checked the boot pins against the reference design.", clips: 1, coverS: 9000},
				{at: base.Add(4 * time.Hour).Format(timestampFormat), hours: 5, title: "Routing and firmware", text: "Routed the USB pair, the buffers and the probe inputs. The firmware is a first pass that samples one channel and prints over USB serial. I still need to add the trigger logic.", clips: 0, coverS: 0},
			},
			bom: []fixtureBom{
				{name: "RP2350A", purpose: "Microcontroller", qty: 1, cents: 620, vendor: "LCSC", link: "https://www.lcsc.com/"},
				{name: "W25Q16 flash", purpose: "Firmware storage", qty: 1, cents: 95, vendor: "LCSC", link: "https://www.lcsc.com/"},
				{name: "USB-C receptacle", purpose: "Power and data", qty: 1, cents: 40, vendor: "LCSC", link: "https://www.lcsc.com/"},
				{name: "74LVC245 buffer", purpose: "Input protection", qty: 1, cents: 55, vendor: "LCSC", link: "https://www.lcsc.com/"},
				{name: "Passives", purpose: "Decoupling", qty: 30, cents: 120, vendor: "LCSC", link: "https://www.lcsc.com/"},
				{name: "PCB fabrication", purpose: "5 boards, 4 layers", qty: 5, cents: 1900, vendor: "JLCPCB", link: "https://jlcpcb.com/"},
			},
			checks: []fixtureCheck{
				{key: "repo.accessible", label: "Repository reachable", group: "repo", status: statusPass, severity: severityInfo, summary: "Public repository found", detail: "github.com/Shubhayu-Banerjee/Open-Trace is public."},
				{key: "repo.readme", label: "README present", group: "repo", status: statusPass, severity: severityInfo, summary: "README found (1.1 KB)", detail: "The README is short but explains the board."},
				{key: "repo.readme.images", label: "README images", group: "repo", status: statusWarn, severity: severityWarn, summary: "1 image (2 recommended)", detail: "The board render is present; the schematic is not."},
				{key: "repo.bom", label: "Bill of materials", group: "repo", status: statusFail, severity: severityWarn, summary: "No BOM.csv; BOM only in the platform", detail: "The repo has no BOM file and the platform list is incomplete."},
				{key: "repo.design_files", label: "Design files", group: "repo", status: statusPass, severity: severityInfo, summary: "KiCad source and gerbers found", detail: "All four PCB files and the production zip are present."},
				{key: "evidence.journals", label: "Journal evidence", group: "evidence", status: statusWarn, severity: severityWarn, summary: "1 of 2 entries reads as AI-written", detail: "The second entry is generic and has no images."},
				{key: "repo.commits", label: "Commit history", group: "repo", status: statusPass, severity: severityInfo, summary: "14 commits over 6 days", detail: "Work is spread across the submission window."},
			},
			priorReview: &fixturePriorReview{result: "CHANGES_REQUESTED", feedback: "Organise the repo into PCB, firmware and production folders. Add the BOM to the README.", reviewer: "reviewer@example.com", at: base.Add(-36 * time.Hour).Format(timestampFormat)},
		},
		{
			externalID:   "fix-blocked:design",
			title:        "Smart Alarm Clock",
			description:  "An ESP32 alarm clock that shows the weather and wakes you with a buzzer. The board has an ESP32 module, a 1.8-inch SPI display, a DS3231 RTC, a TP4056 charger and four buttons.",
			repoURL:      "https://github.com/SBdevz/Smart-Alarm-Clock",
			thumbnailURL: "/projects/smart-alarm-clock.png",
			theme:        "pcb",
			makerEmail:   "sbdevz@example.com",
			makerName:    "SBdevz",
			tier:         1,
			hoursClaimed: 40320,
			hoursJournal: 40320,
			priority:     10,
			journals: []fixtureJournal{
				{at: base.Add(time.Hour).Format(timestampFormat), hours: 6, title: "Schematic and Wokwi model", text: "Built the ESP32 circuit in Wokwi with the display, the RTC and the buzzer, then moved it to KiCad. The alarm logic is written but the board files still need work.", clips: 0, coverS: 0},
				{at: base.Add(4 * time.Hour).Format(timestampFormat), hours: 5, title: "Board layout", text: "Started the layout and routed the display and USB-C. The repo is still private while I clean up the README.", clips: 0, coverS: 0},
			},
			bom: []fixtureBom{
				{name: "ESP32 DevKit V1", purpose: "Microcontroller", qty: 1, cents: 650, vendor: "AliExpress", link: "https://www.aliexpress.com/"},
				{name: "1.8 inch SPI display", purpose: "Display", qty: 1, cents: 420, vendor: "AliExpress", link: "https://www.aliexpress.com/"},
				{name: "DS3231 RTC", purpose: "Real-time clock", qty: 1, cents: 250, vendor: "AliExpress", link: "https://www.aliexpress.com/"},
				{name: "TP4056 charger", purpose: "Battery charging", qty: 1, cents: 90, vendor: "AliExpress", link: "https://www.aliexpress.com/"},
				{name: "PCB fabrication", purpose: "5 boards", qty: 5, cents: 500, vendor: "JLCPCB", link: "https://jlcpcb.com/"},
			},
			checks: []fixtureCheck{
				{key: "repo.accessible", label: "Repository reachable", group: "repo", status: statusFail, severity: severityBlock, summary: "Repository is private or missing", detail: "github.com/SBdevz/Smart-Alarm-Clock returned 404 without credentials."},
				{key: "repo.readme", label: "README present", group: "repo", status: statusSkip, severity: severityInfo, summary: "Not checked", detail: "The repository could not be read."},
				{key: "repo.bom", label: "Bill of materials", group: "repo", status: statusSkip, severity: severityInfo, summary: "Not checked", detail: "The repository could not be read."},
				{key: "repo.design_files", label: "Design files", group: "repo", status: statusSkip, severity: severityInfo, summary: "Not checked", detail: "The repository could not be read."},
				{key: "evidence.journals", label: "Journal evidence", group: "evidence", status: statusPass, severity: severityInfo, summary: "2 entries logged", detail: "Both entries explain the work and name the next step."},
			},
		},
	}

	for index := range fixtures {
		// A partial seed leaves earlier fixtures behind; skip the ids that already exist instead of failing on the primary key.
		var existing string
		if err := a.store.QueryRow(ctx, `select id from submission where id = ?`, fixtureID(index)).Scan(&existing); err == nil {
			continue
		}

		if err := a.insertFixture(ctx, index, fixtures[index], now); err != nil {
			// Another instance can seed the same fixture during a deploy. If its row exists now, this insert lost the race and the fixture is already there.
			if scanErr := a.store.QueryRow(ctx, `select id from submission where id = ?`, fixtureID(index)).Scan(&existing); scanErr == nil {
				continue
			}

			return err
		}
	}

	return nil
}

// fixtureID returns the fixed id for a demo ship, so reseeding can skip one that is already present.
func fixtureID(index int) string {
	return fmt.Sprintf("sub_fixture_%d", index+1)
}

// handleDevReset clears the demo ships and reseeds them. It exists so a rehearsal can start from the same three rows; REVIEW_DEV_AUTH=0 removes it.
func (a *reviewApp) handleDevReset(w http.ResponseWriter, r *http.Request) {
	if !a.config.AllowDevAccounts {
		app.WriteError(w, http.StatusNotFound, "development reset is disabled")
		return
	}

	if _, ok := a.currentReviewer(w, r); !ok {
		return
	}

	now := a.nowText()
	tables := []string{"draft", "check_ack", "claim", "review", "outbox", "submission_event", "journal_entry", "bom_line", "check_result", "submission_version", "ingest_delivery", "submission"}

	statements := make([]storage.TransactionStatement, 0, len(tables)+1)
	for _, table := range tables {
		statements = append(statements, storage.TransactionStatement{SQL: "delete from " + table})
	}

	statements = append(statements, storage.TransactionStatement{SQL: `delete from fake_receiver_delivery`})

	if _, err := a.store.Transaction(r.Context(), statements...); err != nil {
		writeStorage(w, err)
		return
	}

	if err := a.seedFixtures(r.Context()); err != nil {
		writeStorage(w, err)
		return
	}

	app.WriteJSON(w, http.StatusOK, map[string]string{"seededAt": now})
}

// insertFixture writes one demo ship, its snapshot, evidence and prescribed checks.
func (a *reviewApp) insertFixture(ctx context.Context, index int, f fixture, now time.Time) error {
	ingested := now.Add(-time.Duration(index+1) * 2 * time.Hour)
	id := fixtureID(index)

	checks := make([]contract.CheckResult, 0, len(f.checks))
	for _, check := range f.checks {
		checks = append(checks, contract.CheckResult{
			Key:       check.key,
			Label:     check.label,
			Group:     check.group,
			Status:    check.status,
			Severity:  check.severity,
			Summary:   check.summary,
			Detail:    stringPtr(check.detail),
			CheckedAt: ingested.Add(time.Minute).Format(timestampFormat),
		})
	}

	summary := checkCountsFor(checks)

	summaryJSON, err := json.Marshal(summary)
	if err != nil {
		return err
	}

	status := statusReady
	if summary.Overall == checkOverallFail {
		status = statusNeedsAttention
	}

	request := contract.IngestRequest{
		ExternalID:   f.externalID,
		Title:        f.title,
		Description:  f.description,
		RepoURL:      stringPtr(f.repoURL),
		ThumbnailURL: stringPtr(f.thumbnailURL),
		Track:        stringPtr("hardware"),
		Maker: contract.IngestMaker{
			Email: f.makerEmail,
			Name:  f.makerName,
		},
		Meta: &contract.IngestMeta{
			Phase:         stringPtr("design"),
			Theme:         stringPtr(f.theme),
			Week:          new(1),
			RequestedTier: new(f.tier),
			GrantCapCents: new(3000),
			Hours: &contract.IngestHours{
				ClaimedSeconds:   f.hoursClaimed,
				JournalSeconds:   f.hoursJournal,
				HackatimeSeconds: 0,
			},
		},
	}
	if f.priorReview != nil {
		request.Meta.PriorReviews = []contract.IngestPriorReview{{
			Result:   f.priorReview.result,
			Feedback: stringPtr(f.priorReview.feedback),
			Reviewer: stringPtr(f.priorReview.reviewer),
			At:       stringPtr(f.priorReview.at),
		}}
	}

	payload, err := json.Marshal(request)
	if err != nil {
		return err
	}

	bomTotal := 0
	for _, line := range f.bom {
		bomTotal += line.qty * line.cents
	}

	tx := []storage.TransactionStatement{
		{SQL: `insert into submission (id, program_id, external_id, version, status, stage, priority, title, description, track, theme, phase, week, maker_email, maker_name, repo_url, thumbnail_url, requested_tier, grant_cap_cents, bom_total_cents, hours_claimed_seconds, hours_journal_seconds, hours_hackatime_seconds, check_summary, ingested_at, created_at, updated_at)
			values (?, ?, ?, 1, ?, 1, ?, ?, ?, 'hardware', ?, 'design', 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?)`,
			Args: []any{id, programID, f.externalID, status, f.priority, f.title, f.description, f.theme, f.makerEmail, f.makerName, f.repoURL, f.thumbnailURL, f.tier, 3000, bomTotal, f.hoursClaimed, f.hoursJournal, string(summaryJSON), ingested.Format(timestampFormat), ingested.Format(timestampFormat), ingested.Format(timestampFormat)}},
		{SQL: `insert into submission_version (submission_id, version, payload, payload_sha256, received_at, delivery_id)
			values (?, 1, ?, ?, ?, ?)`,
			Args: []any{id, string(payload), sha256Hex(payload), ingested.Format(timestampFormat), "seed:" + id}},
		{SQL: `insert into submission_event (id, submission_id, kind, data, created_at) values (?, ?, 'ingested', ?, ?)`,
			Args: []any{newID("evt"), id, `{"source":"fixture"}`, ingested.Format(timestampFormat)}},
		{SQL: `insert into submission_event (id, submission_id, kind, data, created_at) values (?, ?, 'checks_ready', ?, ?)`,
			Args: []any{newID("evt"), id, string(summaryJSON), ingested.Add(time.Minute).Format(timestampFormat)}},
	}

	for i, entry := range f.journals {
		media := make([]map[string]string, entry.media)
		for j := range media {
			media[j] = map[string]string{"type": "image", "url": "https://example.com/journal.jpg"}
		}

		timelapses := make([]map[string]any, entry.clips)
		for j := range timelapses {
			timelapses[j] = map[string]any{"provider": "lapse", "externalId": "clip_" + id, "coveredSeconds": entry.coverS / max(entry.clips, 1)}
		}

		mediaJSON, _ := json.Marshal(media)
		clipsJSON, _ := json.Marshal(timelapses)

		tx = append(tx, storage.TransactionStatement{
			SQL:  `insert into journal_entry (id, submission_id, at, seconds, title, text, media, timelapses, sort) values (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			Args: []any{newID("jrn"), id, entry.at, entry.hours * 3600, entry.title, entry.text, string(mediaJSON), string(clipsJSON), i},
		})
	}

	for i, line := range f.bom {
		tx = append(tx, storage.TransactionStatement{
			SQL:  `insert into bom_line (id, submission_id, name, purpose, quantity, unit_cost_cents, vendor, link, sort) values (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			Args: []any{newID("bom"), id, line.name, line.purpose, line.qty, line.cents, line.vendor, line.link, i},
		})
	}

	for _, check := range f.checks {
		tx = append(tx, storage.TransactionStatement{
			SQL: `insert into check_result (id, submission_id, version, check_key, label, group_key, status, severity, summary, detail, checked_at)
				values (?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?)`,
			Args: []any{newID("chk"), id, check.key, check.label, check.group, check.status, check.severity, check.summary, check.detail, ingested.Add(time.Minute).Format(timestampFormat)},
		})
	}

	_, err = a.store.Transaction(ctx, tx...)

	return err
}

// stringPtr returns a pointer to value. It keeps the fixture literals readable.
func stringPtr(value string) *string {
	if value == "" {
		return nil
	}

	return &value
}

// trimSpace removes leading and trailing whitespace from a pointer to a string, leaving nil alone.
func trimSpace(value *string) *string {
	if value == nil {
		return nil
	}

	trimmed := strings.TrimSpace(*value)

	return &trimmed
}
