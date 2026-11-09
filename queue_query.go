package main

import (
	"regexp"
	"strconv"
	"strings"
)

type queueCompare struct {
	op    string
	value int64
}

type queueChecksFilter struct {
	overallPass bool
	count       string
	op          string
	value       int64
}

type queueCheckFilter struct {
	key    string
	status string
}

type queueSearch struct {
	raw       string
	supported bool
	age       *queueCompare
	check     *queueCheckFilter
	checks    *queueChecksFilter
	journals  *queueCompare
	status    string
	text      []string
	time      *queueCompare
	kind      string
	version   *queueCompare
	week      *queueCompare
}

var queueDurationPart = regexp.MustCompile(`(\d+(?:\.\d+)?)([smhdw])`)

var queueDurationUnits = map[string]int64{
	"s": 1000,
	"m": 60 * 1000,
	"h": 60 * 60 * 1000,
	"d": 24 * 60 * 60 * 1000,
	"w": 7 * 24 * 60 * 60 * 1000,
}

var queueDurationRank = map[string]int{
	"s": 1,
	"m": 2,
	"h": 3,
	"d": 4,
	"w": 5,
}

var queueKnownStatuses = map[string]bool{
	"checking": true, "claimed": true, "decided": true, "in_review": true,
	"needs_attention": true, "ready": true, "stage2_hold": true, "withdrawn": true,
}

func parseQueueSearch(raw string) queueSearch {
	query := queueSearch{raw: strings.TrimSpace(raw), supported: true}
	if query.raw == "" {
		return query
	}

	for _, token := range strings.Fields(query.raw) {
		separator := strings.Index(token, ":")
		if separator == -1 {
			query.text = append(query.text, strings.ToLower(token))
			continue
		}

		key := strings.ToLower(token[:separator])
		value := strings.TrimSpace(token[separator+1:])

		switch key {
		case "journals":
			if parsed := parseQueueComparison(value); parsed != nil {
				query.journals = parsed
			} else {
				query.text = append(query.text, strings.ToLower(token))
			}
		case "status":
			status := normalizeQueueStatus(value)
			if queueKnownStatuses[status] {
				query.status = status
			} else {
				query.text = append(query.text, strings.ToLower(token))
			}
		case "time":
			if parsed := parseQueueDuration(value, ">="); parsed != nil {
				query.time = parsed
			} else {
				query.text = append(query.text, strings.ToLower(token))
			}
		case "type":
			if value != "" {
				query.kind = strings.ToLower(value)
			} else {
				query.text = append(query.text, strings.ToLower(token))
			}
		case "ver":
			if parsed := parseQueueComparison(value); parsed != nil {
				query.version = parsed
			} else {
				query.text = append(query.text, strings.ToLower(token))
			}
		case "week":
			if parsed := parseQueueComparison(value); parsed != nil {
				query.week = parsed
			} else {
				query.text = append(query.text, strings.ToLower(token))
			}
		case "age":
			if parsed := parseQueueDuration(value, ">="); parsed != nil && queueAgeOp(parsed.op) != "" {
				query.age = parsed
			} else if parsed := parseQueueDuration(value, ">="); parsed != nil {
				query.supported = false
			} else {
				query.text = append(query.text, strings.ToLower(token))
			}
		case "checks":
			if parsed := parseQueueChecks(value); parsed != nil {
				query.checks = parsed
			} else {
				query.text = append(query.text, strings.ToLower(token))
			}
		case "check":
			if separator := strings.Index(value, "="); separator != -1 {
				query.check = &queueCheckFilter{
					key:    strings.ToLower(value[:separator]),
					status: strings.ToLower(value[separator+1:]),
				}
			} else {
				query.text = append(query.text, strings.ToLower(token))
			}
		default:
			query.text = append(query.text, strings.ToLower(token))
		}
	}

	return query
}

func normalizeQueueStatus(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, " ", "_")
	value = strings.ReplaceAll(value, "-", "_")
	return value
}

func parseQueueComparison(value string) *queueCompare {
	op := "="
	body := value
	for _, prefix := range []string{">=", "<=", "!=", ">", "<", "="} {
		if strings.HasPrefix(value, prefix) {
			op = prefix
			body = value[len(prefix):]
			break
		}
	}

	if body == "" || body != strings.TrimSpace(body) {
		return nil
	}

	number, err := strconv.ParseInt(body, 10, 64)
	if err != nil {
		return nil
	}

	return &queueCompare{op: op, value: number}
}

var queueCheckCounts = map[string]bool{
	"error": true, "fail": true, "pass": true, "running": true, "total": true, "warn": true,
}

var queueChecksRE = regexp.MustCompile(`^(error|fail|pass|running|total|warn)?(>=|<=|!=|>|<|=)?(\d+)$`)

