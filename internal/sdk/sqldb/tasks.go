package sqldb

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/nourabuild/relays-api/internal/sdk/models"
)

const taskSelectColumns = `
	t.id::text,
	t.created_by_id,
	t.assigned_to_id,
	t.title,
	t.description,
	t.status,
	t.due_at,
	t.delegated_from_task_id::text,
	t.created_at,
	t.updated_at,
	t.completed_at,
	t.cancelled_at,
	creator.id::text,
	creator.name,
	creator.account,
	creator.email,
	creator.bio,
	creator.dob,
	creator.city,
	creator.phone,
	creator.avatar_photo_id,
	creator.is_admin,
	creator.created_at,
	creator.updated_at,
	assignee.id::text,
	assignee.name,
	assignee.account,
	assignee.email,
	assignee.bio,
	assignee.dob,
	assignee.city,
	assignee.phone,
	assignee.avatar_photo_id,
	assignee.is_admin,
	assignee.created_at,
	assignee.updated_at
`

const taskJoins = `
	JOIN todos.users creator ON creator.id = t.created_by_id
	JOIN todos.users assignee ON assignee.id = t.assigned_to_id
`

const taskMessageSelectColumns = `
	m.id::text,
	m.task_id::text,
	m.author_id,
	m.body,
	m.created_at,
	author.id::text,
	author.name,
	author.account,
	author.email,
	author.bio,
	author.dob,
	author.city,
	author.phone,
	author.avatar_photo_id,
	author.is_admin,
	author.created_at,
	author.updated_at
`

