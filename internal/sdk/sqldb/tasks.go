package sqldb

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nourabuild/relays-api/internal/sdk/models"
)

type sqlRunner interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func (s *service) CreateTaskBatch(ctx context.Context, creatorID string, input models.CreateTaskBatch) (models.TaskBatchCreateResult, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.TaskBatchCreateResult{}, fmt.Errorf("beginning task batch transaction: %w", err)
	}
	defer tx.Rollback()

	if input.IdempotencyKey != nil {
		existingBatchID, err := getExistingBatchIDByIdempotencyKey(ctx, tx, creatorID, *input.IdempotencyKey)
		if err != nil && !errors.Is(err, ErrDBNotFound) {
			return models.TaskBatchCreateResult{}, err
		}
		if existingBatchID != "" {
			result, err := getTaskBatchCreateResult(ctx, tx, existingBatchID)
			if err != nil {
				return models.TaskBatchCreateResult{}, err
			}
			if err := tx.Commit(); err != nil {
				return models.TaskBatchCreateResult{}, fmt.Errorf("committing idempotent task batch transaction: %w", err)
			}
			return result, nil
		}
	}

	assignments := withCreatorAssignment(creatorID, input.Assignments)

	metadataJSON, err := marshalJSONMap(input.Metadata)
	if err != nil {
		return models.TaskBatchCreateResult{}, fmt.Errorf("encoding task batch metadata: %w", err)
	}

	reviewRequired := false
	if input.ReviewRequired != nil {
		reviewRequired = *input.ReviewRequired
	}

	const createBatchQuery = `
		INSERT INTO todos.task_batches (
			created_by,
			title,
			description,
			instructions,
			priority,
			due_at,
			review_required,
			idempotency_key,
			metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9::jsonb)
		RETURNING id::text, created_by, title, description, instructions, priority,
		          due_at, review_required, idempotency_key, metadata::text, created_at
	`

	batch, err := scanTaskBatch(tx.QueryRowContext(ctx, createBatchQuery,
		creatorID,
		input.Title,
		NullString(input.Description),
		NullString(input.Instructions),
		NullString(input.Priority),
		NullTime(input.DueAt),
		reviewRequired,
		NullString(input.IdempotencyKey),
		metadataJSON,
	))
	if err != nil {
		if isPgError(err, uniqueViolation) {
			return models.TaskBatchCreateResult{}, ErrDBDuplicatedEntry
		}
		if isPgError(err, checkViolation) {
			return models.TaskBatchCreateResult{}, ErrCheckViolation
		}
		if isPgError(err, foreignKeyViolation) {
			return models.TaskBatchCreateResult{}, ErrForeignKeyViolation
		}
		return models.TaskBatchCreateResult{}, fmt.Errorf("creating task batch: %w", err)
	}

	instances := make([]models.TaskInstance, 0, len(assignments))
	instancesByAssignmentKey := make(map[string]models.TaskInstance, len(assignments))
	for _, assignment := range assignments {
		instance, err := createTaskInstanceForAssignment(ctx, tx, creatorID, batch, assignment)
		if err != nil {
			return models.TaskBatchCreateResult{}, err
		}
		if err := createTaskInstanceEvent(ctx, tx, instance.ID, creatorID, "assigned", nil, map[string]any{
			"assignee_id": instance.AssigneeID,
			"status":      instance.Status,
		}); err != nil {
			return models.TaskBatchCreateResult{}, err
		}
		instances = append(instances, instance)
		if instance.AssignmentKey != nil {
			instancesByAssignmentKey[*instance.AssignmentKey] = instance
		}
	}

	dependencies, err := createTaskBatchDependencies(ctx, tx, creatorID, input.Dependencies, instancesByAssignmentKey)
	if err != nil {
		return models.TaskBatchCreateResult{}, err
	}

	result := models.TaskBatchCreateResult{
		Batch:          batch,
		Instances:      instances,
		Dependencies:   dependencies,
		TotalInstances: len(instances),
	}
	result, err = populateTaskBatchCreateResultAssignees(ctx, tx, result)
	if err != nil {
		return models.TaskBatchCreateResult{}, err
	}

	if err := tx.Commit(); err != nil {
		return models.TaskBatchCreateResult{}, fmt.Errorf("committing task batch transaction: %w", err)
	}

	return result, nil
}

func (s *service) GetTaskBatch(ctx context.Context, batchID string) (models.TaskBatch, error) {
	batch, err := getTaskBatch(ctx, s.db, batchID)
	if err != nil {
		return models.TaskBatch{}, err
	}
	return populateTaskBatchAssignees(ctx, s.db, batch)
}

func (s *service) GetTaskBatchProgress(ctx context.Context, batchID string, includeInstances bool) (models.TaskBatchProgress, error) {
	batch, err := getTaskBatch(ctx, s.db, batchID)
	if err != nil {
		return models.TaskBatchProgress{}, err
	}

	return buildTaskBatchProgress(ctx, s.db, batch, includeInstances)
}

func buildTaskBatchProgress(ctx context.Context, runner sqlRunner, batch models.TaskBatch, includeInstances bool) (models.TaskBatchProgress, error) {
	instances, err := listTaskBatchInstances(ctx, runner, batch.ID)
	if err != nil {
		return models.TaskBatchProgress{}, err
	}

	assignees, err := listTaskBatchAssignees(ctx, runner, batch.ID)
	if err != nil {
		return models.TaskBatchProgress{}, err
	}
	instances = applyAssigneesToTaskInstances(instances, assignees)

	summary := map[string]int{
		"assigned":       0,
		"in_progress":    0,
		"blocked":        0,
		"pending_review": 0,
		"completed":      0,
		"cancelled":      0,
		"overdue":        0,
	}

	now := time.Now()
	for _, instance := range instances {
		summary[instance.Status]++
		if instance.DueAt != nil && instance.DueAt.Before(now) && !models.IsTerminalTaskStatus(instance.Status) {
			summary["overdue"]++
		}
	}

	progress := models.TaskBatchProgress{
		BatchID:       batch.ID,
		Title:         batch.Title,
		CreatedBy:     batch.CreatedBy,
		Total:         len(instances),
		Summary:       summary,
		DerivedStatus: deriveBatchStatus(len(instances), summary),
		Assignees:     assignees,
	}
	if includeInstances {
		progress.Instances = instances
	}

	return progress, nil
}

func (s *service) ListTaskBatchInstances(ctx context.Context, batchID string) ([]models.TaskInstance, error) {
	instances, err := listTaskBatchInstances(ctx, s.db, batchID)
	if err != nil {
		return nil, err
	}
	return populateTaskInstancesAssignees(ctx, s.db, instances)
}

// IsTaskBatchAssignee reports whether the user is assigned to any task instance
// within the batch. This drives the delegation chain: a participant who already
// holds a task in the batch may append further tasks to it.
func (s *service) IsTaskBatchAssignee(ctx context.Context, batchID, userID string) (bool, error) {
	const query = `
		SELECT EXISTS (
			SELECT 1
			FROM todos.task_instances
			WHERE batch_id = $1 AND assignee_id = $2
		)
	`

	var exists bool
	if err := s.db.QueryRowContext(ctx, query, batchID, userID).Scan(&exists); err != nil {
		return false, fmt.Errorf("checking task batch assignee: %w", err)
	}
	return exists, nil
}

func (s *service) AddTaskBatchInstance(ctx context.Context, batchID, creatorID string, assignment models.TaskAssignmentInput) (models.TaskInstance, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.TaskInstance{}, fmt.Errorf("beginning add task batch instance transaction: %w", err)
	}
	defer tx.Rollback()

	batch, err := getTaskBatch(ctx, tx, batchID)
	if err != nil {
		return models.TaskInstance{}, err
	}

	instance, err := createTaskInstanceForAssignment(ctx, tx, creatorID, batch, assignment)
	if err != nil {
		return models.TaskInstance{}, err
	}

	if err := createTaskInstanceEvent(ctx, tx, instance.ID, creatorID, "assigned", nil, map[string]any{
		"assignee_id": instance.AssigneeID,
		"status":      instance.Status,
	}); err != nil {
		return models.TaskInstance{}, err
	}

	instance, err = populateTaskInstanceAssignees(ctx, tx, instance)
	if err != nil {
		return models.TaskInstance{}, err
	}

	if err := tx.Commit(); err != nil {
		return models.TaskInstance{}, fmt.Errorf("committing add task batch instance transaction: %w", err)
	}

	return instance, nil
}

func (s *service) GetTaskInstance(ctx context.Context, taskInstanceID string) (models.TaskInstance, error) {
	instance, err := getTaskInstance(ctx, s.db, taskInstanceID)
	if err != nil {
		return models.TaskInstance{}, err
	}
	return populateTaskInstanceAssignees(ctx, s.db, instance)
}