func parseQueueChecks(value string) *queueChecksFilter {
	lower := strings.ToLower(value)
	if lower == "pass" {
		return &queueChecksFilter{overallPass: true}
	}

	if queueCheckCounts[lower] {
		return &queueChecksFilter{count: lower, op: ">", value: 0}
	}

	match := queueChecksRE.FindStringSubmatch(lower)
	if match == nil {
		return nil
	}

	count := match[1]
	if count == "" {
		count = "total"
	}
	if !queueCheckCounts[count] {
		return nil
	}
	if count == "pass" && match[2] == "" && match[3] == "" {
		return &queueChecksFilter{overallPass: true}
	}

	op := match[2]
	if op == "" {
		op = "="
	}
	number, err := strconv.ParseInt(match[3], 10, 64)
	if err != nil {
		return nil
	}

	return &queueChecksFilter{count: count, op: op, value: number}
}

func parseQueueDuration(value, fallback string) *queueCompare {
	op := fallback
	body := strings.ToLower(value)
	for _, prefix := range []string{">=", "<=", "!=", ">", "<", "="} {
		if strings.HasPrefix(body, prefix) {
			op = prefix
			body = body[len(prefix):]
			break
		}
	}

	matches := queueDurationPart.FindAllStringSubmatchIndex(body, -1)
	if len(matches) == 0 {
		return nil
	}

	joined := ""
	var total float64
	previous := 99
	for _, match := range queueDurationPart.FindAllStringSubmatch(body, -1) {
		joined += match[0]
		rank := queueDurationRank[match[2]]
		if rank == 0 || rank >= previous {
			return nil
		}
		previous = rank
		amount, err := strconv.ParseFloat(match[1], 64)
		if err != nil {
			return nil
		}
		total += amount * float64(queueDurationUnits[match[2]])
	}

	if joined != body {
		return nil
	}

	return &queueCompare{op: op, value: int64(total)}
}

func (q queueSearch) active() bool {
	return q.raw != "" && q.supported
}

func queueChecksExpr(count string) string {
	field := func(name string) string {
		return "coalesce(json_extract(s.check_summary, '$." + name + "'), 0)"
	}
	switch count {
	case "error", "fail", "pass", "warn":
		return field(count)
	case "running":
		return "(" + field("running") + " + case when s.status = 'checking' and " + field("running") + " = 0 then 1 else 0 end)"
	case "total":
		return "(" + field("pass") + " + " + field("warn") + " + " + field("fail") + " + " + field("error") + " + " + field("running") + ")"
	default:
		return ""
	}
}

// queueAgeOp maps an age comparison (age = now - created_at/submitted) to a created_at comparison.
func queueAgeOp(op string) string {
	switch op {
	case ">":
		return "<"
	case ">=":
		return "<="
	case "<":
		return ">"
	case "<=":
		return ">="
	default:
		return ""
	}
}

func (q queueSearch) apply(query string, args []any) (string, []any) {
	if !q.active() {
		return query, args
	}

	if q.age != nil {
		if op := queueAgeOp(q.age.op); op != "" {
			seconds := q.age.value / 1000
			query += " and s.created_at " + op + " datetime('now', ?)"
			args = append(args, "-"+strconv.FormatInt(seconds, 10)+" seconds")
		}
	}

	if q.check != nil {
		query += ` and exists (select 1 from check_result c
			where c.submission_id = s.id and c.version = s.version
			  and lower(c.check_key) = ? and lower(c.status) = ?
			  and c.checked_at = (select max(c2.checked_at) from check_result c2
				where c2.submission_id = c.submission_id and c2.version = c.version and c2.check_key = c.check_key))`
		args = append(args, q.check.key, q.check.status)
	}

	if q.checks != nil {
		if q.checks.overallPass {
			query += " and coalesce(json_extract(s.check_summary, '$.overall'), '') = 'pass'"
		} else {
			expr := queueChecksExpr(q.checks.count)
			if expr != "" {
				query += " and " + expr + " " + q.checks.op + " ?"
				args = append(args, q.checks.value)
			}
		}
	}

	if q.journals != nil {
		query += " and (select count(*) from journal_entry je where je.submission_id = s.id) " + q.journals.op + " ?"
		args = append(args, q.journals.value)
	}

	if q.status != "" {
		query += " and s.status = ?"
		args = append(args, q.status)
	}

	if q.time != nil {
		query += " and (s.hours_claimed_seconds * 1000) " + q.time.op + " ?"
		args = append(args, q.time.value)
	}

	if q.kind != "" {
		query += " and lower(trim(coalesce(s.theme, ''))) = ?"
		args = append(args, q.kind)
	}

	if q.version != nil {
		query += " and s.version " + q.version.op + " ?"
		args = append(args, q.version.value)
	}

	if q.week != nil {
		query += " and s.week is not null and s.week " + q.week.op + " ?"
		args = append(args, q.week.value)
	}

	for _, term := range q.text {
		like := "%" + escapeLike(strings.ToLower(term)) + "%"
		query += " and (lower(s.title) like ? escape '\\' or lower(coalesce(s.maker_name, '')) like ? escape '\\' or lower(s.status) like ? escape '\\' or lower(replace(s.status, '_', ' ')) like ? escape '\\' or lower(coalesce(s.theme, '')) like ? escape '\\')"
		args = append(args, like, like, like, like, like)
	}

	return query, args
}

func escapeLike(value string) string {
	value = strings.ReplaceAll(value, `\`, `\\`)
	value = strings.ReplaceAll(value, `%`, `\%`)
	value = strings.ReplaceAll(value, `_`, `\_`)
	return value
}


