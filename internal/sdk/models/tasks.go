package models

import "time"

const (
	TaskStatusAssigned      = "assigned"
	TaskStatusInProgress    = "in_progress"
	TaskStatusBlocked       = "blocked"
	TaskStatusPendingReview = "pending_review"
	TaskStatusCompleted     = "completed"
	TaskStatusCancelled     = "cancelled"

	AssignmentModeSameWork       = "same_work"
	AssignmentModeCustomizedWork = "customized_work"

	AttachmentScopeTemplate = "template"
	AttachmentScopeBatch    = "batch"
	AttachmentScopeInstance = "instance"

	DependencyTypeBlocksStart      = "blocks_start"
	DependencyTypeBlocksCompletion = "blocks_completion"

	SubmissionStatusSubmitted         = "submitted"
	SubmissionStatusAccepted          = "accepted"
	SubmissionStatusRejected          = "rejected"
	SubmissionStatusRevisionRequested = "revision_requested"
)

func IsValidTaskStatus(status string) bool {
	switch status {
	case TaskStatusAssigned,
		TaskStatusInProgress,
		TaskStatusBlocked,
		TaskStatusPendingReview,
		TaskStatusCompleted,
		TaskStatusCancelled:
		return true
	default:
		return false
	}
}

func IsTerminalTaskStatus(status string) bool {
	return status == TaskStatusCompleted || status == TaskStatusCancelled
}

func IsValidAssignmentMode(mode string) bool {
	return mode == AssignmentModeSameWork || mode == AssignmentModeCustomizedWork
}

func IsValidAttachmentScope(scope string) bool {
	return scope == AttachmentScopeTemplate || scope == AttachmentScopeBatch || scope == AttachmentScopeInstance
}

func IsValidTaskDependencyType(dependencyType string) bool {
	return dependencyType == DependencyTypeBlocksStart ||
		dependencyType == DependencyTypeBlocksCompletion
}

func IsValidSubmissionReviewStatus(status string) bool {
	return status == SubmissionStatusAccepted ||
		status == SubmissionStatusRejected ||
		status == SubmissionStatusRevisionRequested
}

