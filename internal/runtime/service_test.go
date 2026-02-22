package runtime

import "testing"

func TestTelegramSendSucceeded(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		expect bool
	}{
		{
			name:   "single json ok true",
			input:  `{"message_id": 1, "ok": true}`,
			expect: true,
		},
		{
			name: "multiple json documents second has ok true",
			input: `{
  "limit": 8,
  "query": "hello",
  "results": []
}
{
  "message_id": 121,
  "ok": true
}`,
			expect: true,
		},
		{
			name: "mixed output with embedded ok true json",
			input: `debug: command started
{"ok": true, "message_id": 122}`,
			expect: true,
		},
		{
			name:   "single json ok false",
			input:  `{"ok": false}`,
			expect: false,
		},
		{
			name:   "empty output",
			input:  ``,
			expect: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := telegramSendSucceeded(tc.input)
			if got != tc.expect {
				t.Fatalf("telegramSendSucceeded() = %v, want %v", got, tc.expect)
			}
		})
	}
}
