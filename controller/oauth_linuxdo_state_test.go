package controller

import "testing"

func TestLinuxDOStateOriginMatchesRequest(t *testing.T) {
	t.Setenv("LINUXDO_OAUTH_ALLOWED_ORIGINS", "https://elysiver.h-e.top,https://elysia.h-e.top")

	tests := []struct {
		name          string
		origin        string
		currentOrigin string
		want          bool
	}{
		{
			name:          "exact HTTPS origin",
			origin:        "https://elysiver.h-e.top",
			currentOrigin: "https://elysiver.h-e.top",
			want:          true,
		},
		{
			name:          "allowlisted HTTPS origin terminated to HTTP",
			origin:        "https://elysiver.h-e.top",
			currentOrigin: "http://elysiver.h-e.top",
			want:          true,
		},
		{
			name:          "different host is rejected",
			origin:        "https://elysiver.h-e.top",
			currentOrigin: "http://attacker.example",
			want:          false,
		},
		{
			name:          "non allowlisted host is rejected",
			origin:        "https://unlisted.example",
			currentOrigin: "http://unlisted.example",
			want:          false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := linuxDOStateOriginMatchesRequest(tt.origin, tt.currentOrigin); got != tt.want {
				t.Fatalf("linuxDOStateOriginMatchesRequest(%q, %q) = %t, want %t", tt.origin, tt.currentOrigin, got, tt.want)
			}
		})
	}
}
