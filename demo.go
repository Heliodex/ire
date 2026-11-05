package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"ire/contract"
)

const (
	demoTimeout = 10 * time.Second
	maxDemoBody = 64 << 10
)

// demoChecks probes a public HTTPS demo without letting it reach loopback or private networks.
func (a *ireApp) demoChecks(ctx context.Context, ship submissionRow) []contract.CheckResult {
	checkedAt := a.nowText()

	if strings.TrimSpace(ship.DemoURL) == "" {
		return []contract.CheckResult{check("demo.probe", "Demo reachable", "demo", statusSkip, severityInfo, "No demo URL", "The ship carries no demo URL to probe.", checkedAt)}
	}

	target, err := safeDemoURL(ctx, ship.DemoURL)
	if err != nil {
		return []contract.CheckResult{check("demo.probe", "Demo reachable", "demo", statusError, severityWarn, "Demo URL could not be probed", err.Error(), checkedAt)}
	}

	client := *a.client
	client.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		if _, err := safeDemoURL(req.Context(), req.URL.String()); err != nil {
			return err
		}

		return nil
	}

	requestCtx, cancel := context.WithTimeout(ctx, demoTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, target.String(), nil)
	if err != nil {
		return []contract.CheckResult{check("demo.probe", "Demo reachable", "demo", statusError, severityWarn, "Demo URL could not be probed", err.Error(), checkedAt)}
	}

	req.Header.Set("Accept", "text/html")
	req.Header.Set("User-Agent", "half-life-ire")

	resp, err := client.Do(req)
	if err != nil {
		return []contract.CheckResult{check("demo.probe", "Demo reachable", "demo", statusWarn, severityWarn, "Demo did not respond", "Open the demo by hand; the probe could not reach it.", checkedAt)}
	}
	defer resp.Body.Close()

	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxDemoBody))

	return []contract.CheckResult{demoResult(resp.StatusCode, checkedAt)}
}

// demoResult maps a probe status to the reviewer-facing check.
func demoResult(code int, checkedAt string) contract.CheckResult {
	status, severity, summary := statusWarn, severityWarn, fmt.Sprintf("Demo returned HTTP %d", code)

	switch {
	case code >= 200 && code < 400:
		status, severity, summary = statusPass, severityInfo, fmt.Sprintf("Demo responded %d", code)
	case code == http.StatusUnauthorized || code == http.StatusForbidden || code == http.StatusTooManyRequests:
		summary = fmt.Sprintf("Demo is alive but bot-walled (HTTP %d)", code)
	case code == http.StatusNotFound:
		summary = "Demo returned 404"
	}

	result := check("demo.probe", "Demo reachable", "demo", status, severity, summary, "Hardware projects may omit a demo; a warning asks the reviewer to open it.", checkedAt)
	result.Source = stringPtr("probe")

	return result
}

// safeDemoURL accepts only HTTPS hosts outside loopback, private and link-local ranges.
func safeDemoURL(ctx context.Context, raw string) (*url.URL, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return nil, errors.New("use an HTTPS URL")
	}

	host := strings.ToLower(parsed.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") {
		return nil, errors.New("do not probe local hosts")
	}

	if ip := net.ParseIP(host); ip != nil {
		if privateIP(ip) {
			return nil, errors.New("do not probe private addresses")
		}

		return parsed, nil
	}

	lookupCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	addresses, err := net.DefaultResolver.LookupIPAddr(lookupCtx, host)
	if err != nil {
		return parsed, nil
	}

	for _, address := range addresses {
		if privateIP(address.IP) {
			return nil, errors.New("do not probe hosts that resolve to private addresses")
		}
	}

	return parsed, nil
}

// privateIP reports addresses a browser on the public internet could not reach.
func privateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}
