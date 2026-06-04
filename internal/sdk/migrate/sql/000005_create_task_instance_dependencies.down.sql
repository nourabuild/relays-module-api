DROP TABLE IF EXISTS todos.task_instance_dependencies;

DELETE FROM todos.task_instance_events
WHERE event_type = 'dependency_added';

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
        'due_date_changed',
        'cancelled',
        'reopened'
    ));