func (s *service) ListTaskInstancesForUser(ctx context.Context, userID string, filter models.TaskInstanceFilter) ([]models.TaskInstance, error) {
	args := []any{userID}
	conditions := []string{"(created_by = $1 OR assignee_id = $1)"}

	if filter.OpenOnly {
		conditions = append(conditions, "status NOT IN ('completed', 'cancelled')")
	} else if filter.Status != nil {
		args = append(args, *filter.Status)
		conditions = append(conditions, fmt.Sprintf("status = $%d", len(args)))
	}

	if filter.DueBefore != nil {
		args = append(args, *filter.DueBefore)
		conditions = append(conditions, fmt.Sprintf("due_at <= $%d", len(args)))
	}

	query := `
		SELECT id::text, batch_id::text, created_by, assignee_id,
		       assignment_key, title, description, instructions, priority, due_at, status,
		       review_required, progress_percent, started_at, completed_at, cancelled_at, completion_note,
		       custom_payload::text,
		       replaced_by_task_instance_id::text, replaces_task_instance_id::text,
		       created_at, updated_at
		FROM todos.task_instances
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY due_at ASC NULLS LAST, created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing task instances for user: %w", err)
	}
	defer rows.Close()

	instances, err := scanTaskInstances(rows)
	if err != nil {
		return nil, err
	}
	return populateTaskInstancesAssignees(ctx, s.db, instances)
}

func (s *service) UpdateTaskInstance(ctx context.Context, taskInstanceID, actorID string, input models.UpdateTaskInstance) (models.TaskInstance, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.TaskInstance{}, fmt.Errorf("beginning task instance update transaction: %w", err)
	}
	defer tx.Rollback()

	oldInstance, err := getTaskInstance(ctx, tx, taskInstanceID)
	if err != nil {
		return models.TaskInstance{}, err
	}

	customPayloadJSON, err := nullableJSONMapParam(input.CustomPayload)
	if err != nil {
		return models.TaskInstance{}, fmt.Errorf("encoding task instance custom payload: %w", err)
	}

	const query = `
		UPDATE todos.task_instances
		SET title = COALESCE($2, title),
		    description = COALESCE($3, description),
		    instructions = COALESCE($4, instructions),
		    priority = COALESCE($5, priority),
		    due_at = COALESCE($6, due_at),
		    review_required = COALESCE($7, review_required),
		    custom_payload = COALESCE($8::jsonb, custom_payload),
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING id::text, batch_id::text, created_by, assignee_id,
		          assignment_key, title, description, instructions, priority, due_at, status,
		          review_required, progress_percent, started_at, completed_at, cancelled_at, completion_note,
		          custom_payload::text,
		          replaced_by_task_instance_id::text, replaces_task_instance_id::text,
		          created_at, updated_at
	`

	updated, err := scanTaskInstance(tx.QueryRowContext(ctx, query,
		taskInstanceID,
		NullString(input.Title),
		NullString(input.Description),
		NullString(input.Instructions),
		NullString(input.Priority),
		NullTime(input.DueAt),
		NullBool(input.ReviewRequired),
		customPayloadJSON,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.TaskInstance{}, ErrDBNotFound
		}
		if isPgError(err, checkViolation) {
			return models.TaskInstance{}, ErrCheckViolation
		}
		return models.TaskInstance{}, fmt.Errorf("updating task instance: %w", err)
	}

	eventType := "updated"
	if input.DueAt != nil {
		eventType = "due_date_changed"
	}
	if err := createTaskInstanceEvent(ctx, tx, updated.ID, actorID, eventType, taskInstanceUpdateEventValue(oldInstance, input), taskInstanceUpdateEventValue(updated, input)); err != nil {
		return models.TaskInstance{}, err
	}

	updated, err = populateTaskInstanceAssignees(ctx, tx, updated)
	if err != nil {
		return models.TaskInstance{}, err
	}

	if err := tx.Commit(); err != nil {
		return models.TaskInstance{}, fmt.Errorf("committing task instance update transaction: %w", err)
	}

	return updated, nil
}

func (s *service) UpdateTaskInstanceStatus(ctx context.Context, taskInstanceID, actorID string, input models.UpdateTaskInstanceStatus) (models.TaskInstance, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.TaskInstance{}, fmt.Errorf("beginning task status transaction: %w", err)
	}
	defer tx.Rollback()

	oldInstance, err := getTaskInstance(ctx, tx, taskInstanceID)
	if err != nil {
		return models.TaskInstance{}, err
	}

	if err := enforceTaskDependenciesForStatus(ctx, tx, taskInstanceID, input.Status); err != nil {
		return models.TaskInstance{}, err
	}

	const query = `
		UPDATE todos.task_instances
		SET status = $2,
		    completion_note = COALESCE($3, completion_note),
		    started_at = CASE
		        WHEN $2 IN ('in_progress', 'pending_review', 'completed') AND started_at IS NULL THEN CURRENT_TIMESTAMP
		        WHEN $2 = 'assigned' THEN NULL
		        ELSE started_at
		    END,
		    completed_at = CASE
		        WHEN $2 = 'completed' THEN CURRENT_TIMESTAMP
		        WHEN $2 <> 'completed' THEN NULL
		        ELSE completed_at
		    END,
		    cancelled_at = CASE
		        WHEN $2 = 'cancelled' THEN CURRENT_TIMESTAMP
		        WHEN $2 <> 'cancelled' THEN NULL
		        ELSE cancelled_at
		    END,
		    progress_percent = $4,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING id::text, batch_id::text, created_by, assignee_id,
		          assignment_key, title, description, instructions, priority, due_at, status,
		          review_required, progress_percent, started_at, completed_at, cancelled_at, completion_note,
		          custom_payload::text,
		          replaced_by_task_instance_id::text, replaces_task_instance_id::text,
		          created_at, updated_at
	`

	updated, err := scanTaskInstance(tx.QueryRowContext(ctx, query,
		taskInstanceID,
		input.Status,
		NullString(input.CompletionNote),
		models.ProgressForStatus(input.Status),
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.TaskInstance{}, ErrDBNotFound
		}
		if isPgError(err, checkViolation) {
			return models.TaskInstance{}, ErrCheckViolation
		}
		return models.TaskInstance{}, fmt.Errorf("updating task instance status: %w", err)
	}

	eventType := "status_changed"
	if input.Status == models.TaskStatusCancelled {
		eventType = "cancelled"
	} else if models.IsTerminalTaskStatus(oldInstance.Status) && !models.IsTerminalTaskStatus(updated.Status) {
		eventType = "reopened"
	}

	if err := createTaskInstanceEvent(ctx, tx, updated.ID, actorID, eventType, map[string]any{
		"status": oldInstance.Status,
	}, map[string]any{
		"status": updated.Status,
	}); err != nil {
		return models.TaskInstance{}, err
	}

	updated, err = populateTaskInstanceAssignees(ctx, tx, updated)
	if err != nil {
		return models.TaskInstance{}, err
	}

	if err := tx.Commit(); err != nil {
		return models.TaskInstance{}, fmt.Errorf("committing task status transaction: %w", err)
	}

	return updated, nil
}

func (s *service) DeleteTaskInstance(ctx context.Context, taskInstanceID string) error {
	const query = `DELETE FROM todos.task_instances WHERE id = $1`

	result, err := s.db.ExecContext(ctx, query, taskInstanceID)
	if err != nil {
		if isPgError(err, foreignKeyViolation) {
			return ErrForeignKeyViolation
		}
		return fmt.Errorf("deleting task instance: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking deleted task instance rows: %w", err)
	}
	if rowsAffected == 0 {
		return ErrDBNotFound
	}

	return nil
}

func (s *service) SubmitTaskForReview(ctx context.Context, taskInstanceID, submittedBy string, input models.SubmitTaskReview) (models.TaskInstance, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.TaskInstance{}, fmt.Errorf("beginning task review submission transaction: %w", err)
	}
	defer tx.Rollback()

	oldInstance, err := getTaskInstance(ctx, tx, taskInstanceID)
	if err != nil {
		return models.TaskInstance{}, err
	}
	if !oldInstance.ReviewRequired {
		return models.TaskInstance{}, ErrCheckViolation
	}
	if err := enforceTaskDependenciesForStatus(ctx, tx, taskInstanceID, models.TaskStatusCompleted); err != nil {
		return models.TaskInstance{}, err
	}

	submission, err := createTaskSubmission(ctx, tx, taskInstanceID, submittedBy, models.CreateTaskSubmission{
		Note: input.Note,
	})
	if err != nil {
		return models.TaskInstance{}, err
	}

	updated, err := setTaskInstancePendingReview(ctx, tx, taskInstanceID, input.Note)
	if err != nil {
		return models.TaskInstance{}, err
	}

	if oldInstance.Status != updated.Status {
		if err := createTaskInstanceEvent(ctx, tx, updated.ID, submittedBy, "status_changed", map[string]any{
			"status": oldInstance.Status,
		}, map[string]any{
			"status":        updated.Status,
			"submission_id": submission.ID,
		}); err != nil {
			return models.TaskInstance{}, err
		}
	}
	if err := createTaskInstanceEvent(ctx, tx, taskInstanceID, submittedBy, "submission_added", nil, map[string]any{
		"submission_id": submission.ID,
		"status":        submission.Status,
	}); err != nil {
		return models.TaskInstance{}, err
	}

	updated, err = populateTaskInstanceAssignees(ctx, tx, updated)
	if err != nil {
		return models.TaskInstance{}, err
	}

	if err := tx.Commit(); err != nil {
		return models.TaskInstance{}, fmt.Errorf("committing task review submission transaction: %w", err)
	}

	return updated, nil
}

func (s *service) ListTaskInstanceEvents(ctx context.Context, taskInstanceID string) ([]models.TaskInstanceEvent, error) {
	const query = `
		SELECT id::text, task_instance_id::text, actor_id, event_type,
		       old_value::text, new_value::text, created_at
		FROM todos.task_instance_events
		WHERE task_instance_id = $1
		ORDER BY created_at ASC
	`

	rows, err := s.db.QueryContext(ctx, query, taskInstanceID)
	if err != nil {
		return nil, fmt.Errorf("listing task instance events: %w", err)
	}
	defer rows.Close()

	var events []models.TaskInstanceEvent
	for rows.Next() {
		event, err := scanTaskInstanceEvent(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning task instance event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating task instance events: %w", err)
	}

	return events, nil
}

func (s *service) CreateTaskInstanceDependency(ctx context.Context, taskInstanceID, creatorID string, input models.CreateTaskInstanceDependency) (models.TaskInstanceDependency, error) {
	dependencyType, err := normalizeTaskDependencyType(input.DependencyType)
	if err != nil {
		return models.TaskInstanceDependency{}, err
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.TaskInstanceDependency{}, fmt.Errorf("beginning task dependency transaction: %w", err)
	}
	defer tx.Rollback()

	dependency, err := createTaskInstanceDependency(ctx, tx, taskInstanceID, input.DependsOnTaskInstanceID, creatorID, dependencyType)
	if err != nil {
		return models.TaskInstanceDependency{}, err
	}

	if err := createTaskInstanceEvent(ctx, tx, taskInstanceID, creatorID, "dependency_added", nil, map[string]any{
		"dependency_id":               dependency.ID,
		"depends_on_task_instance_id": dependency.DependsOnTaskInstanceID,
		"dependency_type":             dependency.DependencyType,
	}); err != nil {
		return models.TaskInstanceDependency{}, err
	}

	if err := tx.Commit(); err != nil {
		return models.TaskInstanceDependency{}, fmt.Errorf("committing task dependency transaction: %w", err)
	}

	return dependency, nil
}

func (s *service) ListTaskInstanceDependencies(ctx context.Context, taskInstanceID string) ([]models.TaskInstanceDependency, error) {
	return listTaskInstanceDependencies(ctx, s.db, taskInstanceID)
}

func (s *service) ListTaskInstanceDependents(ctx context.Context, taskInstanceID string) ([]models.TaskInstanceDependency, error) {
	return listTaskInstanceDependents(ctx, s.db, taskInstanceID)
}

func (s *service) CreateTaskComment(ctx context.Context, taskInstanceID, authorID string, input models.CreateTaskComment) (models.TaskComment, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.TaskComment{}, fmt.Errorf("beginning task comment transaction: %w", err)
	}
	defer tx.Rollback()

	const query = `
		INSERT INTO todos.task_comments (task_instance_id, author_id, body)
		VALUES ($1, $2, $3)
		RETURNING id::text, task_instance_id::text, author_id, body, created_at, updated_at
	`

	comment, err := scanTaskComment(tx.QueryRowContext(ctx, query, taskInstanceID, authorID, input.Body))
	if err != nil {
		if isPgError(err, foreignKeyViolation) {
			return models.TaskComment{}, ErrForeignKeyViolation
		}
		if isPgError(err, checkViolation) {
			return models.TaskComment{}, ErrCheckViolation
		}
		return models.TaskComment{}, fmt.Errorf("creating task comment: %w", err)
	}

	if err := createTaskInstanceEvent(ctx, tx, taskInstanceID, authorID, "comment_added", nil, map[string]any{
		"comment_id": comment.ID,
	}); err != nil {
		return models.TaskComment{}, err
	}

	if err := tx.Commit(); err != nil {
		return models.TaskComment{}, fmt.Errorf("committing task comment transaction: %w", err)
	}

	return comment, nil
}

func (s *service) ListTaskComments(ctx context.Context, taskInstanceID string) ([]models.TaskComment, error) {
	const query = `
		SELECT id::text, task_instance_id::text, author_id, body, created_at, updated_at
		FROM todos.task_comments
		WHERE task_instance_id = $1
		ORDER BY created_at ASC
	`

	rows, err := s.db.QueryContext(ctx, query, taskInstanceID)
	if err != nil {
		return nil, fmt.Errorf("listing task comments: %w", err)
	}
	defer rows.Close()

	var comments []models.TaskComment
	for rows.Next() {
		comment, err := scanTaskComment(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning task comment: %w", err)
		}
		comments = append(comments, comment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating task comments: %w", err)
	}

	return comments, nil
}

func (s *service) CreateTaskBatchComment(ctx context.Context, batchID, authorID string, input models.CreateTaskBatchComment) (models.TaskBatchComment, error) {
	const query = `
		INSERT INTO todos.task_batch_comments (batch_id, author_id, body)
		VALUES ($1, $2, $3)
		RETURNING id::text, batch_id::text, author_id, body, created_at
	`

	comment, err := scanTaskBatchComment(s.db.QueryRowContext(ctx, query, batchID, authorID, input.Body))
	if err != nil {
		if isPgError(err, foreignKeyViolation) {
			return models.TaskBatchComment{}, ErrForeignKeyViolation
		}
		if isPgError(err, checkViolation) {
			return models.TaskBatchComment{}, ErrCheckViolation
		}
		return models.TaskBatchComment{}, fmt.Errorf("creating task batch comment: %w", err)
	}

	return comment, nil
}

func (s *service) ListTaskBatchComments(ctx context.Context, batchID string) ([]models.TaskBatchComment, error) {
	const query = `
		SELECT id::text, batch_id::text, author_id, body, created_at
		FROM todos.task_batch_comments
		WHERE batch_id = $1
		ORDER BY created_at ASC
	`

	rows, err := s.db.QueryContext(ctx, query, batchID)
	if err != nil {
		return nil, fmt.Errorf("listing task batch comments: %w", err)
	}
	defer rows.Close()

	var comments []models.TaskBatchComment
	for rows.Next() {
		comment, err := scanTaskBatchComment(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning task batch comment: %w", err)
		}
		comments = append(comments, comment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating task batch comments: %w", err)
	}

	return comments, nil
}

func (s *service) CreateTaskAttachment(ctx context.Context, scope, targetID, uploadedBy string, input models.CreateTaskAttachment) (models.TaskAttachment, error) {
	var batchID, taskInstanceID sql.NullString
	switch scope {
	case models.AttachmentScopeBatch:
		batchID = sql.NullString{String: targetID, Valid: true}
	case models.AttachmentScopeInstance:
		taskInstanceID = sql.NullString{String: targetID, Valid: true}
	default:
		return models.TaskAttachment{}, ErrCheckViolation
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.TaskAttachment{}, fmt.Errorf("beginning task attachment transaction: %w", err)
	}
	defer tx.Rollback()

	const query = `
		INSERT INTO todos.task_attachments (
			scope,
			batch_id,
			task_instance_id,
			uploaded_by,
			file_url,
			file_name,
			mime_type,
			size_bytes
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		RETURNING id::text, scope, batch_id::text, task_instance_id::text,
		          uploaded_by, file_url, file_name, mime_type, size_bytes, created_at
	`

	attachment, err := scanTaskAttachment(tx.QueryRowContext(ctx, query,
		scope,
		batchID,
		taskInstanceID,
		uploadedBy,
		input.FileURL,
		NullString(input.FileName),
		NullString(input.MimeType),
		NullInt64(input.SizeBytes),
	))
	if err != nil {
		if isPgError(err, foreignKeyViolation) {
			return models.TaskAttachment{}, ErrForeignKeyViolation
		}
		if isPgError(err, checkViolation) {
			return models.TaskAttachment{}, ErrCheckViolation
		}
		return models.TaskAttachment{}, fmt.Errorf("creating task attachment: %w", err)
	}

	if scope == models.AttachmentScopeInstance {
		if err := createTaskInstanceEvent(ctx, tx, targetID, uploadedBy, "file_uploaded", nil, map[string]any{
			"attachment_id": attachment.ID,
			"file_url":      attachment.FileURL,
		}); err != nil {
			return models.TaskAttachment{}, err
		}
	}

	if err := tx.Commit(); err != nil {
		return models.TaskAttachment{}, fmt.Errorf("committing task attachment transaction: %w", err)
	}

	return attachment, nil
}

func (s *service) ListTaskAttachments(ctx context.Context, scope, targetID string) ([]models.TaskAttachment, error) {
	column, err := attachmentTargetColumn(scope)
	if err != nil {
		return nil, err
	}

	query := `
		SELECT id::text, scope, batch_id::text, task_instance_id::text,
		       uploaded_by, file_url, file_name, mime_type, size_bytes, created_at
		FROM todos.task_attachments
		WHERE scope = $1
		  AND ` + column + ` = $2
		ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, scope, targetID)
	if err != nil {
		return nil, fmt.Errorf("listing task attachments: %w", err)
	}
	defer rows.Close()

	var attachments []models.TaskAttachment
	for rows.Next() {
		attachment, err := scanTaskAttachment(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning task attachment: %w", err)
		}
		attachments = append(attachments, attachment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating task attachments: %w", err)
	}

	return attachments, nil
}

func (s *service) GetTaskSubmission(ctx context.Context, submissionID string) (models.TaskSubmission, error) {
	return getTaskSubmission(ctx, s.db, submissionID)
}

func (s *service) CreateTaskSubmission(ctx context.Context, taskInstanceID, submittedBy string, input models.CreateTaskSubmission) (models.TaskSubmission, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.TaskSubmission{}, fmt.Errorf("beginning task submission transaction: %w", err)
	}
	defer tx.Rollback()

	oldInstance, err := getTaskInstance(ctx, tx, taskInstanceID)
	if err != nil {
		return models.TaskSubmission{}, err
	}
	shouldRequestReview := oldInstance.ReviewRequired && !models.IsTerminalTaskStatus(oldInstance.Status)
	if shouldRequestReview {
		if err := enforceTaskDependenciesForStatus(ctx, tx, taskInstanceID, models.TaskStatusCompleted); err != nil {
			return models.TaskSubmission{}, err
		}
	}

	submission, err := createTaskSubmission(ctx, tx, taskInstanceID, submittedBy, input)
	if err != nil {
		return models.TaskSubmission{}, err
	}

	if shouldRequestReview {
		updated, err := setTaskInstancePendingReview(ctx, tx, taskInstanceID, input.Note)
		if err != nil {
			return models.TaskSubmission{}, err
		}
		if oldInstance.Status != updated.Status {
			if err := createTaskInstanceEvent(ctx, tx, taskInstanceID, submittedBy, "status_changed", map[string]any{
				"status": oldInstance.Status,
			}, map[string]any{
				"status":        updated.Status,
				"submission_id": submission.ID,
			}); err != nil {
				return models.TaskSubmission{}, err
			}
		}
	}

	if err := createTaskInstanceEvent(ctx, tx, taskInstanceID, submittedBy, "submission_added", nil, map[string]any{
		"submission_id": submission.ID,
		"status":        submission.Status,
	}); err != nil {
		return models.TaskSubmission{}, err
	}

	if err := tx.Commit(); err != nil {
		return models.TaskSubmission{}, fmt.Errorf("committing task submission transaction: %w", err)
	}

	return submission, nil
}

