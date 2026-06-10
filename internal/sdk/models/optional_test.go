package models

import (
	"encoding/json"
	"testing"
)

func TestUpdateTaskDistinguishesAbsentNullAndValue(t *testing.T) {
	t.Parallel()

	var input UpdateTask
	payload := `{"title":"new title","description":null,"due_at":"2026-07-01T12:00:00Z"}`
	if err := json.Unmarshal([]byte(payload), &input); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Present with value.
	if !input.DueAt.Set || input.DueAt.Value == nil {
		t.Errorf("due_at should be set with a value: %+v", input.DueAt)
	}

	// Present as explicit null: clear.
	if !input.Description.Set || input.Description.Value != nil {
		t.Errorf("description should be set to null: %+v", input.Description)
	}

	// Absent: untouched.
	if input.DelegatedFromTaskID.Set {
		t.Errorf("delegated_from_task_id should be absent: %+v", input.DelegatedFromTaskID)
	}
}
