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
	ID                  string     `json:"id"`
	CreatedByID         string     `json:"created_by_id"`
	AssignedToID        string     `json:"assigned_to_id"`
	Title               string     `json:"title"`
	Description         *string    `json:"description,omitempty"`
	Status              string     `json:"status"`
	DueAt               *time.Time `json:"due_at,omitempty"`
	DelegatedFromTaskID *string    `json:"delegated_from_task_id,omitempty"`
	CreatedBy           *User      `json:"created_by,omitempty"`
	AssignedTo          *User      `json:"assigned_to,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	CompletedAt         *time.Time `json:"completed_at,omitempty"`
	CancelledAt         *time.Time `json:"cancelled_at,omitempty"`
}

type TaskListItem struct {
	ID                  string     `json:"id"`
	Title               string     `json:"title"`
	Description         *string    `json:"description,omitempty"`
	Status              string     `json:"status"`
	DueAt               *time.Time `json:"due_at,omitempty"`
	DelegatedFromTaskID *string    `json:"delegated_from_task_id,omitempty"`
	AssignedBy          *User      `json:"assigned_by,omitempty"`
	AssignedTo          *User      `json:"assigned_to,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
	CompletedAt         *time.Time `json:"completed_at,omitempty"`
	CancelledAt         *time.Time `json:"cancelled_at,omitempty"`
}

type CreateTask struct {
	AssignedToID        string     `json:"assigned_to_id"`
	Title               string     `json:"title"`
	Description         *string    `json:"description,omitempty"`
	DueAt               *time.Time `json:"due_at,omitempty"`
	DelegatedFromTaskID *string    `json:"delegated_from_task_id,omitempty"`
}

type UpdateTask struct {
	AssignedToID        *string    `json:"assigned_to_id,omitempty"`
	Title               *string    `json:"title,omitempty"`
	Description         *string    `json:"description,omitempty"`
	DueAt               *time.Time `json:"due_at,omitempty"`
	DelegatedFromTaskID *string    `json:"delegated_from_task_id,omitempty"`
}

type UpdateTaskStatus struct {
	Status string `json:"status"`
}

type TaskMessage struct {
	ID        string    `json:"id"`
	TaskID    string    `json:"task_id"`
	AuthorID  string    `json:"author_id"`
	Author    *User     `json:"author,omitempty"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateTaskMessage struct {
	Body string `json:"body"`
}
