ALTER TABLE todos.task_batches
    ADD COLUMN IF NOT EXISTS instructions TEXT,
    ADD COLUMN IF NOT EXISTS priority TEXT,
    ADD COLUMN IF NOT EXISTS due_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS review_required BOOLEAN NOT NULL DEFAULT FALSE;

DO $$
BEGIN
    IF to_regclass('todos.task_templates') IS NOT NULL
       AND EXISTS (
           SELECT 1
           FROM information_schema.columns
           WHERE table_schema = 'todos'
             AND table_name = 'task_batches'
             AND column_name = 'template_id'
       ) THEN
        EXECUTE $copy_template_defaults$
            UPDATE todos.task_batches AS b
            SET title = COALESCE(NULLIF(trim(b.title), ''), t.title),
                description = COALESCE(b.description, t.description),
                instructions = COALESCE(b.instructions, t.instructions),
                priority = COALESCE(b.priority, t.default_priority),
                due_at = COALESCE(b.due_at, t.default_due_at),
                review_required = COALESCE(b.review_required, t.review_required)
            FROM todos.task_templates AS t
            WHERE b.template_id = t.id
        $copy_template_defaults$;
    END IF;
END $$;

UPDATE todos.task_batches
SET title = 'Untitled task batch'
WHERE title IS NULL OR length(trim(title)) = 0;

ALTER TABLE todos.task_batches
    ALTER COLUMN title SET NOT NULL;

ALTER TABLE todos.task_batches
    DROP CONSTRAINT IF EXISTS task_batches_title_check;

ALTER TABLE todos.task_batches
    ADD CONSTRAINT task_batches_title_check CHECK (length(trim(title)) > 0);

DELETE FROM todos.task_attachments
WHERE scope = 'template';

DROP INDEX IF EXISTS todos.idx_task_batches_template;
DROP INDEX IF EXISTS todos.idx_task_attachments_template;

ALTER TABLE todos.task_attachments
    DROP CONSTRAINT IF EXISTS task_attachments_scope_check,
    DROP CONSTRAINT IF EXISTS task_attachments_check,
    DROP CONSTRAINT IF EXISTS task_attachments_target_check;

ALTER TABLE todos.task_attachments
    DROP COLUMN IF EXISTS template_id;

ALTER TABLE todos.task_attachments
    ADD CONSTRAINT task_attachments_scope_check CHECK (scope IN ('batch', 'instance')),
    ADD CONSTRAINT task_attachments_target_check CHECK (
        (scope = 'batch' AND batch_id IS NOT NULL AND task_instance_id IS NULL)
        OR (scope = 'instance' AND batch_id IS NULL AND task_instance_id IS NOT NULL)
    );

ALTER TABLE todos.task_instances
    DROP COLUMN IF EXISTS template_id,
    DROP COLUMN IF EXISTS template_snapshot;

ALTER TABLE todos.task_batches
    DROP COLUMN IF EXISTS template_id,
    DROP COLUMN IF EXISTS assignment_mode;

DROP TABLE IF EXISTS todos.task_templates;
