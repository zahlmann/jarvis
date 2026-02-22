package config

import (
	"testing"

	"github.com/zahlmann/phi/agent"
)

func TestParseThinkingLevel(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want agent.ThinkingLevel
	}{
		{name: "none", raw: "none", want: agent.ThinkingNone},
		{name: "minimal", raw: "minimal", want: agent.ThinkingMinimal},
		{name: "low", raw: "low", want: agent.ThinkingLow},
		{name: "medium", raw: "medium", want: agent.ThinkingMedium},
		{name: "high", raw: "high", want: agent.ThinkingHigh},
		{name: "xhigh", raw: "xhigh", want: agent.ThinkingXHigh},
		{name: "unknown defaults xhigh", raw: "bogus", want: agent.ThinkingXHigh},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseThinkingLevel(tc.raw); got != tc.want {
				t.Fatalf("parseThinkingLevel(%q): got=%q want=%q", tc.raw, got, tc.want)
			}
		})
	}
}
