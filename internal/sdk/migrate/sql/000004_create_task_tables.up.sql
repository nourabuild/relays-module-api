CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS todos.task_batches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_by TEXT NOT NULL REFERENCES todos.users(id),
    idempotency_key TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS unique_task_batches_created_by_idempotency_key
    ON todos.task_batches(created_by, idempotency_key)
    WHERE idempotency_key IS NOT NULL;

CREATE TABLE IF NOT EXISTS todos.task_instances (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id UUID NOT NULL REFERENCES todos.task_batches(id) ON DELETE CASCADE,
    created_by TEXT NOT NULL REFERENCES todos.users(id),
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    description TEXT,
    instructions TEXT,
    priority TEXT,
    due_at TIMESTAMPTZ,
    status TEXT NOT NULL DEFAULT 'assigned'
        CHECK (status IN ('assigned', 'accepted', 'in_progress', 'blocked', 'completed', 'rejected', 'cancelled')),
    progress_percent INT NOT NULL DEFAULT 0
        CHECK (progress_percent >= 0 AND progress_percent <= 100),
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    cancelled_at TIMESTAMPTZ,
    completion_note TEXT,
    review_required BOOLEAN NOT NULL DEFAULT FALSE,
    custom_payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    replaced_by_task_instance_id UUID REFERENCES todos.task_instances(id),
    replaces_task_instance_id UUID REFERENCES todos.task_instances(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS todos.task_instance_assignees (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_instance_id UUID NOT NULL REFERENCES todos.task_instances(id) ON DELETE CASCADE,
    user_id TEXT NOT NULL REFERENCES todos.users(id),
    role TEXT NOT NULL CHECK (role IN ('assigned_by', 'assignee')),
    status TEXT NOT NULL DEFAULT 'assigned'
        CHECK (status IN ('assigned_by', 'assigned', 'in_progress', 'blocked', 'pending_review', 'completed', 'cancelled')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (task_instance_id, user_id)
);

CREATE TABLE IF NOT EXISTS todos.task_instance_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_instance_id UUID NOT NULL REFERENCES todos.task_instances(id) ON DELETE CASCADE,
    actor_id TEXT NOT NULL REFERENCES todos.users(id),
    event_type TEXT NOT NULL
        CHECK (event_type IN (
            'created',
            'assigned',
            'updated',
            'status_changed',
            'comment_added',
            'file_uploaded',
            'submission_added',
            'submission_reviewed',
            'due_date_changed',
            'cancelled',
            'reopened'
        )),
    old_value JSONB,
    new_value JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS todos.task_comments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_instance_id UUID NOT NULL REFERENCES todos.task_instances(id) ON DELETE CASCADE,
    author_id TEXT NOT NULL REFERENCES todos.users(id),
    body TEXT NOT NULL CHECK (length(trim(body)) > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS todos.task_batch_comments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    batch_id UUID NOT NULL REFERENCES todos.task_batches(id) ON DELETE CASCADE,
    author_id TEXT NOT NULL REFERENCES todos.users(id),
    body TEXT NOT NULL CHECK (length(trim(body)) > 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS todos.task_attachments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scope TEXT NOT NULL CHECK (scope IN ('batch', 'instance')),
    batch_id UUID REFERENCES todos.task_batches(id) ON DELETE CASCADE,
    task_instance_id UUID REFERENCES todos.task_instances(id) ON DELETE CASCADE,
    uploaded_by TEXT NOT NULL REFERENCES todos.users(id),
    file_url TEXT NOT NULL CHECK (length(trim(file_url)) > 0),
    file_name TEXT,
    mime_type TEXT,
    size_bytes BIGINT CHECK (size_bytes IS NULL OR size_bytes >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (
        (scope = 'batch' AND batch_id IS NOT NULL AND task_instance_id IS NULL)
        OR (scope = 'instance' AND batch_id IS NULL AND task_instance_id IS NOT NULL)
    )
);

CREATE TABLE IF NOT EXISTS todos.task_submissions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_instance_id UUID NOT NULL REFERENCES todos.task_instances(id) ON DELETE CASCADE,
    submitted_by TEXT NOT NULL REFERENCES todos.users(id),
    note TEXT,
    status TEXT NOT NULL DEFAULT 'submitted'
        CHECK (status IN ('submitted', 'accepted', 'rejected', 'revision_requested')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    reviewed_at TIMESTAMPTZ,
    reviewed_by TEXT REFERENCES todos.users(id)
);

CREATE INDEX IF NOT EXISTS idx_task_batches_created_by
    ON todos.task_batches(created_by);

CREATE INDEX IF NOT EXISTS idx_task_instance_assignees_task_instance
    ON todos.task_instance_assignees(task_instance_id);

CREATE INDEX IF NOT EXISTS idx_task_instance_assignees_user
    ON todos.task_instance_assignees(user_id);

CREATE INDEX IF NOT EXISTS idx_task_instances_batch
    ON todos.task_instances(batch_id);

CREATE INDEX IF NOT EXISTS idx_task_instances_batch_status
    ON todos.task_instances(batch_id, status);

CREATE INDEX IF NOT EXISTS idx_task_instances_created_by
    ON todos.task_instances(created_by);

CREATE INDEX IF NOT EXISTS idx_task_events_task_instance
    ON todos.task_instance_events(task_instance_id, created_at);

CREATE INDEX IF NOT EXISTS idx_task_comments_task_instance
    ON todos.task_comments(task_instance_id, created_at);

CREATE INDEX IF NOT EXISTS idx_task_batch_comments_batch
    ON todos.task_batch_comments(batch_id, created_at);

CREATE INDEX IF NOT EXISTS idx_task_attachments_batch
    ON todos.task_attachments(batch_id);

CREATE INDEX IF NOT EXISTS idx_task_attachments_instance
    ON todos.task_attachments(task_instance_id);

CREATE INDEX IF NOT EXISTS idx_task_submissions_task_instance
    ON todos.task_submissions(task_instance_id, created_at);
