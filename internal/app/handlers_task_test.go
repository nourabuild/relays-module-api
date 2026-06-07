package app

import (
	"testing"

	"github.com/nourabuild/relays-api/internal/sdk/models"
)

func TestValidateCreateTaskRejectsSelfAssignment(t *testing.T) {
	t.Parallel()

	details := validateCreateTask(models.CreateTask{
		AssignedToID: "28",
		Title:        "Review invoice",
	}, "28")

	if details["assigned_to_id"] != "assigned_to_id cannot be the creator" {
		t.Fatalf("expected self-assignment validation error, got %q", details["assigned_to_id"])
	}
}
