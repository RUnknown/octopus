package relay

import (
	"net/http"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
)

func TestIsOfficialCodexHeaders(t *testing.T) {
	tests := []struct {
		name    string
		headers http.Header
		want    bool
	}{
		{name: "originator", headers: http.Header{"Originator": {"codex-tui"}}, want: true},
		{name: "user agent", headers: http.Header{"User-Agent": {"codex-tui/0.147.0 (Mac OS; arm64)"}}, want: true},
		{name: "generic client", headers: http.Header{"User-Agent": {"openai-node/6"}}, want: false},
		{name: "near match", headers: http.Header{"Originator": {"third-party-codex"}}, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isOfficialCodexHeaders(tc.headers); got != tc.want {
				t.Fatalf("isOfficialCodexHeaders() = %t, want %t", got, tc.want)
			}
		})
	}
}

func TestMapFinalCodexStatus(t *testing.T) {
	if got := mapFinalCodexStatus(http.StatusTooManyRequests, true); got != http.StatusServiceUnavailable {
		t.Fatalf("mapped status = %d, want %d", got, http.StatusServiceUnavailable)
	}
	if got := mapFinalCodexStatus(http.StatusTooManyRequests, false); got != http.StatusTooManyRequests {
		t.Fatalf("disabled mapping changed status to %d", got)
	}
	if got := mapFinalCodexStatus(http.StatusBadGateway, true); got != http.StatusBadGateway {
		t.Fatalf("non-429 status changed to %d", got)
	}
}

func TestDownstreamWSHeadersReachUpstreamPoolKey(t *testing.T) {
	first := http.Header{
		"Session-Id":         {"session-parent"},
		"Thread-Id":          {"thread-parent"},
		"X-Codex-Turn-State": {"turn-parent"},
		"Cookie":             {"local-session=secret"},
		"Origin":             {"http://localhost:8080"},
	}
	second := first.Clone()
	second.Set("Thread-Id", "thread-subagent")

	req := &relayRequest{clientHeaders: first}
	attempt := &relayAttempt{relayRequest: req}
	if got := attempt.clientRequestHeaders().Get("X-Codex-Turn-State"); got != "turn-parent" {
		t.Fatalf("turn state header = %q, want turn-parent", got)
	}

	channel := &dbmodel.Channel{ID: 7}
	firstHeaders := buildUpstreamWSHeaders(first, channel, "secret")
	secondHeaders := buildUpstreamWSHeaders(second, channel, "secret")
	if firstHeaders.Get("Session-Id") != "session-parent" || firstHeaders.Get("Thread-Id") != "thread-parent" {
		t.Fatalf("session identity headers were not forwarded: %v", firstHeaders)
	}
	if firstHeaders.Get("Cookie") != "" || firstHeaders.Get("Origin") != "" {
		t.Fatalf("downstream browser session headers leaked upstream: %v", firstHeaders)
	}
	if newWSPoolKey(7, 11, firstHeaders) == newWSPoolKey(7, 11, secondHeaders) {
		t.Fatal("different Codex threads produced the same upstream pool key")
	}
}