func (s *service) ListExpectations(ctx context.Context, userID string) ([]models.Task, error) {
	const query = `
		SELECT ` + taskSelectColumns + `
		FROM todos.tasks t
		` + taskJoins + `
		WHERE t.assigned_to_id = $1
		  AND t.status <> 'cancelled'
		ORDER BY t.due_at ASC NULLS LAST, t.created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("listing expectations: %w", err)
	}
	defer rows.Close()

	return scanTasks(rows)
}

func (s *service) ListTodos(ctx context.Context, userID string) ([]models.Task, error) {
	const query = `
		SELECT ` + taskSelectColumns + `
		FROM todos.tasks t
		` + taskJoins + `
		WHERE t.created_by_id = $1
		  AND t.status <> 'cancelled'
		ORDER BY t.due_at ASC NULLS LAST, t.created_at DESC
	`

	rows, err := s.db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("listing todos: %w", err)
	}
	defer rows.Close()

	return scanTasks(rows)
}

func (s *service) CreateTask(ctx context.Context, creatorID string, input models.CreateTask) (models.Task, error) {
	const query = `
		INSERT INTO todos.tasks (
			created_by_id,
			assigned_to_id,
			title,
			description,
			due_at,
			delegated_from_task_id
		)
		VALUES ($1, $2, $3, $4, $5, $6::uuid)
		RETURNING id::text
	`

	var taskID string
	if err := s.db.QueryRowContext(ctx, query,
		creatorID,
		input.AssignedToID,
		input.Title,
		NullString(input.Description),
		NullTime(input.DueAt),
		NullString(input.DelegatedFromTaskID),
	).Scan(&taskID); err != nil {
		return models.Task{}, taskWriteError("creating task", err)
	}

	return s.GetTask(ctx, taskID)
}

func (s *service) GetTask(ctx context.Context, taskID string) (models.Task, error) {
	const query = `
		SELECT ` + taskSelectColumns + `
		FROM todos.tasks t
		` + taskJoins + `
		WHERE t.id = $1
	`

	task, err := scanTaskWithUsers(s.db.QueryRowContext(ctx, query, taskID))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Task{}, ErrDBNotFound
		}
		return models.Task{}, fmt.Errorf("getting task: %w", err)
	}
	return task, nil
}

func (s *service) UpdateTask(ctx context.Context, taskID string, input models.UpdateTask) (models.Task, error) {
	const query = `
		UPDATE todos.tasks
		SET assigned_to_id = COALESCE($2, assigned_to_id),
		    title = COALESCE($3, title),
		    description = COALESCE($4, description),
		    due_at = COALESCE($5, due_at),
		    delegated_from_task_id = COALESCE($6::uuid, delegated_from_task_id),
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING id::text
	`

	var updatedID string
	if err := s.db.QueryRowContext(ctx, query,
		taskID,
		NullString(input.AssignedToID),
		NullString(input.Title),
		NullString(input.Description),
		NullTime(input.DueAt),
		NullString(input.DelegatedFromTaskID),
	).Scan(&updatedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Task{}, ErrDBNotFound
		}
		return models.Task{}, taskWriteError("updating task", err)
	}

	return s.GetTask(ctx, updatedID)
}

func (s *service) UpdateTaskStatus(ctx context.Context, taskID, status string) (models.Task, error) {
	const query = `
		UPDATE todos.tasks
		SET status = $2,
		    completed_at = CASE WHEN $2 = 'done' THEN CURRENT_TIMESTAMP ELSE NULL END,
		    cancelled_at = CASE WHEN $2 = 'cancelled' THEN CURRENT_TIMESTAMP ELSE NULL END,
		    updated_at = CURRENT_TIMESTAMP
		WHERE id = $1
		RETURNING id::text
	`

	var updatedID string
	if err := s.db.QueryRowContext(ctx, query, taskID, status).Scan(&updatedID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.Task{}, ErrDBNotFound
		}
		return models.Task{}, taskWriteError("updating task status", err)
	}

	return s.GetTask(ctx, updatedID)
}

func (s *service) CreateTaskMessage(ctx context.Context, taskID, authorID string, input models.CreateTaskMessage) (models.TaskMessage, error) {
	const query = `
		WITH inserted AS (
			INSERT INTO todos.task_messages (task_id, author_id, body)
			SELECT $1, $2, $3
			WHERE EXISTS (
				SELECT 1
				FROM todos.tasks
				WHERE id = $1
				  AND (created_by_id = $2 OR assigned_to_id = $2)
			)
			RETURNING id, task_id, author_id, body, created_at
		)
		SELECT ` + taskMessageSelectColumns + `
		FROM inserted m
		JOIN todos.users author ON author.id = m.author_id
	`

	message, err := scanTaskMessageWithAuthor(s.db.QueryRowContext(ctx, query, taskID, authorID, input.Body))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return models.TaskMessage{}, ErrDBNotFound
		}
		return models.TaskMessage{}, taskWriteError("creating task message", err)
	}
	return message, nil
}

func (s *service) ListTaskMessages(ctx context.Context, taskID, userID string) ([]models.TaskMessage, error) {
	const query = `
		SELECT ` + taskMessageSelectColumns + `
		FROM todos.task_messages m
		JOIN todos.tasks t ON t.id = m.task_id
		JOIN todos.users author ON author.id = m.author_id
		WHERE m.task_id = $1
		  AND (t.created_by_id = $2 OR t.assigned_to_id = $2)
		ORDER BY m.created_at ASC
	`

	rows, err := s.db.QueryContext(ctx, query, taskID, userID)
	if err != nil {
		return nil, fmt.Errorf("listing task messages: %w", err)
	}
	defer rows.Close()

	var messages []models.TaskMessage
	for rows.Next() {
		message, err := scanTaskMessageWithAuthor(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning task message: %w", err)
		}
		messages = append(messages, message)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating task messages: %w", err)
	}

	return messages, nil
}

func scanTasks(rows *sql.Rows) ([]models.Task, error) {
	var tasks []models.Task
	for rows.Next() {
		task, err := scanTaskWithUsers(rows)
		if err != nil {
			return nil, fmt.Errorf("scanning task: %w", err)
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating tasks: %w", err)
	}
	return tasks, nil
}

func scanTaskWithUsers(scanner rowScanner) (models.Task, error) {
	var task models.Task
	var description, delegatedFromTaskID sql.NullString
	var dueAt, completedAt, cancelledAt sql.NullTime
	var creator, assignee models.User
	var creatorBio, creatorDOB, creatorCity, creatorPhone sql.NullString
	var assigneeBio, assigneeDOB, assigneeCity, assigneePhone sql.NullString
	var creatorAvatarPhotoID, assigneeAvatarPhotoID sql.NullInt32

	if err := scanner.Scan(
		&task.ID,
		&task.CreatedByID,
		&task.AssignedToID,
		&task.Title,
		&description,
		&task.Status,
		&dueAt,
		&delegatedFromTaskID,
		&task.CreatedAt,
		&task.UpdatedAt,
		&completedAt,
		&cancelledAt,
		&creator.ID,
		&creator.Name,
		&creator.Account,
		&creator.Email,
		&creatorBio,
		&creatorDOB,
		&creatorCity,
		&creatorPhone,
		&creatorAvatarPhotoID,
		&creator.IsAdmin,
		&creator.CreatedAt,
		&creator.UpdatedAt,
		&assignee.ID,
		&assignee.Name,
		&assignee.Account,
		&assignee.Email,
		&assigneeBio,
		&assigneeDOB,
		&assigneeCity,
		&assigneePhone,
		&assigneeAvatarPhotoID,
		&assignee.IsAdmin,
		&assignee.CreatedAt,
		&assignee.UpdatedAt,
	); err != nil {
		return models.Task{}, err
	}

	task.Description = StringPtr(description)
	task.DueAt = TimePtr(dueAt)
	task.DelegatedFromTaskID = StringPtr(delegatedFromTaskID)
	task.CompletedAt = TimePtr(completedAt)
	task.CancelledAt = TimePtr(cancelledAt)

	applyUserNullableFields(&creator, creatorBio, creatorDOB, creatorCity, creatorPhone, creatorAvatarPhotoID)
	applyUserNullableFields(&assignee, assigneeBio, assigneeDOB, assigneeCity, assigneePhone, assigneeAvatarPhotoID)
	task.CreatedBy = &creator
	task.AssignedTo = &assignee

	return task, nil
}

func scanTaskMessageWithAuthor(scanner rowScanner) (models.TaskMessage, error) {
	var message models.TaskMessage
	var author models.User
	var bio, dob, city, phone sql.NullString
	var avatarPhotoID sql.NullInt32

	if err := scanner.Scan(
		&message.ID,
		&message.TaskID,
		&message.AuthorID,
		&message.Body,
		&message.CreatedAt,
		&author.ID,
		&author.Name,
		&author.Account,
		&author.Email,
		&bio,
		&dob,
		&city,
		&phone,
		&avatarPhotoID,
		&author.IsAdmin,
		&author.CreatedAt,
		&author.UpdatedAt,
	); err != nil {
		return models.TaskMessage{}, err
	}

	applyUserNullableFields(&author, bio, dob, city, phone, avatarPhotoID)
	message.Author = &author

	return message, nil
}

func applyUserNullableFields(user *models.User, bio, dob, city, phone sql.NullString, avatarPhotoID sql.NullInt32) {
	user.Bio = StringPtr(bio)
	user.DOB = StringPtr(dob)
	user.City = StringPtr(city)
	user.Phone = StringPtr(phone)
	user.AvatarPhotoID = Int32Ptr(avatarPhotoID)
}

func taskWriteError(operation string, err error) error {
	switch {
	case isPgError(err, foreignKeyViolation):
		return ErrForeignKeyViolation
	case isPgError(err, checkViolation):
		return ErrCheckViolation
	case isPgError(err, notNullViolation):
		return ErrNotNullViolation
	default:
		return fmt.Errorf("%s: %w", operation, err)
	}
}
