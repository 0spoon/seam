package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerifyMCPAPIKey(t *testing.T) {
	t.Parallel()

	const apiKey = "secret-key-1234567890"

	cases := []struct {
		name   string
		header string
		key    string
		want   bool
	}{
		{name: "empty apiKey rejects everything", header: "Bearer " + apiKey, key: "", want: false},
		{name: "missing header", header: "", key: apiKey, want: false},
		{name: "non-bearer scheme", header: "Basic abc", key: apiKey, want: false},
		{name: "bearer with no token", header: "Bearer", key: apiKey, want: false},
		{name: "wrong key", header: "Bearer wrong", key: apiKey, want: false},
		{name: "correct key lowercase scheme", header: "bearer " + apiKey, key: apiKey, want: true},
		{name: "correct key mixed case scheme", header: "BeArEr " + apiKey, key: apiKey, want: true},
		{name: "correct key", header: "Bearer " + apiKey, key: apiKey, want: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(http.MethodPost, "/", nil)
			if tc.header != "" {
				r.Header.Set("Authorization", tc.header)
			}
			if got := VerifyMCPAPIKey(r, tc.key); got != tc.want {
				t.Fatalf("VerifyMCPAPIKey: got %v, want %v", got, tc.want)
			}
		})
	}
}
