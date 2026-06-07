package app

import (
	"testing"

	"github.com/nourabuild/relays-api/internal/sdk/models"
)

func TestValidateCreateTaskBatchRequiresAssignments(t *testing.T) {
	t.Parallel()

	details := validateCreateTaskBatch(models.CreateTaskBatch{Title: "Review invoice"})

	if details["assignments"] != "at least one assignee is required" {
		t.Fatalf("expected assignments validation error, got %q", details["assignments"])
	}
}