func (s *service) ListTaskSubmissions(ctx context.Context, taskInstanceID string) ([]models.TaskSubmission, error) {
	const query = `
		SELECT id::text, task_instance_id::text, submitted_by, note, status,
		       created_at, reviewed_at, reviewed_by
		FROM todos.task_submissions
		WHERE task_instance_id = $1
		ORDER BY created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, taskInstanceID)
	if err != nil {
		return nil, fmt.Errorf("listing task submissions: %w", err)
	}
	defer rows.Close()

	var submissions []models.TaskSubmission
	for rows.Next() {
		submission, err := scanTaskSubmission(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning task submission: %w", err)
		}
		submissions = append(submissions, submission)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating task submissions: %w", err)
	}

	return submissions, nil
}

func (s *service) ReviewTaskSubmission(ctx context.Context, submissionID, reviewerID string, input models.ReviewTaskSubmission) (models.TaskSubmission, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return models.TaskSubmission{}, fmt.Errorf("beginning task submission review transaction: %w", err)
	}
	defer tx.Rollback()

	oldSubmission, err := getTaskSubmission(ctx, tx, submissionID)
	if err != nil {
		return models.TaskSubmission{}, err
	}

	oldInstance, err := getTaskInstance(ctx, tx, oldSubmission.TaskInstanceID)
	if err != nil {
		return models.TaskSubmission{}, err
	}

	const query = `
		UPDATE todos.task_submissions
		SET status = $2,
		    reviewed_at = CURRENT_TIMESTAMP,
		    reviewed_by = $3
		WHERE id = $1
		RETURNING id::text, task_instance_id::text, submitted_by, note, status,
		          created_at, reviewed_at, reviewed_by
	`

	submission, err := scanTaskSubmission(tx.QueryRowContext(ctx, query, submissionID, input.Status, reviewerID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.TaskSubmission{}, ErrDBNotFound
		}
		if isPgError(err, foreignKeyViolation) {
			return models.TaskSubmission{}, ErrForeignKeyViolation
		}
		if isPgError(err, checkViolation) {
			return models.TaskSubmission{}, ErrCheckViolation
		}
		return models.TaskSubmission{}, fmt.Errorf("reviewing task submission: %w", err)
	}

	if oldInstance.ReviewRequired {
		updatedInstance, err := applyTaskSubmissionReviewToTaskInstance(ctx, tx, oldInstance, submission)
		if err != nil {
			return models.TaskSubmission{}, err
		}
		if oldInstance.Status != updatedInstance.Status {
			if err := createTaskInstanceEvent(ctx, tx, submission.TaskInstanceID, reviewerID, "status_changed", map[string]any{
				"status": oldInstance.Status,
			}, map[string]any{
				"status":        updatedInstance.Status,
				"submission_id": submission.ID,
			}); err != nil {
				return models.TaskSubmission{}, err
			}
		}
	}

	if err := createTaskInstanceEvent(ctx, tx, submission.TaskInstanceID, reviewerID, "submission_reviewed", map[string]any{
		"submission_id": oldSubmission.ID,
		"status":        oldSubmission.Status,
	}, map[string]any{
		"submission_id": submission.ID,
		"status":        submission.Status,
	}); err != nil {
		return models.TaskSubmission{}, err
	}

	if err := tx.Commit(); err != nil {
		return models.TaskSubmission{}, fmt.Errorf("committing task submission review transaction: %w", err)
	}

	return submission, nil
}

func getExistingBatchIDByIdempotencyKey(ctx context.Context, runner sqlRunner, creatorID, idempotencyKey string) (string, error) {
	const query = `
		SELECT id::text
		FROM todos.task_batches
		WHERE created_by = $1
		  AND idempotency_key = $2
	`

	var batchID string
	if err := runner.QueryRowContext(ctx, query, creatorID, idempotencyKey).Scan(&batchID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrDBNotFound
		}
		return "", fmt.Errorf("getting existing task batch by idempotency key: %w", err)
	}

	return batchID, nil
}

func getTaskBatchCreateResult(ctx context.Context, runner sqlRunner, batchID string) (models.TaskBatchCreateResult, error) {
	batch, err := getTaskBatch(ctx, runner, batchID)
	if err != nil {
		return models.TaskBatchCreateResult{}, err
	}

	instances, err := listTaskBatchInstances(ctx, runner, batchID)
	if err != nil {
		return models.TaskBatchCreateResult{}, err
	}

	dependencies, err := listTaskBatchDependencies(ctx, runner, batchID)
	if err != nil {
		return models.TaskBatchCreateResult{}, err
	}

	result := models.TaskBatchCreateResult{
		Batch:          batch,
		Instances:      instances,
		Dependencies:   dependencies,
		TotalInstances: len(instances),
	}
	return populateTaskBatchCreateResultAssignees(ctx, runner, result)
}

func getTaskBatch(ctx context.Context, runner sqlRunner, batchID string) (models.TaskBatch, error) {
	const query = `
		SELECT id::text, created_by, title, description, instructions, priority,
		       due_at, review_required, idempotency_key, metadata::text, created_at
		FROM todos.task_batches
		WHERE id = $1
	`

	batch, err := scanTaskBatch(runner.QueryRowContext(ctx, query, batchID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.TaskBatch{}, ErrDBNotFound
		}
		return models.TaskBatch{}, fmt.Errorf("getting task batch: %w", err)
	}

	return batch, nil
}

func createTaskInstanceForAssignment(ctx context.Context, runner sqlRunner, creatorID string, batch models.TaskBatch, assignment models.TaskAssignmentInput) (models.TaskInstance, error) {
	resolved := resolveAssignment(batch, assignment)
	customPayloadJSON, err := marshalJSONMap(assignment.CustomPayload)
	if err != nil {
		return models.TaskInstance{}, fmt.Errorf("encoding assignment custom payload: %w", err)
	}

	const query = `
		INSERT INTO todos.task_instances (
			batch_id,
			created_by,
			assignee_id,
			assignment_key,
			title,
			description,
			instructions,
			priority,
			due_at,
			status,
			review_required,
			custom_payload
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 'assigned', $10, $11::jsonb)
		RETURNING id::text, batch_id::text, created_by, assignee_id,
		          assignment_key, title, description, instructions, priority, due_at, status,
		          review_required, progress_percent, started_at, completed_at, cancelled_at, completion_note,
		          custom_payload::text,
		          replaced_by_task_instance_id::text, replaces_task_instance_id::text,
		          created_at, updated_at
	`

	instance, err := scanTaskInstance(runner.QueryRowContext(ctx, query,
		batch.ID,
		creatorID,
		assignment.AssigneeID,
		NullString(assignment.AssignmentKey),
		resolved.Title,
		NullString(resolved.Description),
		NullString(resolved.Instructions),
		NullString(resolved.Priority),
		NullTime(resolved.DueAt),
		resolved.ReviewRequired,
		customPayloadJSON,
	))
	if err != nil {
		if isPgError(err, foreignKeyViolation) {
			return models.TaskInstance{}, ErrForeignKeyViolation
		}
		if isPgError(err, uniqueViolation) {
			return models.TaskInstance{}, ErrDBDuplicatedEntry
		}
		if isPgError(err, checkViolation) || isPgError(err, notNullViolation) {
			return models.TaskInstance{}, ErrCheckViolation
		}
		return models.TaskInstance{}, fmt.Errorf("creating task instance: %w", err)
	}

	return instance, nil
}

func getTaskInstance(ctx context.Context, runner sqlRunner, taskInstanceID string) (models.TaskInstance, error) {
	const query = `
		SELECT id::text, batch_id::text, created_by, assignee_id,
		       assignment_key, title, description, instructions, priority, due_at, status,
		       review_required, progress_percent, started_at, completed_at, cancelled_at, completion_note,
		       custom_payload::text,
		       replaced_by_task_instance_id::text, replaces_task_instance_id::text,
		       created_at, updated_at
		FROM todos.task_instances
		WHERE id = $1
	`

	instance, err := scanTaskInstance(runner.QueryRowContext(ctx, query, taskInstanceID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.TaskInstance{}, ErrDBNotFound
		}
		return models.TaskInstance{}, fmt.Errorf("getting task instance: %w", err)
	}

	return instance, nil
}

func listTaskBatchInstances(ctx context.Context, runner sqlRunner, batchID string) ([]models.TaskInstance, error) {
	const query = `
		SELECT id::text, batch_id::text, created_by, assignee_id,
		       assignment_key, title, description, instructions, priority, due_at, status,
		       review_required, progress_percent, started_at, completed_at, cancelled_at, completion_note,
		       custom_payload::text,
		       replaced_by_task_instance_id::text, replaces_task_instance_id::text,
		       created_at, updated_at
		FROM todos.task_instances
		WHERE batch_id = $1
		ORDER BY created_at ASC
	`

	rows, err := runner.QueryContext(ctx, query, batchID)
	if err != nil {
		return nil, fmt.Errorf("listing task batch instances: %w", err)
	}
	defer rows.Close()

	return scanTaskInstances(rows)
}

func populateTaskBatchCreateResultAssignees(ctx context.Context, runner sqlRunner, result models.TaskBatchCreateResult) (models.TaskBatchCreateResult, error) {
	assignees, err := listTaskBatchAssignees(ctx, runner, result.Batch.ID)
	if err != nil {
		return models.TaskBatchCreateResult{}, err
	}

	result.Batch.Assignees = assignees
	result.Instances = applyAssigneesToTaskInstances(result.Instances, assignees)

	return result, nil
}

func populateTaskBatchAssignees(ctx context.Context, runner sqlRunner, batch models.TaskBatch) (models.TaskBatch, error) {
	assignees, err := listTaskBatchAssignees(ctx, runner, batch.ID)
	if err != nil {
		return models.TaskBatch{}, err
	}

	batch.Assignees = assignees
	return batch, nil
}

func populateTaskInstanceAssignees(ctx context.Context, runner sqlRunner, instance models.TaskInstance) (models.TaskInstance, error) {
	assignees, err := listTaskBatchAssignees(ctx, runner, instance.BatchID)
	if err != nil {
		return models.TaskInstance{}, err
	}

	instance.Assignees = assignees
	return instance, nil
}

func populateTaskInstancesAssignees(ctx context.Context, runner sqlRunner, instances []models.TaskInstance) ([]models.TaskInstance, error) {
	if len(instances) == 0 {
		return instances, nil
	}

	batchIDs := make([]string, 0, len(instances))
	seenBatchIDs := make(map[string]struct{}, len(instances))
	for _, instance := range instances {
		if _, seen := seenBatchIDs[instance.BatchID]; seen {
			continue
		}
		seenBatchIDs[instance.BatchID] = struct{}{}
		batchIDs = append(batchIDs, instance.BatchID)
	}

	assigneesByBatchID, err := listTaskAssigneesByBatchIDs(ctx, runner, batchIDs)
	if err != nil {
		return nil, err
	}

	for i := range instances {
		assignees := assigneesByBatchID[instances[i].BatchID]
		if assignees == nil {
			assignees = []models.TaskAssignee{}
		}
		instances[i].Assignees = assignees
	}

	return instances, nil
}

func applyAssigneesToTaskInstances(instances []models.TaskInstance, assignees []models.TaskAssignee) []models.TaskInstance {
	if assignees == nil {
		assignees = []models.TaskAssignee{}
	}
	for i := range instances {
		instances[i].Assignees = assignees
	}
	return instances
}

func listTaskBatchAssignees(ctx context.Context, runner sqlRunner, batchID string) ([]models.TaskAssignee, error) {
	assigneesByBatchID, err := listTaskAssigneesByBatchIDs(ctx, runner, []string{batchID})
	if err != nil {
		return nil, err
	}

	assignees := assigneesByBatchID[batchID]
	if assignees == nil {
		return []models.TaskAssignee{}, nil
	}
	return assignees, nil
}

func listTaskAssigneesByBatchIDs(ctx context.Context, runner sqlRunner, batchIDs []string) (map[string][]models.TaskAssignee, error) {
	assigneesByBatchID := make(map[string][]models.TaskAssignee, len(batchIDs))
	if len(batchIDs) == 0 {
		return assigneesByBatchID, nil
	}

	placeholders := make([]string, 0, len(batchIDs))
	args := make([]any, 0, len(batchIDs))
	for i, batchID := range batchIDs {
		placeholders = append(placeholders, fmt.Sprintf("$%d", i+1))
		args = append(args, batchID)
		assigneesByBatchID[batchID] = []models.TaskAssignee{}
	}

	query := `
		SELECT ti.id::text, ti.batch_id::text, ti.assignee_id, ti.assignment_key, ti.status,
		       u.id::text, u.name, u.account, u.email, u.bio, u.dob, u.city, u.phone,
		       u.avatar_photo_id, u.is_admin, u.created_at, u.updated_at,
		       ti.created_at, ti.updated_at
		FROM todos.task_instances ti
		JOIN todos.users u ON u.id = ti.assignee_id
		WHERE ti.batch_id::text IN (` + strings.Join(placeholders, ", ") + `)
		ORDER BY ti.batch_id::text, ti.created_at ASC
	`

	rows, err := runner.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing task assignees: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		assignee, err := scanTaskAssignee(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning task assignee: %w", err)
		}
		assigneesByBatchID[assignee.BatchID] = append(assigneesByBatchID[assignee.BatchID], assignee)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating task assignees: %w", err)
	}

	return assigneesByBatchID, nil
}

func createTaskBatchDependencies(ctx context.Context, runner sqlRunner, creatorID string, inputs []models.TaskBatchDependency, instancesByAssignmentKey map[string]models.TaskInstance) ([]models.TaskInstanceDependency, error) {
	if len(inputs) == 0 {
		return nil, nil
	}

	dependencies := make([]models.TaskInstanceDependency, 0, len(inputs))
	for _, input := range inputs {
		instance, ok := instancesByAssignmentKey[input.AssignmentKey]
		if !ok {
			return nil, ErrForeignKeyViolation
		}
		dependsOnInstance, ok := instancesByAssignmentKey[input.DependsOnAssignmentKey]
		if !ok {
			return nil, ErrForeignKeyViolation
		}

		dependencyType, err := normalizeTaskDependencyType(input.DependencyType)
		if err != nil {
			return nil, err
		}

		dependency, err := createTaskInstanceDependency(ctx, runner, instance.ID, dependsOnInstance.ID, creatorID, dependencyType)
		if err != nil {
			return nil, err
		}
		if err := createTaskInstanceEvent(ctx, runner, instance.ID, creatorID, "dependency_added", nil, map[string]any{
			"dependency_id":               dependency.ID,
			"depends_on_task_instance_id": dependency.DependsOnTaskInstanceID,
			"dependency_type":             dependency.DependencyType,
		}); err != nil {
			return nil, err
		}

		dependencies = append(dependencies, dependency)
	}

	return dependencies, nil
}

func createTaskInstanceDependency(ctx context.Context, runner sqlRunner, taskInstanceID, dependsOnTaskInstanceID, creatorID, dependencyType string) (models.TaskInstanceDependency, error) {
	if err := validateTaskInstanceDependency(ctx, runner, taskInstanceID, dependsOnTaskInstanceID); err != nil {
		return models.TaskInstanceDependency{}, err
	}

	const query = `
		INSERT INTO todos.task_instance_dependencies (
			task_instance_id,
			depends_on_task_instance_id,
			dependency_type,
			created_by
		)
		VALUES ($1, $2, $3, $4)
		RETURNING id::text, task_instance_id::text, depends_on_task_instance_id::text,
		          dependency_type, created_by, created_at
	`

	dependency, err := scanTaskInstanceDependency(runner.QueryRowContext(ctx, query,
		taskInstanceID,
		dependsOnTaskInstanceID,
		dependencyType,
		creatorID,
	))
	if err != nil {
		if isPgError(err, uniqueViolation) {
			return models.TaskInstanceDependency{}, ErrDBDuplicatedEntry
		}
		if isPgError(err, foreignKeyViolation) {
			return models.TaskInstanceDependency{}, ErrForeignKeyViolation
		}
		if isPgError(err, checkViolation) || isPgError(err, notNullViolation) {
			return models.TaskInstanceDependency{}, ErrCheckViolation
		}
		return models.TaskInstanceDependency{}, fmt.Errorf("creating task instance dependency: %w", err)
	}

	return dependency, nil
}

func normalizeTaskDependencyType(dependencyType *string) (string, error) {
	resolved := models.DependencyTypeBlocksCompletion
	if dependencyType != nil {
		resolved = *dependencyType
	}
	if !models.IsValidTaskDependencyType(resolved) {
		return "", ErrCheckViolation
	}
	return resolved, nil
}

func validateTaskInstanceDependency(ctx context.Context, runner sqlRunner, taskInstanceID, dependsOnTaskInstanceID string) error {
	if taskInstanceID == dependsOnTaskInstanceID {
		return ErrCheckViolation
	}

	const batchQuery = `
		SELECT task.batch_id::text, depends_on.batch_id::text
		FROM todos.task_instances task
		CROSS JOIN todos.task_instances depends_on
		WHERE task.id = $1
		  AND depends_on.id = $2
	`

	var taskBatchID, dependsOnBatchID string
	if err := runner.QueryRowContext(ctx, batchQuery, taskInstanceID, dependsOnTaskInstanceID).Scan(&taskBatchID, &dependsOnBatchID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrForeignKeyViolation
		}
		return fmt.Errorf("validating task dependency batches: %w", err)
	}
	if taskBatchID != dependsOnBatchID {
		return ErrCheckViolation
	}

	cycle, err := taskDependencyWouldCreateCycle(ctx, runner, taskInstanceID, dependsOnTaskInstanceID)
	if err != nil {
		return err
	}
	if cycle {
		return ErrTaskDependencyCycle
	}

	return nil
}

func taskDependencyWouldCreateCycle(ctx context.Context, runner sqlRunner, taskInstanceID, dependsOnTaskInstanceID string) (bool, error) {
	const query = `
		WITH RECURSIVE dependency_chain(depends_on_task_instance_id) AS (
			SELECT depends_on_task_instance_id
			FROM todos.task_instance_dependencies
			WHERE task_instance_id = $2

			UNION

			SELECT dependency.depends_on_task_instance_id
			FROM todos.task_instance_dependencies dependency
			INNER JOIN dependency_chain chain
				ON dependency.task_instance_id = chain.depends_on_task_instance_id
		)
		SELECT EXISTS (
			SELECT 1
			FROM dependency_chain
			WHERE depends_on_task_instance_id = $1
		)
	`

	var cycle bool
	if err := runner.QueryRowContext(ctx, query, taskInstanceID, dependsOnTaskInstanceID).Scan(&cycle); err != nil {
		return false, fmt.Errorf("checking task dependency cycle: %w", err)
	}
	return cycle, nil
}

func enforceTaskDependenciesForStatus(ctx context.Context, runner sqlRunner, taskInstanceID, status string) error {
	switch status {
	case models.TaskStatusInProgress:
		return enforceTaskDependencyType(ctx, runner, taskInstanceID, models.DependencyTypeBlocksStart)
	case models.TaskStatusCompleted:
		if err := enforceTaskDependencyType(ctx, runner, taskInstanceID, models.DependencyTypeBlocksStart); err != nil {
			return err
		}
		return enforceTaskDependencyType(ctx, runner, taskInstanceID, models.DependencyTypeBlocksCompletion)
	default:
		return nil
	}
}

func enforceTaskDependencyType(ctx context.Context, runner sqlRunner, taskInstanceID, dependencyType string) error {
	const query = `
		SELECT EXISTS (
			SELECT 1
			FROM todos.task_instance_dependencies dependency
			INNER JOIN todos.task_instances depends_on
				ON depends_on.id = dependency.depends_on_task_instance_id
			WHERE dependency.task_instance_id = $1
			  AND dependency.dependency_type = $2
			  AND depends_on.status <> 'completed'
		)
	`

	var blocked bool
	if err := runner.QueryRowContext(ctx, query, taskInstanceID, dependencyType).Scan(&blocked); err != nil {
		return fmt.Errorf("checking task dependency status: %w", err)
	}
	if blocked {
		return ErrTaskBlockedByDeps
	}

	return nil
}

func listTaskBatchDependencies(ctx context.Context, runner sqlRunner, batchID string) ([]models.TaskInstanceDependency, error) {
	const query = `
		SELECT dependency.id::text, dependency.task_instance_id::text,
		       dependency.depends_on_task_instance_id::text, dependency.dependency_type,
		       dependency.created_by, dependency.created_at
		FROM todos.task_instance_dependencies dependency
		INNER JOIN todos.task_instances task
			ON task.id = dependency.task_instance_id
		WHERE task.batch_id = $1
		ORDER BY dependency.created_at ASC
	`

	rows, err := runner.QueryContext(ctx, query, batchID)
	if err != nil {
		return nil, fmt.Errorf("listing task batch dependencies: %w", err)
	}
	defer rows.Close()

	return scanTaskInstanceDependencies(rows)
}

func listTaskInstanceDependencies(ctx context.Context, runner sqlRunner, taskInstanceID string) ([]models.TaskInstanceDependency, error) {
	const query = `
		SELECT id::text, task_instance_id::text, depends_on_task_instance_id::text,
		       dependency_type, created_by, created_at
		FROM todos.task_instance_dependencies
		WHERE task_instance_id = $1
		ORDER BY created_at ASC
	`

	rows, err := runner.QueryContext(ctx, query, taskInstanceID)
	if err != nil {
		return nil, fmt.Errorf("listing task instance dependencies: %w", err)
	}
	defer rows.Close()

	return scanTaskInstanceDependencies(rows)
}

func listTaskInstanceDependents(ctx context.Context, runner sqlRunner, taskInstanceID string) ([]models.TaskInstanceDependency, error) {
	const query = `
		SELECT id::text, task_instance_id::text, depends_on_task_instance_id::text,
		       dependency_type, created_by, created_at
		FROM todos.task_instance_dependencies
		WHERE depends_on_task_instance_id = $1
		ORDER BY created_at ASC
	`

	rows, err := runner.QueryContext(ctx, query, taskInstanceID)
	if err != nil {
		return nil, fmt.Errorf("listing task instance dependents: %w", err)
	}
	defer rows.Close()

	return scanTaskInstanceDependencies(rows)
}

func createTaskInstanceEvent(ctx context.Context, runner sqlRunner, taskInstanceID, actorID, eventType string, oldValue, newValue map[string]any) error {
	oldValueJSON, err := nullableJSONMapParam(oldValue)
	if err != nil {
		return fmt.Errorf("encoding old event value: %w", err)
	}
	newValueJSON, err := nullableJSONMapParam(newValue)
	if err != nil {
		return fmt.Errorf("encoding new event value: %w", err)
	}

	const query = `
		INSERT INTO todos.task_instance_events (
			task_instance_id,
			actor_id,
			event_type,
			old_value,
			new_value
		)
		VALUES ($1, $2, $3, $4::jsonb, $5::jsonb)
	`

	if _, err := runner.ExecContext(ctx, query, taskInstanceID, actorID, eventType, oldValueJSON, newValueJSON); err != nil {
		if isPgError(err, foreignKeyViolation) {
			return ErrForeignKeyViolation
		}
		if isPgError(err, checkViolation) {
			return ErrCheckViolation
		}
		return fmt.Errorf("creating task instance event: %w", err)
	}

	return nil
}

func getTaskSubmission(ctx context.Context, runner sqlRunner, submissionID string) (models.TaskSubmission, error) {
	const query = `
		SELECT id::text, task_instance_id::text, submitted_by, note, status,
		       created_at, reviewed_at, reviewed_by
		FROM todos.task_submissions
		WHERE id = $1
	`

	submission, err := scanTaskSubmission(runner.QueryRowContext(ctx, query, submissionID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.TaskSubmission{}, ErrDBNotFound
		}
		return models.TaskSubmission{}, fmt.Errorf("getting task submission: %w", err)
	}

	return submission, nil
}

func createTaskSubmission(ctx context.Context, runner sqlRunner, taskInstanceID, submittedBy string, input models.CreateTaskSubmission) (models.TaskSubmission, error) {
	const query = `
		INSERT INTO todos.task_submissions (task_instance_id, submitted_by, note)
		VALUES ($1, $2, $3)
		RETURNING id::text, task_instance_id::text, submitted_by, note, status,
		          created_at, reviewed_at, reviewed_by
	`

	submission, err := scanTaskSubmission(runner.QueryRowContext(ctx, query, taskInstanceID, submittedBy, NullString(input.Note)))
	if err != nil {
		if isPgError(err, foreignKeyViolation) {
			return models.TaskSubmission{}, ErrForeignKeyViolation
		}
		return models.TaskSubmission{}, fmt.Errorf("creating task submission: %w", err)
	}

	return submission, nil
}

func setTaskInstancePendingReview(ctx context.Context, runner sqlRunner, taskInstanceID string, note *string) (models.TaskInstance, error) {
	const query = `
		UPDATE todos.task_instances
		SET status = 'pending_review',
		    completion_note = COALESCE($2, completion_note),
		    started_at = COALESCE(started_at, CURRENT_TIMESTAMP),
		    completed_at = NULL,
		    cancelled_at = NULL,
		    progress_percent = $3,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING id::text, batch_id::text, created_by, assignee_id,
		          assignment_key, title, description, instructions, priority, due_at, status,
		          review_required, progress_percent, started_at, completed_at, cancelled_at, completion_note,
		          custom_payload::text,
		          replaced_by_task_instance_id::text, replaces_task_instance_id::text,
		          created_at, updated_at
	`

	instance, err := scanTaskInstance(runner.QueryRowContext(ctx, query, taskInstanceID, NullString(note), models.ProgressForStatus(models.TaskStatusPendingReview)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.TaskInstance{}, ErrDBNotFound
		}
		if isPgError(err, checkViolation) {
			return models.TaskInstance{}, ErrCheckViolation
		}
		return models.TaskInstance{}, fmt.Errorf("setting task instance pending review: %w", err)
	}

	return instance, nil
}

func applyTaskSubmissionReviewToTaskInstance(ctx context.Context, runner sqlRunner, instance models.TaskInstance, submission models.TaskSubmission) (models.TaskInstance, error) {
	status := models.TaskStatusInProgress
	if submission.Status == models.SubmissionStatusAccepted {
		status = models.TaskStatusCompleted
	}

	const query = `
		UPDATE todos.task_instances
		SET status = $2,
		    completion_note = CASE
		        WHEN $2 = 'completed' THEN COALESCE($3, completion_note)
		        ELSE completion_note
		    END,
		    started_at = CASE
		        WHEN started_at IS NULL THEN CURRENT_TIMESTAMP
		        ELSE started_at
		    END,
		    completed_at = CASE
		        WHEN $2 = 'completed' THEN CURRENT_TIMESTAMP
		        ELSE NULL
		    END,
		    cancelled_at = NULL,
		    progress_percent = $4,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING id::text, batch_id::text, created_by, assignee_id,
		          assignment_key, title, description, instructions, priority, due_at, status,
		          review_required, progress_percent, started_at, completed_at, cancelled_at, completion_note,
		          custom_payload::text,
		          replaced_by_task_instance_id::text, replaces_task_instance_id::text,
		          created_at, updated_at
	`

	updated, err := scanTaskInstance(runner.QueryRowContext(ctx, query, instance.ID, status, NullString(submission.Note), models.ProgressForStatus(status)))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.TaskInstance{}, ErrDBNotFound
		}
		if isPgError(err, checkViolation) {
			return models.TaskInstance{}, ErrCheckViolation
		}
		return models.TaskInstance{}, fmt.Errorf("updating task instance after submission review: %w", err)
	}

	return updated, nil
}

type resolvedAssignment struct {
	Title          string
	Description    *string
	Instructions   *string
	Priority       *string
	DueAt          *time.Time
	ReviewRequired bool
}

func resolveAssignment(batch models.TaskBatch, assignment models.TaskAssignmentInput) resolvedAssignment {
	resolved := resolvedAssignment{
		Title:          batch.Title,
		Description:    batch.Description,
		Instructions:   batch.Instructions,
		Priority:       batch.Priority,
		DueAt:          batch.DueAt,
		ReviewRequired: batch.ReviewRequired,
	}

	if assignment.Overrides == nil {
		return resolved
	}
	if assignment.Overrides.Title != nil {
		resolved.Title = *assignment.Overrides.Title
	}
	if assignment.Overrides.Description != nil {
		resolved.Description = assignment.Overrides.Description
	}
	if assignment.Overrides.Instructions != nil {
		resolved.Instructions = assignment.Overrides.Instructions
	}
	if assignment.Overrides.Priority != nil {
		resolved.Priority = assignment.Overrides.Priority
	}
	if assignment.Overrides.DueAt != nil {
		resolved.DueAt = assignment.Overrides.DueAt
	}
	if assignment.Overrides.ReviewRequired != nil {
		resolved.ReviewRequired = *assignment.Overrides.ReviewRequired
	}

	return resolved
}

func withCreatorAssignment(creatorID string, assignments []models.TaskAssignmentInput) []models.TaskAssignmentInput {
	for _, assignment := range assignments {
		if assignment.AssigneeID == creatorID {
			return assignments
		}
	}

	normalized := make([]models.TaskAssignmentInput, 0, len(assignments)+1)
	normalized = append(normalized, models.TaskAssignmentInput{AssigneeID: creatorID})
	normalized = append(normalized, assignments...)
	return normalized
}

func deriveBatchStatus(total int, summary map[string]int) string {
	if total == 0 {
		return "empty"
	}
	if summary["cancelled"] == total {
		return "cancelled"
	}
	if summary["overdue"] > 0 {
		return "overdue"
	}
	if summary["pending_review"] > 0 {
		return "attention_required"
	}
	if summary["blocked"] > 0 {
		return "attention_required"
	}
	if summary["completed"] == total {
		return "completed"
	}
	if summary["completed"] > 0 {
		return "partially_completed"
	}
	return "pending"
}

func attachmentTargetColumn(scope string) (string, error) {
	switch scope {
	case models.AttachmentScopeBatch:
		return "batch_id", nil
	case models.AttachmentScopeInstance:
		return "task_instance_id", nil
	default:
		return "", ErrCheckViolation
	}
}

func taskInstanceUpdateEventValue(instance models.TaskInstance, input models.UpdateTaskInstance) map[string]any {
	value := map[string]any{}
	if input.Title != nil {
		value["title"] = instance.Title
	}
	if input.Description != nil {
		value["description"] = instance.Description
	}
	if input.Instructions != nil {
		value["instructions"] = instance.Instructions
	}
	if input.Priority != nil {
		value["priority"] = instance.Priority
	}
	if input.DueAt != nil {
		value["due_at"] = instance.DueAt
	}
	if input.ReviewRequired != nil {
		value["review_required"] = instance.ReviewRequired
	}
	if input.CustomPayload != nil {
		value["custom_payload"] = instance.CustomPayload
	}
	return value
}

func scanTaskBatch(scanner rowScanner) (models.TaskBatch, error) {
	var batch models.TaskBatch
	var description, instructions, priority, idempotencyKey sql.NullString
	var dueAt sql.NullTime
	var metadata string

	if err := scanner.Scan(
		&batch.ID,
		&batch.CreatedBy,
		&batch.Title,
		&description,
		&instructions,
		&priority,
		&dueAt,
		&batch.ReviewRequired,
		&idempotencyKey,
		&metadata,
		&batch.CreatedAt,
	); err != nil {
		return models.TaskBatch{}, err
	}

	metadataMap, err := jsonMapFromString(metadata)
	if err != nil {
		return models.TaskBatch{}, err
	}

	batch.Description = StringPtr(description)
	batch.Instructions = StringPtr(instructions)
	batch.Priority = StringPtr(priority)
	batch.DueAt = TimePtr(dueAt)
	batch.IdempotencyKey = StringPtr(idempotencyKey)
	batch.Metadata = metadataMap
	batch.Assignees = []models.TaskAssignee{}

	return batch, nil
}

func scanTaskInstance(scanner rowScanner) (models.TaskInstance, error) {
	var instance models.TaskInstance
	var assignmentKey, description, instructions, priority, completionNote sql.NullString
	var replacedByTaskInstanceID, replacesTaskInstanceID sql.NullString
	var dueAt, startedAt, completedAt, cancelledAt sql.NullTime
	var customPayload string

	if err := scanner.Scan(
		&instance.ID,
		&instance.BatchID,
		&instance.CreatedBy,
		&instance.AssigneeID,
		&assignmentKey,
		&instance.Title,
		&description,
		&instructions,
		&priority,
		&dueAt,
		&instance.Status,
		&instance.ReviewRequired,
		&instance.ProgressPercent,
		&startedAt,
		&completedAt,
		&cancelledAt,
		&completionNote,
		&customPayload,
		&replacedByTaskInstanceID,
		&replacesTaskInstanceID,
		&instance.CreatedAt,
		&instance.UpdatedAt,
	); err != nil {
		return models.TaskInstance{}, err
	}

	customPayloadMap, err := jsonMapFromString(customPayload)
	if err != nil {
		return models.TaskInstance{}, err
	}

	instance.AssignmentKey = StringPtr(assignmentKey)
	instance.Description = StringPtr(description)
	instance.Instructions = StringPtr(instructions)
	instance.Priority = StringPtr(priority)
	instance.DueAt = TimePtr(dueAt)
	instance.StartedAt = TimePtr(startedAt)
	instance.CompletedAt = TimePtr(completedAt)
	instance.CancelledAt = TimePtr(cancelledAt)
	instance.CompletionNote = StringPtr(completionNote)
	instance.CustomPayload = customPayloadMap
	instance.ReplacedByTaskInstanceID = StringPtr(replacedByTaskInstanceID)
	instance.ReplacesTaskInstanceID = StringPtr(replacesTaskInstanceID)
	instance.Assignees = []models.TaskAssignee{}

	return instance, nil
}

func scanTaskInstances(rows *sql.Rows) ([]models.TaskInstance, error) {
	var instances []models.TaskInstance
	for rows.Next() {
		instance, err := scanTaskInstance(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning task instance: %w", err)
		}
		instances = append(instances, instance)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating task instances: %w", err)
	}
	return instances, nil
}

func scanTaskAssignee(scanner rowScanner) (models.TaskAssignee, error) {
	var assignee models.TaskAssignee
	var user models.User
	var assignmentKey sql.NullString
	var bio, dob, city, phone sql.NullString
	var avatarPhotoID sql.NullInt32

	if err := scanner.Scan(
		&assignee.ID,
		&assignee.BatchID,
		&assignee.UserID,
		&assignmentKey,
		&assignee.Status,
		&user.ID,
		&user.Name,
		&user.Account,
		&user.Email,
		&bio,
		&dob,
		&city,
		&phone,
		&avatarPhotoID,
		&user.IsAdmin,
		&user.CreatedAt,
		&user.UpdatedAt,
		&assignee.CreatedAt,
		&assignee.UpdatedAt,
	); err != nil {
		return models.TaskAssignee{}, err
	}

	user.Bio = StringPtr(bio)
	user.DOB = StringPtr(dob)
	user.City = StringPtr(city)
	user.Phone = StringPtr(phone)
	user.AvatarPhotoID = Int32Ptr(avatarPhotoID)

	assignee.TaskInstanceID = assignee.ID
	assignee.AssignmentKey = StringPtr(assignmentKey)
	assignee.User = &user

	return assignee, nil
}

func scanTaskInstanceEvent(scanner rowScanner) (models.TaskInstanceEvent, error) {
	var event models.TaskInstanceEvent
	var oldValue, newValue sql.NullString

	if err := scanner.Scan(
		&event.ID,
		&event.TaskInstanceID,
		&event.ActorID,
		&event.EventType,
		&oldValue,
		&newValue,
		&event.CreatedAt,
	); err != nil {
		return models.TaskInstanceEvent{}, err
	}

	oldMap, err := jsonMapFromNullString(oldValue)
	if err != nil {
		return models.TaskInstanceEvent{}, err
	}
	newMap, err := jsonMapFromNullString(newValue)
	if err != nil {
		return models.TaskInstanceEvent{}, err
	}

	event.OldValue = oldMap
	event.NewValue = newMap

	return event, nil
}

func scanTaskInstanceDependency(scanner rowScanner) (models.TaskInstanceDependency, error) {
	var dependency models.TaskInstanceDependency
	if err := scanner.Scan(
		&dependency.ID,
		&dependency.TaskInstanceID,
		&dependency.DependsOnTaskInstanceID,
		&dependency.DependencyType,
		&dependency.CreatedBy,
		&dependency.CreatedAt,
	); err != nil {
		return models.TaskInstanceDependency{}, err
	}
	return dependency, nil
}

func scanTaskInstanceDependencies(rows *sql.Rows) ([]models.TaskInstanceDependency, error) {
	var dependencies []models.TaskInstanceDependency
	for rows.Next() {
		dependency, err := scanTaskInstanceDependency(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning task instance dependency: %w", err)
		}
		dependencies = append(dependencies, dependency)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating task instance dependencies: %w", err)
	}
	return dependencies, nil
}

func scanTaskComment(scanner rowScanner) (models.TaskComment, error) {
	var comment models.TaskComment
	var updatedAt sql.NullTime

	if err := scanner.Scan(
		&comment.ID,
		&comment.TaskInstanceID,
		&comment.AuthorID,
		&comment.Body,
		&comment.CreatedAt,
		&updatedAt,
	); err != nil {
		return models.TaskComment{}, err
	}

	comment.UpdatedAt = TimePtr(updatedAt)
	return comment, nil
}

func scanTaskBatchComment(scanner rowScanner) (models.TaskBatchComment, error) {
	var comment models.TaskBatchComment
	if err := scanner.Scan(
		&comment.ID,
		&comment.BatchID,
		&comment.AuthorID,
		&comment.Body,
		&comment.CreatedAt,
	); err != nil {
		return models.TaskBatchComment{}, err
	}
	return comment, nil
}

func scanTaskAttachment(scanner rowScanner) (models.TaskAttachment, error) {
	var attachment models.TaskAttachment
	var batchID, taskInstanceID, fileName, mimeType sql.NullString
	var sizeBytes sql.NullInt64

	if err := scanner.Scan(
		&attachment.ID,
		&attachment.Scope,
		&batchID,
		&taskInstanceID,
		&attachment.UploadedBy,
		&attachment.FileURL,
		&fileName,
		&mimeType,
		&sizeBytes,
		&attachment.CreatedAt,
	); err != nil {
		return models.TaskAttachment{}, err
	}

	attachment.BatchID = StringPtr(batchID)
	attachment.TaskInstanceID = StringPtr(taskInstanceID)
	attachment.FileName = StringPtr(fileName)
	attachment.MimeType = StringPtr(mimeType)
	attachment.SizeBytes = Int64Ptr(sizeBytes)

	return attachment, nil
}

func scanTaskSubmission(scanner rowScanner) (models.TaskSubmission, error) {
	var submission models.TaskSubmission
	var note, reviewedBy sql.NullString
	var reviewedAt sql.NullTime

	if err := scanner.Scan(
		&submission.ID,
		&submission.TaskInstanceID,
		&submission.SubmittedBy,
		&note,
		&submission.Status,
		&submission.CreatedAt,
		&reviewedAt,
		&reviewedBy,
	); err != nil {
		return models.TaskSubmission{}, err
	}

	submission.Note = StringPtr(note)
	submission.ReviewedAt = TimePtr(reviewedAt)
	submission.ReviewedBy = StringPtr(reviewedBy)

	return submission, nil
}

func marshalJSONMap(value map[string]any) ([]byte, error) {
	if value == nil {
		value = map[string]any{}
	}
	return json.Marshal(value)
}

func nullableJSONMapParam(value map[string]any) (any, error) {
	if value == nil {
		return nil, nil
	}
	return json.Marshal(value)
}

func jsonMapFromString(value string) (map[string]any, error) {
	if value == "" {
		return map[string]any{}, nil
	}

	var decoded map[string]any
	if err := json.Unmarshal([]byte(value), &decoded); err != nil {
		return nil, fmt.Errorf("decoding json map: %w", err)
	}
	if decoded == nil {
		decoded = map[string]any{}
	}

	return decoded, nil
}

func jsonMapFromNullString(value sql.NullString) (map[string]any, error) {
	if !value.Valid {
		return nil, nil
	}
	return jsonMapFromString(value.String)
}
