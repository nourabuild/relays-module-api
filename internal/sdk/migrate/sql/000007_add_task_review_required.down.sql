UPDATE todos.task_instances
SET status = 'in_progress',
    updated_at = CURRENT_TIMESTAMP
WHERE status = 'pending_review';

ALTER TABLE todos.task_instances
    DROP CONSTRAINT IF EXISTS task_instances_status_check;

ALTER TABLE todos.task_instances
    ADD CONSTRAINT task_instances_status_check
    CHECK (status IN ('assigned', 'in_progress', 'blocked', 'completed', 'cancelled'));

ALTER TABLE todos.task_instances
    DROP COLUMN IF EXISTS review_required;

ALTER TABLE todos.task_templates
    DROP COLUMN IF EXISTS review_required;
