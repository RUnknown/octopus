package model

import (
	"testing"
	"time"
)

func TestGetChannelKeyPrefersPreferredKeyID(t *testing.T) {
	channel := &Channel{
		Keys: []ChannelKey{
			{ID: 1, Enabled: true, ChannelKey: "first", TotalCost: 1},
			{ID: 2, Enabled: true, ChannelKey: "preferred", TotalCost: 100},
		},
	}

	selected := channel.GetChannelKey(ChannelKeySelectOptions{PreferredKeyID: 2})
	if selected.ID != 2 {
		t.Fatalf("expected preferred key 2, got %d", selected.ID)
	}
}

func TestGetChannelKeyUsesPreferredKeyAfterRecent429(t *testing.T) {
	channel := &Channel{
		Keys: []ChannelKey{
			{ID: 1, Enabled: true, ChannelKey: "fallback", TotalCost: 1},
			{ID: 2, Enabled: true, ChannelKey: "preferred", TotalCost: 100, StatusCode: 429, LastUseTimeStamp: time.Now().Unix()},
		},
	}

	selected := channel.GetChannelKey(ChannelKeySelectOptions{PreferredKeyID: 2})
	if selected.ID != 2 {
		t.Fatalf("expected preferred key 2 despite recent 429, got %d", selected.ID)
	}
}

func TestGetChannelKeyUsesLowestCostKeyAfterRecent429(t *testing.T) {
	channel := &Channel{
		Keys: []ChannelKey{
			{ID: 1, Enabled: true, ChannelKey: "recent-429", TotalCost: 1, StatusCode: 429, LastUseTimeStamp: time.Now().Unix()},
			{ID: 2, Enabled: true, ChannelKey: "other", TotalCost: 100},
		},
	}

	selected := channel.GetChannelKey()
	if selected.ID != 1 {
		t.Fatalf("expected lowest cost key 1 despite recent 429, got %d", selected.ID)
	}
}

func TestValidateChannelBaseURLs(t *testing.T) {
	tests := []struct {
		name    string
		items   []BaseUrl
		wantErr bool
	}{
		{name: "valid", items: []BaseUrl{{URL: "https://example.com/v1"}, {URL: "http://127.0.0.1:8080", Delay: 10}}},
		{name: "missing", items: nil, wantErr: true},
		{name: "empty", items: []BaseUrl{{URL: ""}}, wantErr: true},
		{name: "unsupported scheme", items: []BaseUrl{{URL: "ftp://example.com"}}, wantErr: true},
		{name: "missing host", items: []BaseUrl{{URL: "https:///v1"}}, wantErr: true},
		{name: "duplicated scheme", items: []BaseUrl{{URL: "https://https://example.com"}}, wantErr: true},
		{name: "negative delay", items: []BaseUrl{{URL: "https://example.com", Delay: -1}}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateChannelBaseURLs(tc.items)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidateChannelBaseURLs() error = %v, wantErr %t", err, tc.wantErr)
			}
		})
	}
}