type TaskTemplate struct {
	ID              string         `json:"id"`
	CreatedBy       string         `json:"created_by"`
	Title           string         `json:"title"`
	Description     *string        `json:"description,omitempty"`
	Instructions    *string        `json:"instructions,omitempty"`
	DefaultPriority *string        `json:"default_priority,omitempty"`
	DefaultDueAt    *time.Time     `json:"default_due_at,omitempty"`
	ReviewRequired  bool           `json:"review_required"`
	Metadata        map[string]any `json:"metadata"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	ArchivedAt      *time.Time     `json:"archived_at,omitempty"`
}

type TaskTemplateInput struct {
	Title           string         `json:"title"`
	Description     *string        `json:"description,omitempty"`
	Instructions    *string        `json:"instructions,omitempty"`
	DefaultPriority *string        `json:"default_priority,omitempty"`
	DefaultDueAt    *time.Time     `json:"default_due_at,omitempty"`
	ReviewRequired  *bool          `json:"review_required,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

type UpdateTaskTemplate struct {
	Title           *string        `json:"title,omitempty"`
	Description     *string        `json:"description,omitempty"`
	Instructions    *string        `json:"instructions,omitempty"`
	DefaultPriority *string        `json:"default_priority,omitempty"`
	DefaultDueAt    *time.Time     `json:"default_due_at,omitempty"`
	ReviewRequired  *bool          `json:"review_required,omitempty"`
	Metadata        map[string]any `json:"metadata,omitempty"`
}

type TaskBatch struct {
	ID             string         `json:"id"`
	TemplateID     string         `json:"template_id"`
	CreatedBy      string         `json:"created_by"`
	Title          *string        `json:"title,omitempty"`
	Description    *string        `json:"description,omitempty"`
	AssignmentMode string         `json:"assignment_mode"`
	IdempotencyKey *string        `json:"idempotency_key,omitempty"`
	Metadata       map[string]any `json:"metadata"`
	CreatedAt      time.Time      `json:"created_at"`
}

type CreateTaskBatch struct {
	TemplateID     *string               `json:"template_id,omitempty"`
	Template       *TaskTemplateInput    `json:"template,omitempty"`
	Title          *string               `json:"title,omitempty"`
	Description    *string               `json:"description,omitempty"`
	AssignmentMode *string               `json:"assignment_mode,omitempty"`
	IdempotencyKey *string               `json:"idempotency_key,omitempty"`
	Metadata       map[string]any        `json:"metadata,omitempty"`
	Assignments    []TaskAssignmentInput `json:"assignments"`
	Dependencies   []TaskBatchDependency `json:"dependencies,omitempty"`
}

type TaskAssignmentInput struct {
	AssigneeID    string                   `json:"assignee_id"`
	AssignmentKey *string                  `json:"assignment_key,omitempty"`
	Overrides     *TaskAssignmentOverrides `json:"overrides,omitempty"`
	CustomPayload map[string]any           `json:"custom_payload,omitempty"`
}

type TaskAssignmentOverrides struct {
	Title          *string    `json:"title,omitempty"`
	Description    *string    `json:"description,omitempty"`
	Instructions   *string    `json:"instructions,omitempty"`
	Priority       *string    `json:"priority,omitempty"`
	DueAt          *time.Time `json:"due_at,omitempty"`
	ReviewRequired *bool      `json:"review_required,omitempty"`
}

type TaskBatchCreateResult struct {
	Batch          TaskBatch                `json:"batch"`
	Template       TaskTemplate             `json:"template"`
	Instances      []TaskInstance           `json:"instances"`
	Dependencies   []TaskInstanceDependency `json:"dependencies,omitempty"`
	TotalInstances int                      `json:"total_instances"`
}

type TaskInstance struct {
	ID                       string         `json:"id"`
	BatchID                  string         `json:"batch_id"`
	TemplateID               *string        `json:"template_id,omitempty"`
	CreatedBy                string         `json:"created_by"`
	AssigneeID               string         `json:"assignee_id"`
	AssignmentKey            *string        `json:"assignment_key,omitempty"`
	Title                    string         `json:"title"`
	Description              *string        `json:"description,omitempty"`
	Instructions             *string        `json:"instructions,omitempty"`
	Priority                 *string        `json:"priority,omitempty"`
	DueAt                    *time.Time     `json:"due_at,omitempty"`
	Status                   string         `json:"status"`
	ReviewRequired           bool           `json:"review_required"`
	ProgressPercent          int            `json:"progress_percent"`
	StartedAt                *time.Time     `json:"started_at,omitempty"`
	CompletedAt              *time.Time     `json:"completed_at,omitempty"`
	CancelledAt              *time.Time     `json:"cancelled_at,omitempty"`
	CompletionNote           *string        `json:"completion_note,omitempty"`
	TemplateSnapshot         map[string]any `json:"template_snapshot"`
	CustomPayload            map[string]any `json:"custom_payload"`
	ReplacedByTaskInstanceID *string        `json:"replaced_by_task_instance_id,omitempty"`
	ReplacesTaskInstanceID   *string        `json:"replaces_task_instance_id,omitempty"`
	CreatedAt                time.Time      `json:"created_at"`
	UpdatedAt                time.Time      `json:"updated_at"`
}

type TaskInstanceFilter struct {
	Status    *string
	DueBefore *time.Time
	OpenOnly  bool
}

type UpdateTaskInstance struct {
	Title           *string        `json:"title,omitempty"`
	Description     *string        `json:"description,omitempty"`
	Instructions    *string        `json:"instructions,omitempty"`
	Priority        *string        `json:"priority,omitempty"`
	DueAt           *time.Time     `json:"due_at,omitempty"`
	ReviewRequired  *bool          `json:"review_required,omitempty"`
	ProgressPercent *int           `json:"progress_percent,omitempty"`
	CustomPayload   map[string]any `json:"custom_payload,omitempty"`
}

type UpdateTaskInstanceStatus struct {
	Status         string  `json:"status"`
	CompletionNote *string `json:"completion_note,omitempty"`
}

type SubmitTaskReview struct {
	Note *string `json:"note,omitempty"`
}

type TaskBatchProgress struct {
	BatchID       string         `json:"batch_id"`
	Title         *string        `json:"title,omitempty"`
	CreatedBy     string         `json:"created_by"`
	Total         int            `json:"total"`
	DerivedStatus string         `json:"derived_status"`
	Summary       map[string]int `json:"summary"`
	Instances     []TaskInstance `json:"instances,omitempty"`
}

type TaskInstanceEvent struct {
	ID             string         `json:"id"`
	TaskInstanceID string         `json:"task_instance_id"`
	ActorID        string         `json:"actor_id"`
	EventType      string         `json:"event_type"`
	OldValue       map[string]any `json:"old_value,omitempty"`
	NewValue       map[string]any `json:"new_value,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
}

type TaskBatchDependency struct {
	AssignmentKey          string  `json:"assignment_key"`
	DependsOnAssignmentKey string  `json:"depends_on_assignment_key"`
	DependencyType         *string `json:"dependency_type,omitempty"`
}

type TaskInstanceDependency struct {
	ID                      string    `json:"id"`
	TaskInstanceID          string    `json:"task_instance_id"`
	DependsOnTaskInstanceID string    `json:"depends_on_task_instance_id"`
	DependencyType          string    `json:"dependency_type"`
	CreatedBy               string    `json:"created_by"`
	CreatedAt               time.Time `json:"created_at"`
}

type CreateTaskInstanceDependency struct {
	DependsOnTaskInstanceID string  `json:"depends_on_task_instance_id"`
	DependencyType          *string `json:"dependency_type,omitempty"`
}

type TaskComment struct {
	ID             string     `json:"id"`
	TaskInstanceID string     `json:"task_instance_id"`
	AuthorID       string     `json:"author_id"`
	Body           string     `json:"body"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      *time.Time `json:"updated_at,omitempty"`
}

type CreateTaskComment struct {
	Body string `json:"body"`
}

type TaskBatchComment struct {
	ID        string    `json:"id"`
	BatchID   string    `json:"batch_id"`
	AuthorID  string    `json:"author_id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

type CreateTaskBatchComment struct {
	Body string `json:"body"`
}

type TaskAttachment struct {
	ID             string    `json:"id"`
	Scope          string    `json:"scope"`
	TemplateID     *string   `json:"template_id,omitempty"`
	BatchID        *string   `json:"batch_id,omitempty"`
	TaskInstanceID *string   `json:"task_instance_id,omitempty"`
	UploadedBy     string    `json:"uploaded_by"`
	FileURL        string    `json:"file_url"`
	FileName       *string   `json:"file_name,omitempty"`
	MimeType       *string   `json:"mime_type,omitempty"`
	SizeBytes      *int64    `json:"size_bytes,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

type CreateTaskAttachment struct {
	FileURL   string  `json:"file_url"`
	FileName  *string `json:"file_name,omitempty"`
	MimeType  *string `json:"mime_type,omitempty"`
	SizeBytes *int64  `json:"size_bytes,omitempty"`
}

type TaskSubmission struct {
	ID             string     `json:"id"`
	TaskInstanceID string     `json:"task_instance_id"`
	SubmittedBy    string     `json:"submitted_by"`
	Note           *string    `json:"note,omitempty"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	ReviewedAt     *time.Time `json:"reviewed_at,omitempty"`
	ReviewedBy     *string    `json:"reviewed_by,omitempty"`
}

type CreateTaskSubmission struct {
	Note *string `json:"note,omitempty"`
}

type ReviewTaskSubmission struct {
	Status string `json:"status"`
}
