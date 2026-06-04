ALTER TABLE todos.task_instance_events
    DROP CONSTRAINT IF EXISTS task_instance_events_event_type_check;

ALTER TABLE todos.task_instance_events
    ADD CONSTRAINT task_instance_events_event_type_check
    CHECK (event_type IN (
        'created',
        'assigned',
        'updated',
        'status_changed',
        'comment_added',
        'file_uploaded',
        'submission_added',
        'submission_reviewed',
        'dependency_added',
        'due_date_changed',
        'cancelled',
        'reopened'
    ));

CREATE TABLE IF NOT EXISTS todos.task_instance_dependencies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_instance_id UUID NOT NULL REFERENCES todos.task_instances(id) ON DELETE CASCADE,
    depends_on_task_instance_id UUID NOT NULL REFERENCES todos.task_instances(id) ON DELETE CASCADE,
    dependency_type TEXT NOT NULL DEFAULT 'blocks_completion'
        CHECK (dependency_type IN ('blocks_start', 'blocks_completion')),
    created_by TEXT NOT NULL REFERENCES todos.users(id),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CHECK (task_instance_id <> depends_on_task_instance_id),
    UNIQUE (task_instance_id, depends_on_task_instance_id, dependency_type)
);

CREATE INDEX IF NOT EXISTS idx_task_instance_dependencies_task_instance
    ON todos.task_instance_dependencies(task_instance_id);

CREATE INDEX IF NOT EXISTS idx_task_instance_dependencies_depends_on
    ON todos.task_instance_dependencies(depends_on_task_instance_id);
