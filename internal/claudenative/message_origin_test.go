package claudenative

import (
	"encoding/json"
	"testing"
)

func TestNativeMessageOriginDoesNotAttributeBackgroundEventsToUser(t *testing.T) {
	for _, tc := range []struct {
		name, source, text string
		meta, compact      bool
		role, kind         string
	}{
		{"background", "system", "<task-notification>done</task-notification>", false, false, "system", "background_notification"},
		{"system plain", "system", "Background check completed", false, false, "system", "native_context"},
		{"meta", "", "Injected context", true, false, "system", "native_context"},
		{"compaction", "", "Summary of earlier conversation", false, true, "system", "compaction"},
		{"legacy background", "", "<task-notification>done</task-notification>", false, false, "system", "background_notification"},
		{"typed quotation", "typed", "<task-notification>please explain this</task-notification>", false, false, "user", "message"},
		{"queued human", "queued", "Check the monitoring service", false, false, "user", "message"},
		{"pasted human", "", "<pasted_content>monitoring logs</pasted_content>", false, false, "user", "message"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b, _ := json.Marshal(map[string]any{"type": "user", "uuid": "origin-test", "promptSource": tc.source, "isMeta": tc.meta, "isCompactSummary": tc.compact, "message": map[string]any{"content": tc.text}})
			m, ok := parseMessage(b)
			if !ok || m.Role != tc.role || m.Kind != tc.kind || m.Text != tc.text {
				t.Fatalf("unexpected classification: %+v", m)
			}
		})
	}
}

func TestScheduledTaskHasExplicitDisplayOrigin(t *testing.T) {
	m, ok := parseMessage([]byte(`{"type":"user","uuid":"scheduled","promptSource":"system","isMeta":true,"scheduledTaskId":"task-1","message":{"content":"Run periodic check"}}`))
	if !ok || m.Role != "system" || m.Kind != "scheduled_trigger" {
		t.Fatalf("unexpected scheduled origin: %+v", m)
	}
}
