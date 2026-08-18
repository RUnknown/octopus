package relay

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	dbmodel "github.com/bestruirui/octopus/internal/model"
)

func TestRunSub2APIBalanceTest(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", request.Method)
		}
		if request.URL.Path != "/proxy/v1/usage" {
			t.Errorf("path = %q, want /proxy/v1/usage", request.URL.Path)
		}
		if got := request.Header.Get("Authorization"); got != "Bearer balance-key" {
			t.Errorf("authorization = %q", got)
		}
		if got := request.Header.Get("X-Test-Header"); got != "present" {
			t.Errorf("custom header = %q", got)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{
			"mode":"quota_limited",
			"isValid":true,
			"planName":"API quota",
			"remaining":8.5,
			"unit":"USD",
			"quota":{"limit":10,"used":1.5,"remaining":8.5,"unit":"USD"}
		}`))
	}))
	defer upstream.Close()

	channel := &dbmodel.Channel{
		ID:           9,
		BaseUrls:     []dbmodel.BaseUrl{{URL: upstream.URL + "/proxy/v1"}},
		Keys:         []dbmodel.ChannelKey{{ID: 12, Enabled: true, ChannelKey: "balance-key"}},
		CustomHeader: []dbmodel.CustomHeader{{HeaderKey: "X-Test-Header", HeaderValue: "present"}},
	}
	result, key, err := RunSub2APIBalanceTest(context.Background(), channel, Sub2APIBalanceTestRequest{
		ChannelID: 9,
		KeyID:     12,
	})
	if err != nil {
		t.Fatalf("RunSub2APIBalanceTest() error = %v", err)
	}
	if result.StatusCode != http.StatusOK || result.KeyID != 12 || key.ID != 12 {
		t.Fatalf("unexpected result=%+v key=%+v", result, key)
	}
	if result.Remaining != 8.5 || result.Unit != "USD" || result.Mode != "quota_limited" || result.PlanName != "API quota" {
		t.Fatalf("unexpected normalized result: %+v", result)
	}
	if result.Limit == nil || *result.Limit != 10 || result.Used == nil || *result.Used != 1.5 {
		t.Fatalf("unexpected quota values: %+v", result)
	}
	if result.IsValid == nil || !*result.IsValid {
		t.Fatalf("unexpected validity: %+v", result.IsValid)
	}
}

func TestParseSub2APIBalanceResponseFallbacks(t *testing.T) {
	tests := []struct {
		name      string
		body      string
		remaining float64
		unit      string
	}{
		{
			name:      "nested quota strings",
			body:      `{"quota":{"remaining":"7.25","unit":"CNY","limit":"9.5","used":"2.25"}}`,
			remaining: 7.25,
			unit:      "CNY",
		},
		{
			name:      "wallet balance",
			body:      `{"balance":12.75}`,
			remaining: 12.75,
			unit:      "USD",
		},
		{
			name:      "legacy total available",
			body:      `{"total_available":3,"total_granted":5}`,
			remaining: 3,
			unit:      "USD",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := &Sub2APIBalanceTestResult{}
			if err := parseSub2APIBalanceResponse([]byte(tc.body), result); err != nil {
				t.Fatalf("parseSub2APIBalanceResponse() error = %v", err)
			}
			if result.Remaining != tc.remaining || result.Unit != tc.unit {
				t.Fatalf("result = %+v, want remaining=%v unit=%s", result, tc.remaining, tc.unit)
			}
		})
	}
}

func TestParseSub2APIBalanceResponseRejectsMissingBalance(t *testing.T) {
	result := &Sub2APIBalanceTestResult{}
	err := parseSub2APIBalanceResponse([]byte(`{"mode":"unrestricted"}`), result)
	if err == nil || !strings.Contains(err.Error(), "missing remaining balance") {
		t.Fatalf("expected missing balance error, got %v", err)
	}
}

func TestBuildSub2APIUsageURL(t *testing.T) {
	tests := map[string]string{
		"https://example.com":              "https://example.com/v1/usage",
		"https://example.com/":             "https://example.com/v1/usage",
		"https://example.com/v1":           "https://example.com/v1/usage",
		"https://example.com/prefix/v1/":   "https://example.com/prefix/v1/usage",
		"https://example.com/proxy?x=test": "https://example.com/proxy/v1/usage",
	}
	for input, expected := range tests {
		got, err := buildSub2APIUsageURL(input)
		if err != nil {
			t.Fatalf("buildSub2APIUsageURL(%q) error = %v", input, err)
		}
		if got != expected {
			t.Fatalf("buildSub2APIUsageURL(%q) = %q, want %q", input, got, expected)
		}
	}
}
