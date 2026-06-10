package sqldb

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nourabuild/relays-api/internal/sdk/models"
)

var userSeq atomic.Int64

// seedUser inserts a user with unique account/email so tests sharing the
// container cannot collide.
func seedUser(t *testing.T, srv Service, label string) models.User {
	t.Helper()

	n := userSeq.Add(1)
	user, err := srv.CreateUser(context.Background(), models.NewUser{
		ID:      fmt.Sprintf("%s-%d", label, n),
		Name:    label,
		Account: fmt.Sprintf("%s%d", label, n),
		Email:   fmt.Sprintf("%s%d@test.example", label, n),
	})
	if err != nil {
		t.Fatalf("seeding user %s: %v", label, err)
	}
	return user
}

func seedTask(t *testing.T, srv Service, creatorID, assigneeID, title string) models.Task {
	t.Helper()

	task, err := srv.CreateTask(context.Background(), creatorID, models.CreateTask{
		AssignedToID: assigneeID,
		Title:        title,
	})
	if err != nil {
		t.Fatalf("seeding task: %v", err)
	}
	return task
}

func TestCreateTaskRoundTrip(t *testing.T) {
	srv := newTestService(t)
	ctx := context.Background()
	creator := seedUser(t, srv, "creator")
	assignee := seedUser(t, srv, "assignee")

	description := "review the invoice"
	task, err := srv.CreateTask(ctx, creator.ID, models.CreateTask{
		AssignedToID: assignee.ID,
		Title:        "Review invoice",
		Description:  &description,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	if task.Status != models.TaskStatusOpen {
		t.Errorf("new task status = %q, want open", task.Status)
	}
	if task.CreatedBy == nil || task.CreatedBy.ID != creator.ID {
		t.Errorf("embedded creator = %+v, want id %s", task.CreatedBy, creator.ID)
	}
	if task.AssignedTo == nil || task.AssignedTo.Account != assignee.Account {
		t.Errorf("embedded assignee = %+v, want account %s", task.AssignedTo, assignee.Account)
	}
	if task.Description == nil || *task.Description != description {
		t.Errorf("description = %v, want %q", task.Description, description)
	}

	fetched, err := srv.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if fetched.ID != task.ID || fetched.Title != "Review invoice" {
		t.Errorf("GetTask returned %+v", fetched)
	}
}

func TestCreateTaskRejectsSelfAssignment(t *testing.T) {
	srv := newTestService(t)
	creator := seedUser(t, srv, "creator")

	_, err := srv.CreateTask(context.Background(), creator.ID, models.CreateTask{
		AssignedToID: creator.ID,
		Title:        "Self-assigned",
	})
	if !errors.Is(err, ErrCheckViolation) {
		t.Fatalf("expected ErrCheckViolation from DB CHECK, got %v", err)
	}
}

func TestUpdateTaskStatusManagesTimestamps(t *testing.T) {
	srv := newTestService(t)
	ctx := context.Background()
	creator := seedUser(t, srv, "creator")
	assignee := seedUser(t, srv, "assignee")
	task := seedTask(t, srv, creator.ID, assignee.ID, "Lifecycle")

	done, err := srv.UpdateTaskStatus(ctx, task.ID, models.TaskStatusDone)
	if err != nil {
		t.Fatalf("UpdateTaskStatus(done): %v", err)
	}
	if done.CompletedAt == nil {
		t.Error("completed_at not set on done")
	}

	reopened, err := srv.UpdateTaskStatus(ctx, task.ID, models.TaskStatusOpen)
	if err != nil {
		t.Fatalf("UpdateTaskStatus(open): %v", err)
	}
	if reopened.CompletedAt != nil || reopened.CancelledAt != nil {
		t.Errorf("reopen did not clear terminal timestamps: %+v", reopened)
	}

	cancelled, err := srv.UpdateTaskStatus(ctx, task.ID, models.TaskStatusCancelled)
	if err != nil {
		t.Fatalf("UpdateTaskStatus(cancelled): %v", err)
	}
	if cancelled.CancelledAt == nil {
		t.Error("cancelled_at not set on cancel")
	}
}

func TestTaskMessageMembershipEnforcedInSQL(t *testing.T) {
	srv := newTestService(t)
	ctx := context.Background()
	creator := seedUser(t, srv, "creator")
	assignee := seedUser(t, srv, "assignee")
	stranger := seedUser(t, srv, "stranger")
	task := seedTask(t, srv, creator.ID, assignee.ID, "Chat")

	// Members can post.
	message, err := srv.CreateTaskMessage(ctx, task.ID, assignee.ID, models.CreateTaskMessage{Body: "on it"})
	if err != nil {
		t.Fatalf("member CreateTaskMessage: %v", err)
	}
	if message.Author == nil || message.Author.ID != assignee.ID {
		t.Errorf("message author = %+v", message.Author)
	}

	// Strangers are rejected by the INSERT's WHERE EXISTS, not handler code.
	if _, err := srv.CreateTaskMessage(ctx, task.ID, stranger.ID, models.CreateTaskMessage{Body: "let me in"}); !errors.Is(err, ErrDBNotFound) {
		t.Fatalf("stranger CreateTaskMessage: got %v, want ErrDBNotFound", err)
	}

	// Members list messages; strangers get nothing.
	messages, err := srv.ListTaskMessages(ctx, task.ID, creator.ID)
	if err != nil {
		t.Fatalf("member ListTaskMessages: %v", err)
	}
	if len(messages) != 1 || messages[0].Body != "on it" {
		t.Errorf("member messages = %+v", messages)
	}

	strangerMessages, err := srv.ListTaskMessages(ctx, task.ID, stranger.ID)
	if err != nil {
		t.Fatalf("stranger ListTaskMessages: %v", err)
	}
	if len(strangerMessages) != 0 {
		t.Errorf("stranger sees %d messages, want 0", len(strangerMessages))
	}
}

func TestTodosAndExpectationsFilterByRoleAndStatus(t *testing.T) {
	srv := newTestService(t)
	ctx := context.Background()
	creator := seedUser(t, srv, "creator")
	assignee := seedUser(t, srv, "assignee")

	open := seedTask(t, srv, creator.ID, assignee.ID, "Open task")
	cancelled := seedTask(t, srv, creator.ID, assignee.ID, "Cancelled task")
	if _, err := srv.UpdateTaskStatus(ctx, cancelled.ID, models.TaskStatusCancelled); err != nil {
		t.Fatalf("cancelling task: %v", err)
	}

	// Todos = tasks the creator delegated; cancelled tasks are hidden.
	todos, err := srv.ListTodos(ctx, creator.ID)
	if err != nil {
		t.Fatalf("ListTodos: %v", err)
	}
	if len(todos) != 1 || todos[0].ID != open.ID {
		t.Errorf("creator todos = %+v, want only %s", todos, open.ID)
	}

	// Expectations = tasks assigned to the assignee.
	expectations, err := srv.ListExpectations(ctx, assignee.ID)
	if err != nil {
		t.Fatalf("ListExpectations: %v", err)
	}
	if len(expectations) != 1 || expectations[0].ID != open.ID {
		t.Errorf("assignee expectations = %+v, want only %s", expectations, open.ID)
	}

	// Roles don't bleed: the creator has no expectations, the assignee no todos.
	if creatorExpectations, _ := srv.ListExpectations(ctx, creator.ID); len(creatorExpectations) != 0 {
		t.Errorf("creator expectations = %+v, want none", creatorExpectations)
	}
	if assigneeTodos, _ := srv.ListTodos(ctx, assignee.ID); len(assigneeTodos) != 0 {
		t.Errorf("assignee todos = %+v, want none", assigneeTodos)
	}
}

func TestUpdateTaskNullSemantics(t *testing.T) {
	srv := newTestService(t)
	ctx := context.Background()
	creator := seedUser(t, srv, "creator")
	assignee := seedUser(t, srv, "assignee")

	description := "initial description"
	dueAt := time.Date(2026, 7, 1, 12, 0, 0, 0, time.UTC)
	task, err := srv.CreateTask(ctx, creator.ID, models.CreateTask{
		AssignedToID: assignee.ID,
		Title:        "Null semantics",
		Description:  &description,
		DueAt:        &dueAt,
	})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}

	// Absent fields leave existing values untouched.
	newTitle := "Renamed"
	updated, err := srv.UpdateTask(ctx, task.ID, models.UpdateTask{Title: &newTitle})
	if err != nil {
		t.Fatalf("UpdateTask(title only): %v", err)
	}
	if updated.Description == nil || *updated.Description != description {
		t.Errorf("absent description was modified: %v", updated.Description)
	}
	if updated.DueAt == nil {
		t.Error("absent due_at was modified")
	}

	// Explicit null clears nullable columns.
	updated, err = srv.UpdateTask(ctx, task.ID, models.UpdateTask{
		Description: models.Optional[string]{Set: true, Value: nil},
		DueAt:       models.Optional[time.Time]{Set: true, Value: nil},
	})
	if err != nil {
		t.Fatalf("UpdateTask(clear): %v", err)
	}
	if updated.Description != nil {
		t.Errorf("description not cleared: %v", *updated.Description)
	}
	if updated.DueAt != nil {
		t.Errorf("due_at not cleared: %v", updated.DueAt)
	}

	// Set with a value updates normally.
	newDescription := "fresh description"
	updated, err = srv.UpdateTask(ctx, task.ID, models.UpdateTask{
		Description: models.Optional[string]{Set: true, Value: &newDescription},
	})
	if err != nil {
		t.Fatalf("UpdateTask(set value): %v", err)
	}
	if updated.Description == nil || *updated.Description != newDescription {
		t.Errorf("description not updated: %v", updated.Description)
	}
}

func TestUserAccountAndEmailAreUnique(t *testing.T) {
	srv := newTestService(t)
	ctx := context.Background()
	existing := seedUser(t, srv, "unique")

	_, err := srv.CreateUser(ctx, models.NewUser{
		ID:      existing.ID + "-other",
		Name:    "Impostor",
		Account: existing.Account,
		Email:   "different@test.example",
	})
	if !errors.Is(err, ErrDBDuplicatedEntry) {
		t.Fatalf("duplicate account: got %v, want ErrDBDuplicatedEntry", err)
	}

	_, err = srv.CreateUser(ctx, models.NewUser{
		ID:      existing.ID + "-other2",
		Name:    "Impostor",
		Account: existing.Account + "x",
		Email:   existing.Email,
	})
	if !errors.Is(err, ErrDBDuplicatedEntry) {
		t.Fatalf("duplicate email: got %v, want ErrDBDuplicatedEntry", err)
	}
}
