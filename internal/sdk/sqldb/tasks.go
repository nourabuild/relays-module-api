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

func (s *service) CreateTaskTemplate(ctx context.Context, creatorID string, input models.TaskTemplateInput) (models.TaskTemplate, error) {
	return createTaskTemplate(ctx, s.db, creatorID, input)
}

func (s *service) GetTaskTemplate(ctx context.Context, templateID string) (models.TaskTemplate, error) {
	return getTaskTemplate(ctx, s.db, templateID)
}

func (s *service) UpdateTaskTemplate(ctx context.Context, templateID string, input models.UpdateTaskTemplate) (models.TaskTemplate, error) {
	metadataJSON, err := nullableJSONMapParam(input.Metadata)
	if err != nil {
		return models.TaskTemplate{}, fmt.Errorf("encoding template metadata: %w", err)
	}

	const query = `
		UPDATE todos.task_templates
		SET title = COALESCE($2, title),
		    description = COALESCE($3, description),
		    instructions = COALESCE($4, instructions),
		    default_priority = COALESCE($5, default_priority),
		    default_due_at = COALESCE($6, default_due_at),
		    metadata = COALESCE($7::jsonb, metadata),
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		  AND archived_at IS NULL
		RETURNING id::text, created_by, title, description, instructions, default_priority,
		          default_due_at, metadata::text, created_at, updated_at, archived_at
	`

	template, err := scanTaskTemplate(s.db.QueryRowContext(ctx, query,
		templateID,
		NullString(input.Title),
		NullString(input.Description),
		NullString(input.Instructions),
		NullString(input.DefaultPriority),
		NullTime(input.DefaultDueAt),
		metadataJSON,
	))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.TaskTemplate{}, ErrDBNotFound
		}
		if isPgError(err, checkViolation) {
			return models.TaskTemplate{}, ErrCheckViolation
		}
		return models.TaskTemplate{}, fmt.Errorf("updating task template: %w", err)
	}

	return template, nil
}

