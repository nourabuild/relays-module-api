package models

import "time"

const (
	TaskStatusOpen      = "open"
	TaskStatusDone      = "done"
	TaskStatusCancelled = "cancelled"
)

func IsValidTaskStatus(status string) bool {
	return status == TaskStatusOpen ||
		status == TaskStatusDone ||
		status == TaskStatusCancelled
}

func IsTerminalTaskStatus(status string) bool {
	return status == TaskStatusDone || status == TaskStatusCancelled
}

type Task struct {
	ID                  string      `json:"id"`
	CreatedByID         string      `json:"created_by_id"`
	AssignedToID        string      `json:"assigned_to_id"`
	Title               string      `json:"title"`
	Description         *string     `json:"description,omitempty"`
	Status              string      `json:"status"`
	DueAt               *time.Time  `json:"due_at,omitempty"`
	DelegatedFromTaskID *string     `json:"delegated_from_task_id,omitempty"`
	CreatedBy           *PublicUser `json:"created_by,omitempty"`
	AssignedTo          *PublicUser `json:"assigned_to,omitempty"`
	CreatedAt           time.Time   `json:"created_at"`
	UpdatedAt           time.Time   `json:"updated_at"`
	CompletedAt         *time.Time  `json:"completed_at,omitempty"`
	CancelledAt         *time.Time  `json:"cancelled_at,omitempty"`
}

type TaskListItem struct {
	ID                  string      `json:"id"`
	Title               string      `json:"title"`
	Description         *string     `json:"description,omitempty"`
	Status              string      `json:"status"`
	DueAt               *time.Time  `json:"due_at,omitempty"`
	DelegatedFromTaskID *string     `json:"delegated_from_task_id,omitempty"`
	AssignedBy          *PublicUser `json:"assigned_by,omitempty"`
	AssignedTo          *PublicUser `json:"assigned_to,omitempty"`
	CreatedAt           time.Time   `json:"created_at"`
	UpdatedAt           time.Time   `json:"updated_at"`
	CompletedAt         *time.Time  `json:"completed_at,omitempty"`
	CancelledAt         *time.Time  `json:"cancelled_at,omitempty"`
}

type CreateTask struct {
	AssignedToID        string     `json:"assigned_to_id"`
	Title               string     `json:"title"`
	Description         *string    `json:"description,omitempty"`
	DueAt               *time.Time `json:"due_at,omitempty"`
	DelegatedFromTaskID *string    `json:"delegated_from_task_id,omitempty"`
}

// UpdateTask uses Optional for nullable columns: an omitted field is left
// unchanged, an explicit JSON null clears it. assigned_to_id and title are
// NOT NULL and therefore never clearable.
type UpdateTask struct {
	AssignedToID        *string             `json:"assigned_to_id,omitempty"`
	Title               *string             `json:"title,omitempty"`
	Description         Optional[string]    `json:"description"`
	DueAt               Optional[time.Time] `json:"due_at"`
	DelegatedFromTaskID Optional[string]    `json:"delegated_from_task_id"`
}

type UpdateTaskStatus struct {
	Status string `json:"status"`
}

type TaskMessage struct {
	ID        string      `json:"id"`
	TaskID    string      `json:"task_id"`
	AuthorID  string      `json:"author_id"`
	Author    *PublicUser `json:"author,omitempty"`
	Body      string      `json:"body"`
	CreatedAt time.Time   `json:"created_at"`
}

type CreateTaskMessage struct {
	Body string `json:"body"`
}
