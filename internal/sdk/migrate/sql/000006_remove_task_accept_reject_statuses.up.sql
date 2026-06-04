UPDATE todos.task_instances
SET status = 'in_progress',
    started_at = COALESCE(started_at, CURRENT_TIMESTAMP),
    updated_at = CURRENT_TIMESTAMP
WHERE status = 'accepted';

UPDATE todos.task_instances
SET status = 'cancelled',
    cancelled_at = COALESCE(cancelled_at, CURRENT_TIMESTAMP),
    updated_at = CURRENT_TIMESTAMP
WHERE status = 'rejected';

ALTER TABLE todos.task_instances
    DROP CONSTRAINT IF EXISTS task_instances_status_check;

ALTER TABLE todos.task_instances
    ADD CONSTRAINT task_instances_status_check
    CHECK (status IN ('assigned', 'in_progress', 'blocked', 'completed', 'cancelled'));
