package app

import (
	"testing"

	"github.com/nourabuild/relays-api/internal/sdk/models"
)

func TestValidateCreateTaskBatchRequiresAssignees(t *testing.T) {
	t.Parallel()

	details := validateCreateTaskBatch(models.CreateTaskBatch{Title: "Review invoice"}, "28")

	if details["assignees"] != "at least one assignee is required" {
		t.Fatalf("expected assignees validation error, got %q", details["assignees"])
	}
}
