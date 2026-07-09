package usagelogs

import "testing"

func TestPrimaryWeeklyQuotaKeysForProvider(t *testing.T) {
	t.Parallel()

	cases := []struct {
		provider string
		want     []string
	}{
		{provider: "xai", want: []string{"weekly_limit"}},
		{provider: "Grok", want: []string{"weekly_limit"}},
		{provider: "codex", want: []string{"code_week"}},
		{provider: "kimi", want: []string{"code_week"}},
		{provider: "claude", want: []string{"seven_day"}},
		{provider: "unknown", want: nil},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.provider, func(t *testing.T) {
			t.Parallel()
			got := primaryWeeklyQuotaKeysForProvider(tc.provider)
			if len(got) != len(tc.want) {
				t.Fatalf("keys len = %d, want %d (%v vs %v)", len(got), len(tc.want), got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("keys[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}