func (s *service) ArchiveTaskTemplate(ctx context.Context, templateID string) error {
	const query = `
		UPDATE todos.task_templates
		SET archived_at = CURRENT_TIMESTAMP,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		  AND archived_at IS NULL
	`

	result, err := s.db.ExecContext(ctx, query, templateID)
	if err != nil {
		return fmt.Errorf("archiving task template: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("checking archived task template rows: %w", err)
	}
	if rowsAffected == 0 {
		return ErrDBNotFound
	}

	return nil
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

	var template models.TaskTemplate
	if input.TemplateID != nil {
		template, err = getTaskTemplateForCreator(ctx, tx, *input.TemplateID, creatorID)
		if err != nil {
			return models.TaskBatchCreateResult{}, err
		}
	} else {
		if input.Template == nil {
			return models.TaskBatchCreateResult{}, ErrNotNullViolation
		}
		template, err = createTaskTemplate(ctx, tx, creatorID, *input.Template)
		if err != nil {
			return models.TaskBatchCreateResult{}, err
		}
	}

	assignmentMode := models.AssignmentModeSameWork
	if input.AssignmentMode != nil {
		assignmentMode = *input.AssignmentMode
	} else if hasAssignmentOverrides(input.Assignments) {
		assignmentMode = models.AssignmentModeCustomizedWork
	}

	metadataJSON, err := marshalJSONMap(input.Metadata)
	if err != nil {
		return models.TaskBatchCreateResult{}, fmt.Errorf("encoding task batch metadata: %w", err)
	}

	const createBatchQuery = `
		INSERT INTO todos.task_batches (
			template_id,
			created_by,
			title,
			description,
			assignment_mode,
			idempotency_key,
			metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
		RETURNING id::text, template_id::text, created_by, title, description,
		          assignment_mode, idempotency_key, metadata::text, created_at
	`

	batch, err := scanTaskBatch(tx.QueryRowContext(ctx, createBatchQuery,
		template.ID,
		creatorID,
		NullString(input.Title),
		NullString(input.Description),
		assignmentMode,
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

	instances := make([]models.TaskInstance, 0, len(input.Assignments))
	for _, assignment := range input.Assignments {
		instance, err := createTaskInstanceForAssignment(ctx, tx, creatorID, template, batch, assignment)
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
	}

	if err := tx.Commit(); err != nil {
		return models.TaskBatchCreateResult{}, fmt.Errorf("committing task batch transaction: %w", err)
	}

	return models.TaskBatchCreateResult{
		Batch:          batch,
		Template:       template,
		Instances:      instances,
		TotalInstances: len(instances),
	}, nil
}

func (s *service) GetTaskBatch(ctx context.Context, batchID string) (models.TaskBatch, error) {
	return getTaskBatch(ctx, s.db, batchID)
}

func (s *service) GetTaskBatchProgress(ctx context.Context, batchID string, includeInstances bool) (models.TaskBatchProgress, error) {
	batch, err := getTaskBatch(ctx, s.db, batchID)
	if err != nil {
		return models.TaskBatchProgress{}, err
	}

	instances, err := listTaskBatchInstances(ctx, s.db, batchID)
	if err != nil {
		return models.TaskBatchProgress{}, err
	}

	summary := map[string]int{
		"assigned":    0,
		"accepted":    0,
		"in_progress": 0,
		"blocked":     0,
		"completed":   0,
		"rejected":    0,
		"cancelled":   0,
		"overdue":     0,
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
	}
	if includeInstances {
		progress.Instances = instances
	}

	return progress, nil
}

func (s *service) ListTaskBatchInstances(ctx context.Context, batchID string) ([]models.TaskInstance, error) {
	return listTaskBatchInstances(ctx, s.db, batchID)
}

func (s *service) GetTaskInstance(ctx context.Context, taskInstanceID string) (models.TaskInstance, error) {
	return getTaskInstance(ctx, s.db, taskInstanceID)
}

func (s *service) ListTaskInstancesByAssignee(ctx context.Context, assigneeID string, filter models.TaskInstanceFilter) ([]models.TaskInstance, error) {
	args := []any{assigneeID}
	conditions := []string{"assignee_id = $1"}

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
		SELECT id::text, batch_id::text, template_id::text, created_by, assignee_id,
		       assignment_key, title, description, instructions, priority, due_at, status,
		       progress_percent, started_at, completed_at, cancelled_at, completion_note,
		       template_snapshot::text, custom_payload::text,
		       replaced_by_task_instance_id::text, replaces_task_instance_id::text,
		       created_at, updated_at
		FROM todos.task_instances
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY due_at ASC NULLS LAST, created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("listing task instances by assignee: %w", err)
	}
	defer rows.Close()

	return scanTaskInstances(rows)
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

	var progressParam any
	if input.ProgressPercent != nil {
		progressParam = *input.ProgressPercent
	}

	const query = `
		UPDATE todos.task_instances
		SET title = COALESCE($2, title),
		    description = COALESCE($3, description),
		    instructions = COALESCE($4, instructions),
		    priority = COALESCE($5, priority),
		    due_at = COALESCE($6, due_at),
		    progress_percent = COALESCE($7, progress_percent),
		    custom_payload = COALESCE($8::jsonb, custom_payload),
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING id::text, batch_id::text, template_id::text, created_by, assignee_id,
		          assignment_key, title, description, instructions, priority, due_at, status,
		          progress_percent, started_at, completed_at, cancelled_at, completion_note,
		          template_snapshot::text, custom_payload::text,
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
		progressParam,
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

	const query = `
		UPDATE todos.task_instances
		SET status = $2,
		    completion_note = COALESCE($3, completion_note),
		    started_at = CASE
		        WHEN $2 IN ('accepted', 'in_progress') AND started_at IS NULL THEN CURRENT_TIMESTAMP
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
		    progress_percent = CASE
		        WHEN $2 = 'completed' THEN 100
		        ELSE progress_percent
		    END,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING id::text, batch_id::text, template_id::text, created_by, assignee_id,
		          assignment_key, title, description, instructions, priority, due_at, status,
		          progress_percent, started_at, completed_at, cancelled_at, completion_note,
		          template_snapshot::text, custom_payload::text,
		          replaced_by_task_instance_id::text, replaces_task_instance_id::text,
		          created_at, updated_at
	`

	updated, err := scanTaskInstance(tx.QueryRowContext(ctx, query,
		taskInstanceID,
		input.Status,
		NullString(input.CompletionNote),
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

	if err := tx.Commit(); err != nil {
		return models.TaskInstance{}, fmt.Errorf("committing task status transaction: %w", err)
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
	var templateID, batchID, taskInstanceID sql.NullString
	switch scope {
	case models.AttachmentScopeTemplate:
		templateID = sql.NullString{String: targetID, Valid: true}
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
			template_id,
			batch_id,
			task_instance_id,
			uploaded_by,
			file_url,
			file_name,
			mime_type,
			size_bytes
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id::text, scope, template_id::text, batch_id::text, task_instance_id::text,
		          uploaded_by, file_url, file_name, mime_type, size_bytes, created_at
	`

	attachment, err := scanTaskAttachment(tx.QueryRowContext(ctx, query,
		scope,
		templateID,
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
		SELECT id::text, scope, template_id::text, batch_id::text, task_instance_id::text,
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

	const query = `
		INSERT INTO todos.task_submissions (task_instance_id, submitted_by, note)
		VALUES ($1, $2, $3)
		RETURNING id::text, task_instance_id::text, submitted_by, note, status,
		          created_at, reviewed_at, reviewed_by
	`

	submission, err := scanTaskSubmission(tx.QueryRowContext(ctx, query, taskInstanceID, submittedBy, NullString(input.Note)))
	if err != nil {
		if isPgError(err, foreignKeyViolation) {
			return models.TaskSubmission{}, ErrForeignKeyViolation
		}
		return models.TaskSubmission{}, fmt.Errorf("creating task submission: %w", err)
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

func createTaskTemplate(ctx context.Context, runner sqlRunner, creatorID string, input models.TaskTemplateInput) (models.TaskTemplate, error) {
	metadataJSON, err := marshalJSONMap(input.Metadata)
	if err != nil {
		return models.TaskTemplate{}, fmt.Errorf("encoding task template metadata: %w", err)
	}

	const query = `
		INSERT INTO todos.task_templates (
			created_by,
			title,
			description,
			instructions,
			default_priority,
			default_due_at,
			metadata
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb)
		RETURNING id::text, created_by, title, description, instructions, default_priority,
		          default_due_at, metadata::text, created_at, updated_at, archived_at
	`

	template, err := scanTaskTemplate(runner.QueryRowContext(ctx, query,
		creatorID,
		input.Title,
		NullString(input.Description),
		NullString(input.Instructions),
		NullString(input.DefaultPriority),
		NullTime(input.DefaultDueAt),
		metadataJSON,
	))
	if err != nil {
		if isPgError(err, foreignKeyViolation) {
			return models.TaskTemplate{}, ErrForeignKeyViolation
		}
		if isPgError(err, checkViolation) || isPgError(err, notNullViolation) {
			return models.TaskTemplate{}, ErrCheckViolation
		}
		return models.TaskTemplate{}, fmt.Errorf("creating task template: %w", err)
	}

	return template, nil
}

func getTaskTemplate(ctx context.Context, runner sqlRunner, templateID string) (models.TaskTemplate, error) {
	const query = `
		SELECT id::text, created_by, title, description, instructions, default_priority,
		       default_due_at, metadata::text, created_at, updated_at, archived_at
		FROM todos.task_templates
		WHERE id = $1
		  AND archived_at IS NULL
	`

	template, err := scanTaskTemplate(runner.QueryRowContext(ctx, query, templateID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.TaskTemplate{}, ErrDBNotFound
		}
		return models.TaskTemplate{}, fmt.Errorf("getting task template: %w", err)
	}

	return template, nil
}

func getTaskTemplateForCreator(ctx context.Context, runner sqlRunner, templateID, creatorID string) (models.TaskTemplate, error) {
	const query = `
		SELECT id::text, created_by, title, description, instructions, default_priority,
		       default_due_at, metadata::text, created_at, updated_at, archived_at
		FROM todos.task_templates
		WHERE id = $1
		  AND created_by = $2
		  AND archived_at IS NULL
	`

	template, err := scanTaskTemplate(runner.QueryRowContext(ctx, query, templateID, creatorID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.TaskTemplate{}, ErrDBNotFound
		}
		return models.TaskTemplate{}, fmt.Errorf("getting task template for creator: %w", err)
	}

	return template, nil
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

	template, err := getTaskTemplate(ctx, runner, batch.TemplateID)
	if err != nil {
		return models.TaskBatchCreateResult{}, err
	}

	instances, err := listTaskBatchInstances(ctx, runner, batchID)
	if err != nil {
		return models.TaskBatchCreateResult{}, err
	}

	return models.TaskBatchCreateResult{
		Batch:          batch,
		Template:       template,
		Instances:      instances,
		TotalInstances: len(instances),
	}, nil
}

func getTaskBatch(ctx context.Context, runner sqlRunner, batchID string) (models.TaskBatch, error) {
	const query = `
		SELECT id::text, template_id::text, created_by, title, description,
		       assignment_mode, idempotency_key, metadata::text, created_at
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

func createTaskInstanceForAssignment(ctx context.Context, runner sqlRunner, creatorID string, template models.TaskTemplate, batch models.TaskBatch, assignment models.TaskAssignmentInput) (models.TaskInstance, error) {
	resolved := resolveAssignment(template, assignment)
	templateSnapshot, err := json.Marshal(template)
	if err != nil {
		return models.TaskInstance{}, fmt.Errorf("encoding template snapshot: %w", err)
	}

	customPayloadJSON, err := marshalJSONMap(assignment.CustomPayload)
	if err != nil {
		return models.TaskInstance{}, fmt.Errorf("encoding assignment custom payload: %w", err)
	}

	const query = `
		INSERT INTO todos.task_instances (
			batch_id,
			template_id,
			created_by,
			assignee_id,
			assignment_key,
			title,
			description,
			instructions,
			priority,
			due_at,
			status,
			template_snapshot,
			custom_payload
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 'assigned', $11::jsonb, $12::jsonb)
		RETURNING id::text, batch_id::text, template_id::text, created_by, assignee_id,
		          assignment_key, title, description, instructions, priority, due_at, status,
		          progress_percent, started_at, completed_at, cancelled_at, completion_note,
		          template_snapshot::text, custom_payload::text,
		          replaced_by_task_instance_id::text, replaces_task_instance_id::text,
		          created_at, updated_at
	`

	instance, err := scanTaskInstance(runner.QueryRowContext(ctx, query,
		batch.ID,
		template.ID,
		creatorID,
		assignment.AssigneeID,
		NullString(assignment.AssignmentKey),
		resolved.Title,
		NullString(resolved.Description),
		NullString(resolved.Instructions),
		NullString(resolved.Priority),
		NullTime(resolved.DueAt),
		templateSnapshot,
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
		SELECT id::text, batch_id::text, template_id::text, created_by, assignee_id,
		       assignment_key, title, description, instructions, priority, due_at, status,
		       progress_percent, started_at, completed_at, cancelled_at, completion_note,
		       template_snapshot::text, custom_payload::text,
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
		SELECT id::text, batch_id::text, template_id::text, created_by, assignee_id,
		       assignment_key, title, description, instructions, priority, due_at, status,
		       progress_percent, started_at, completed_at, cancelled_at, completion_note,
		       template_snapshot::text, custom_payload::text,
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

type resolvedAssignment struct {
	Title        string
	Description  *string
	Instructions *string
	Priority     *string
	DueAt        *time.Time
}

func resolveAssignment(template models.TaskTemplate, assignment models.TaskAssignmentInput) resolvedAssignment {
	resolved := resolvedAssignment{
		Title:        template.Title,
		Description:  template.Description,
		Instructions: template.Instructions,
		Priority:     template.DefaultPriority,
		DueAt:        template.DefaultDueAt,
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

	return resolved
}

func hasAssignmentOverrides(assignments []models.TaskAssignmentInput) bool {
	for _, assignment := range assignments {
		if assignment.Overrides != nil {
			return true
		}
	}
	return false
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
	case models.AttachmentScopeTemplate:
		return "template_id", nil
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
	if input.ProgressPercent != nil {
		value["progress_percent"] = instance.ProgressPercent
	}
	if input.CustomPayload != nil {
		value["custom_payload"] = instance.CustomPayload
	}
	return value
}

func scanTaskTemplate(scanner rowScanner) (models.TaskTemplate, error) {
	var template models.TaskTemplate
	var description, instructions, defaultPriority sql.NullString
	var defaultDueAt, archivedAt sql.NullTime
	var metadata string

	if err := scanner.Scan(
		&template.ID,
		&template.CreatedBy,
		&template.Title,
		&description,
		&instructions,
		&defaultPriority,
		&defaultDueAt,
		&metadata,
		&template.CreatedAt,
		&template.UpdatedAt,
		&archivedAt,
	); err != nil {
		return models.TaskTemplate{}, err
	}

	metadataMap, err := jsonMapFromString(metadata)
	if err != nil {
		return models.TaskTemplate{}, err
	}

	template.Description = StringPtr(description)
	template.Instructions = StringPtr(instructions)
	template.DefaultPriority = StringPtr(defaultPriority)
	template.DefaultDueAt = TimePtr(defaultDueAt)
	template.Metadata = metadataMap
	template.ArchivedAt = TimePtr(archivedAt)

	return template, nil
}

func scanTaskBatch(scanner rowScanner) (models.TaskBatch, error) {
	var batch models.TaskBatch
	var title, description, idempotencyKey sql.NullString
	var metadata string

	if err := scanner.Scan(
		&batch.ID,
		&batch.TemplateID,
		&batch.CreatedBy,
		&title,
		&description,
		&batch.AssignmentMode,
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

	batch.Title = StringPtr(title)
	batch.Description = StringPtr(description)
	batch.IdempotencyKey = StringPtr(idempotencyKey)
	batch.Metadata = metadataMap

	return batch, nil
}

func scanTaskInstance(scanner rowScanner) (models.TaskInstance, error) {
	var instance models.TaskInstance
	var templateID, assignmentKey, description, instructions, priority, completionNote sql.NullString
	var replacedByTaskInstanceID, replacesTaskInstanceID sql.NullString
	var dueAt, startedAt, completedAt, cancelledAt sql.NullTime
	var templateSnapshot, customPayload string

	if err := scanner.Scan(
		&instance.ID,
		&instance.BatchID,
		&templateID,
		&instance.CreatedBy,
		&instance.AssigneeID,
		&assignmentKey,
		&instance.Title,
		&description,
		&instructions,
		&priority,
		&dueAt,
		&instance.Status,
		&instance.ProgressPercent,
		&startedAt,
		&completedAt,
		&cancelledAt,
		&completionNote,
		&templateSnapshot,
		&customPayload,
		&replacedByTaskInstanceID,
		&replacesTaskInstanceID,
		&instance.CreatedAt,
		&instance.UpdatedAt,
	); err != nil {
		return models.TaskInstance{}, err
	}

	templateSnapshotMap, err := jsonMapFromString(templateSnapshot)
	if err != nil {
		return models.TaskInstance{}, err
	}
	customPayloadMap, err := jsonMapFromString(customPayload)
	if err != nil {
		return models.TaskInstance{}, err
	}

	instance.TemplateID = StringPtr(templateID)
	instance.AssignmentKey = StringPtr(assignmentKey)
	instance.Description = StringPtr(description)
	instance.Instructions = StringPtr(instructions)
	instance.Priority = StringPtr(priority)
	instance.DueAt = TimePtr(dueAt)
	instance.StartedAt = TimePtr(startedAt)
	instance.CompletedAt = TimePtr(completedAt)
	instance.CancelledAt = TimePtr(cancelledAt)
	instance.CompletionNote = StringPtr(completionNote)
	instance.TemplateSnapshot = templateSnapshotMap
	instance.CustomPayload = customPayloadMap
	instance.ReplacedByTaskInstanceID = StringPtr(replacedByTaskInstanceID)
	instance.ReplacesTaskInstanceID = StringPtr(replacesTaskInstanceID)

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
	var templateID, batchID, taskInstanceID, fileName, mimeType sql.NullString
	var sizeBytes sql.NullInt64

	if err := scanner.Scan(
		&attachment.ID,
		&attachment.Scope,
		&templateID,
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

	attachment.TemplateID = StringPtr(templateID)
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
