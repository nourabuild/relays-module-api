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

INSERT INTO todos.task_instance_assignees (
    task_instance_id,
    user_id,
    role,
    status,
    created_at,
    updated_at
)
SELECT task.id,
       task.created_by,
       'assigned_by',
       'assigned_by',
       task.created_at,
       task.updated_at
FROM todos.task_instances AS task
ON CONFLICT (task_instance_id, user_id) DO NOTHING;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM information_schema.columns
        WHERE table_schema = 'todos'
          AND table_name = 'task_instances'
          AND column_name = 'assignee_id'
    ) THEN
        INSERT INTO todos.task_instance_assignees (
            task_instance_id,
            user_id,
            role,
            status,
            created_at,
            updated_at
        )
        SELECT task.id,
               task.assignee_id,
               'assignee',
               task.status,
               task.created_at,
               task.updated_at
        FROM todos.task_instances AS task
        WHERE task.assignee_id IS NOT NULL
        ON CONFLICT (task_instance_id, user_id) DO NOTHING;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_task_instance_assignees_task_instance
    ON todos.task_instance_assignees(task_instance_id);

CREATE INDEX IF NOT EXISTS idx_task_instance_assignees_user
    ON todos.task_instance_assignees(user_id);

DROP INDEX IF EXISTS todos.idx_task_instances_assignee_status;
DROP INDEX IF EXISTS todos.idx_task_instances_assignee_due;
DROP INDEX IF EXISTS todos.unique_task_instances_assignment_key_per_batch;

ALTER TABLE todos.task_instances
    DROP COLUMN IF EXISTS assignee_id,
    DROP COLUMN IF EXISTS assignment_key,
    DROP COLUMN IF EXISTS template_id,
    DROP COLUMN IF EXISTS template_snapshot;

ALTER TABLE todos.task_batches
    DROP COLUMN IF EXISTS title,
    DROP COLUMN IF EXISTS description,
    DROP COLUMN IF EXISTS instructions,
    DROP COLUMN IF EXISTS priority,
    DROP COLUMN IF EXISTS due_at,
    DROP COLUMN IF EXISTS review_required,
    DROP COLUMN IF EXISTS template_id,
    DROP COLUMN IF EXISTS assignment_mode;
