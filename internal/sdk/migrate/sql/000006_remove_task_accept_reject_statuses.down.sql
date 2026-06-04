ALTER TABLE todos.task_instances
    DROP CONSTRAINT IF EXISTS task_instances_status_check;

ALTER TABLE todos.task_instances
    ADD CONSTRAINT task_instances_status_check
    CHECK (status IN ('assigned', 'accepted', 'in_progress', 'blocked', 'completed', 'rejected', 'cancelled'));
