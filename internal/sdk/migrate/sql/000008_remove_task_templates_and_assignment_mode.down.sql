CREATE TABLE IF NOT EXISTS todos.task_templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_by TEXT NOT NULL REFERENCES todos.users(id),
    title TEXT NOT NULL CHECK (length(trim(title)) > 0),
    description TEXT,
    instructions TEXT,
    default_priority TEXT,
    default_due_at TIMESTAMPTZ,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    archived_at TIMESTAMPTZ,
    review_required BOOLEAN NOT NULL DEFAULT FALSE
);

ALTER TABLE todos.task_batches
    ADD COLUMN IF NOT EXISTS template_id UUID,
    ADD COLUMN IF NOT EXISTS assignment_mode TEXT NOT NULL DEFAULT 'same_work';

CREATE TEMP TABLE task_batch_template_restore (
    batch_id UUID PRIMARY KEY,
    template_id UUID NOT NULL
) ON COMMIT DROP;

INSERT INTO task_batch_template_restore (batch_id, template_id)
SELECT id, gen_random_uuid()
FROM todos.task_batches
WHERE template_id IS NULL;

INSERT INTO todos.task_templates (
    id,
    created_by,
    title,
    description,
    instructions,
    default_priority,
    default_due_at,
    review_required,
    metadata,
    created_at,
    updated_at
)
SELECT r.template_id,
       b.created_by,
       b.title,
       b.description,
       b.instructions,
       b.priority,
       b.due_at,
       b.review_required,
       b.metadata,
       b.created_at,
       b.created_at
FROM task_batch_template_restore AS r
JOIN todos.task_batches AS b ON b.id = r.batch_id;

UPDATE todos.task_batches AS b
SET template_id = r.template_id
FROM task_batch_template_restore AS r
WHERE b.id = r.batch_id;

ALTER TABLE todos.task_batches
    ALTER COLUMN template_id SET NOT NULL,
    DROP CONSTRAINT IF EXISTS task_batches_template_id_fkey,
    ADD CONSTRAINT task_batches_template_id_fkey FOREIGN KEY (template_id) REFERENCES todos.task_templates(id),
    DROP CONSTRAINT IF EXISTS task_batches_assignment_mode_check,
    ADD CONSTRAINT task_batches_assignment_mode_check CHECK (assignment_mode IN ('same_work', 'customized_work'));

ALTER TABLE todos.task_instances
    ADD COLUMN IF NOT EXISTS template_id UUID,
    ADD COLUMN IF NOT EXISTS template_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb;

UPDATE todos.task_instances AS ti
SET template_id = b.template_id
FROM todos.task_batches AS b
WHERE ti.batch_id = b.id
  AND ti.template_id IS NULL;

ALTER TABLE todos.task_instances
    DROP CONSTRAINT IF EXISTS task_instances_template_id_fkey,
    ADD CONSTRAINT task_instances_template_id_fkey FOREIGN KEY (template_id) REFERENCES todos.task_templates(id);

ALTER TABLE todos.task_attachments
    ADD COLUMN IF NOT EXISTS template_id UUID;

ALTER TABLE todos.task_attachments
    DROP CONSTRAINT IF EXISTS task_attachments_scope_check,
    DROP CONSTRAINT IF EXISTS task_attachments_check,
    DROP CONSTRAINT IF EXISTS task_attachments_target_check,
    DROP CONSTRAINT IF EXISTS task_attachments_template_id_fkey,
    ADD CONSTRAINT task_attachments_template_id_fkey FOREIGN KEY (template_id) REFERENCES todos.task_templates(id) ON DELETE CASCADE,
    ADD CONSTRAINT task_attachments_scope_check CHECK (scope IN ('template', 'batch', 'instance')),
    ADD CONSTRAINT task_attachments_check CHECK (
        (scope = 'template' AND template_id IS NOT NULL AND batch_id IS NULL AND task_instance_id IS NULL)
        OR (scope = 'batch' AND template_id IS NULL AND batch_id IS NOT NULL AND task_instance_id IS NULL)
        OR (scope = 'instance' AND template_id IS NULL AND batch_id IS NULL AND task_instance_id IS NOT NULL)
    );

CREATE INDEX IF NOT EXISTS idx_task_templates_created_by
    ON todos.task_templates(created_by);

CREATE INDEX IF NOT EXISTS idx_task_batches_template
    ON todos.task_batches(template_id);

CREATE INDEX IF NOT EXISTS idx_task_attachments_template
    ON todos.task_attachments(template_id);
