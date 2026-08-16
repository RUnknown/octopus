package relay

import (
	"net/http"
	"strings"

	dbmodel "github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
)

func isOfficialCodexHeaders(headers http.Header) bool {
	if headers == nil {
		return false
	}
	originator := strings.ToLower(strings.TrimSpace(headers.Get("Originator")))
	if originator == "codex-tui" {
		return true
	}
	userAgent := strings.ToLower(strings.TrimSpace(headers.Get("User-Agent")))
	return strings.Contains(userAgent, "codex-tui/")
}

func shouldMapFinalCodex429(headers http.Header) bool {
	if !isOfficialCodexHeaders(headers) {
		return false
	}
	enabled, err := op.SettingGetBool(dbmodel.SettingKeyCodexMap429To503)
	return err == nil && enabled
}

func mapFinalCodexStatus(statusCode int, enabled bool) int {
	if enabled && statusCode == http.StatusTooManyRequests {
		return http.StatusServiceUnavailable
	}
	return statusCode
}
