package usage

import "testing"

func TestExtractSessionIDFromDetailsRecognizesXSessionID(t *testing.T) {
	detail := `{"client":{"headers":{"X-Session-Id":["zcode-session"],"Conversation-Id":["conversation"]}}}`
	if got := extractSessionIDFromDetails(detail); got != "zcode-session" {
		t.Fatalf("session_id = %q, want zcode-session", got)
	}
}
