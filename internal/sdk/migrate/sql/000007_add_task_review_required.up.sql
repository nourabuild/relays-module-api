ALTER TABLE todos.task_instances
    ADD COLUMN IF NOT EXISTS review_required BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE todos.task_instances
    DROP CONSTRAINT IF EXISTS task_instances_status_check;

ALTER TABLE todos.task_instances
    ADD CONSTRAINT task_instances_status_check
    CHECK (status IN ('assigned', 'in_progress', 'blocked', 'pending_review', 'completed', 'cancelled'));
