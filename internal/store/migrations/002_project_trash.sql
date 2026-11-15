-- Projects are soft-deleted into a trash; NULL means live.
ALTER TABLE projects ADD COLUMN deleted_at TEXT;
CREATE INDEX projects_deleted ON projects(workspace_id, deleted_at);
