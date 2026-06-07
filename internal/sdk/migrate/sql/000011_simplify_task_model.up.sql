CREATE EXTENSION IF NOT EXISTS pgcrypto;

DROP TABLE IF EXISTS todos.task_submissions;
DROP TABLE IF EXISTS todos.task_attachments;
DROP TABLE IF EXISTS todos.task_comments;
DROP TABLE IF EXISTS todos.task_batch_comments;
DROP TABLE IF EXISTS todos.task_instance_dependencies;
DROP TABLE IF EXISTS todos.task_instance_events;
DROP TABLE IF EXISTS todos.task_instance_assignees;
DROP TABLE IF EXISTS todos.task_instances;
DROP TABLE IF EXISTS todos.task_batches;

CREATE TABLE IF NOT EXISTS todos.tasks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_by_id TEXT NOT NULL REFERENCES todos.users(id),
    assigned_to_id TEXT NOT NULL REFERENCES todos.users(id),
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    description TEXT,
    status TEXT NOT NULL DEFAULT 'open'
        CHECK (status IN ('open', 'done', 'cancelled')),
    due_at TIMESTAMPTZ,
    delegated_from_task_id UUID REFERENCES todos.tasks(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    completed_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ,
    CHECK (created_by_id <> assigned_to_id)
);

CREATE TABLE IF NOT EXISTS todos.task_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL REFERENCES todos.tasks(id) ON DELETE CASCADE,
    author_id TEXT NOT NULL REFERENCES todos.users(id),
    body TEXT NOT NULL CHECK (length(trim(body)) > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_tasks_assigned_to
    ON todos.tasks(assigned_to_id, status, due_at);

CREATE INDEX IF NOT EXISTS idx_tasks_created_by
    ON todos.tasks(created_by_id, status, due_at);

CREATE INDEX IF NOT EXISTS idx_tasks_delegated_from
    ON todos.tasks(delegated_from_task_id);

CREATE INDEX IF NOT EXISTS idx_task_messages_task
    ON todos.task_messages(task_id, created_at);
