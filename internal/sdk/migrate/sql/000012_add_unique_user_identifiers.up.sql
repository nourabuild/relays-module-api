-- GetUserByAccount and GetUserByEmail assume these are unique, but the
-- schema only had a composite UNIQUE(email, account), which allows two rows
-- to share an account (or email) as long as the other column differs.
-- This migration fails if duplicates already exist; resolve them first.
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_account_unique
    ON todos.users (account);

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email_unique
    ON todos.users (email);
