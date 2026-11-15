CREATE TABLE users (
    id            TEXT PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE COLLATE NOCASE,
    password_hash TEXT NOT NULL,
    display_name  TEXT NOT NULL,
    first_name    TEXT NOT NULL DEFAULT '',
    last_name     TEXT NOT NULL DEFAULT '',
    is_active     INTEGER NOT NULL DEFAULT 1,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL
);

CREATE TABLE sessions (
    token_hash BLOB PRIMARY KEY,
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);
CREATE INDEX sessions_user ON sessions(user_id);

CREATE TABLE api_tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    label        TEXT NOT NULL DEFAULT '',
    token_hash   BLOB NOT NULL UNIQUE,
    created_at   TEXT NOT NULL,
    last_used_at TEXT
);
CREATE INDEX api_tokens_user ON api_tokens(user_id);

CREATE TABLE workspaces (
    id         TEXT PRIMARY KEY,
    name       TEXT NOT NULL,
    slug       TEXT NOT NULL UNIQUE,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE workspace_members (
    id           TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role         INTEGER NOT NULL,
    created_at   TEXT NOT NULL,
    updated_at   TEXT NOT NULL,
    UNIQUE (workspace_id, user_id)
);
CREATE INDEX workspace_members_user ON workspace_members(user_id);

CREATE TABLE projects (
    id                       TEXT PRIMARY KEY,
    workspace_id             TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
    name                     TEXT NOT NULL,
    identifier               TEXT NOT NULL,
    description              TEXT NOT NULL DEFAULT '',
    network                  INTEGER NOT NULL DEFAULT 2,
    project_lead             TEXT REFERENCES users(id) ON DELETE SET NULL,
    default_assignee         TEXT REFERENCES users(id) ON DELETE SET NULL,
    default_state            TEXT,
    emoji                    TEXT,
    icon_prop                TEXT,
    cover_image              TEXT,
    module_view              INTEGER NOT NULL DEFAULT 1,
    cycle_view               INTEGER NOT NULL DEFAULT 1,
    issue_views_view         INTEGER NOT NULL DEFAULT 0,
    page_view                INTEGER NOT NULL DEFAULT 1,
    intake_view              INTEGER NOT NULL DEFAULT 0,
    is_time_tracking_enabled INTEGER NOT NULL DEFAULT 0,
    is_issue_type_enabled    INTEGER NOT NULL DEFAULT 0,
    guest_view_all_features  INTEGER NOT NULL DEFAULT 0,
    archive_in               INTEGER NOT NULL DEFAULT 0,
    close_in                 INTEGER NOT NULL DEFAULT 0,
    timezone                 TEXT NOT NULL DEFAULT 'UTC',
    external_source          TEXT,
    external_id              TEXT,
    next_sequence            INTEGER NOT NULL DEFAULT 1,
    created_by               TEXT REFERENCES users(id),
    updated_by               TEXT REFERENCES users(id),
    created_at               TEXT NOT NULL,
    updated_at               TEXT NOT NULL,
    UNIQUE (workspace_id, identifier),
    UNIQUE (workspace_id, name)
);
CREATE INDEX projects_created ON projects(workspace_id, created_at);
CREATE INDEX projects_external ON projects(workspace_id, external_source, external_id);

CREATE TABLE states (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    color           TEXT NOT NULL,
    grp             TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    sequence        REAL NOT NULL,
    is_default      INTEGER NOT NULL DEFAULT 0,
    slug            TEXT NOT NULL,
    external_source TEXT,
    external_id     TEXT,
    created_by      TEXT REFERENCES users(id),
    updated_by      TEXT REFERENCES users(id),
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    UNIQUE (project_id, name)
);
CREATE INDEX states_sequence ON states(project_id, sequence);
CREATE INDEX states_external ON states(project_id, external_source, external_id);

CREATE TABLE labels (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    color           TEXT NOT NULL DEFAULT '',
    description     TEXT NOT NULL DEFAULT '',
    parent_id       TEXT REFERENCES labels(id) ON DELETE SET NULL,
    sort_order      REAL NOT NULL,
    external_source TEXT,
    external_id     TEXT,
    created_by      TEXT REFERENCES users(id),
    updated_by      TEXT REFERENCES users(id),
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    UNIQUE (project_id, name)
);
CREATE INDEX labels_created ON labels(project_id, created_at);
CREATE INDEX labels_parent ON labels(parent_id);
CREATE INDEX labels_external ON labels(project_id, external_source, external_id);

CREATE TABLE issues (
    id               TEXT PRIMARY KEY,
    project_id       TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    sequence_id      INTEGER NOT NULL,
    name             TEXT NOT NULL,
    description_html TEXT NOT NULL DEFAULT '<p></p>',
    state_id         TEXT NOT NULL REFERENCES states(id),
    priority         TEXT NOT NULL DEFAULT 'none',
    parent_id        TEXT REFERENCES issues(id) ON DELETE SET NULL,
    start_date       TEXT,
    target_date      TEXT,
    sort_order       REAL NOT NULL,
    completed_at     TEXT,
    external_source  TEXT,
    external_id      TEXT,
    created_by       TEXT REFERENCES users(id),
    updated_by       TEXT REFERENCES users(id),
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    UNIQUE (project_id, sequence_id)
);
CREATE INDEX issues_created ON issues(project_id, created_at);
CREATE INDEX issues_updated ON issues(project_id, updated_at);
CREATE INDEX issues_sort ON issues(project_id, sort_order);
CREATE INDEX issues_state ON issues(state_id);
CREATE INDEX issues_priority ON issues(project_id, priority);
CREATE INDEX issues_parent ON issues(parent_id);
CREATE INDEX issues_dates ON issues(project_id, target_date);
CREATE INDEX issues_start ON issues(project_id, start_date);
CREATE INDEX issues_completed ON issues(project_id, completed_at);
CREATE INDEX issues_external ON issues(project_id, external_source, external_id);

CREATE TABLE issue_assignees (
    issue_id TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    user_id  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (issue_id, user_id)
);
CREATE INDEX issue_assignees_user ON issue_assignees(user_id);

CREATE TABLE issue_labels (
    issue_id TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    label_id TEXT NOT NULL REFERENCES labels(id) ON DELETE CASCADE,
    PRIMARY KEY (issue_id, label_id)
);
CREATE INDEX issue_labels_label ON issue_labels(label_id);

CREATE TABLE cycles (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    start_date      TEXT,
    end_date        TEXT,
    owned_by        TEXT REFERENCES users(id),
    timezone        TEXT NOT NULL DEFAULT 'UTC',
    sort_order      REAL NOT NULL,
    external_source TEXT,
    external_id     TEXT,
    created_by      TEXT REFERENCES users(id),
    updated_by      TEXT REFERENCES users(id),
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);
CREATE INDEX cycles_created ON cycles(project_id, created_at);
CREATE INDEX cycles_dates ON cycles(project_id, start_date, end_date);
CREATE INDEX cycles_external ON cycles(project_id, external_source, external_id);

-- issue_id is UNIQUE: a work item is in at most one cycle.
CREATE TABLE cycle_issues (
    id         TEXT PRIMARY KEY,
    cycle_id   TEXT NOT NULL REFERENCES cycles(id) ON DELETE CASCADE,
    issue_id   TEXT NOT NULL UNIQUE REFERENCES issues(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    created_by TEXT REFERENCES users(id),
    updated_by TEXT REFERENCES users(id),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
CREATE INDEX cycle_issues_cycle ON cycle_issues(cycle_id);

CREATE TABLE modules (
    id              TEXT PRIMARY KEY,
    project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    start_date      TEXT,
    target_date     TEXT,
    status          TEXT NOT NULL DEFAULT 'planned',
    lead_id         TEXT REFERENCES users(id) ON DELETE SET NULL,
    sort_order      REAL NOT NULL,
    external_source TEXT,
    external_id     TEXT,
    created_by      TEXT REFERENCES users(id),
    updated_by      TEXT REFERENCES users(id),
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    UNIQUE (project_id, name)
);
CREATE INDEX modules_created ON modules(project_id, created_at);
CREATE INDEX modules_external ON modules(project_id, external_source, external_id);

CREATE TABLE module_members (
    module_id TEXT NOT NULL REFERENCES modules(id) ON DELETE CASCADE,
    user_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    PRIMARY KEY (module_id, user_id)
);

CREATE TABLE module_issues (
    id         TEXT PRIMARY KEY,
    module_id  TEXT NOT NULL REFERENCES modules(id) ON DELETE CASCADE,
    issue_id   TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    created_by TEXT REFERENCES users(id),
    updated_by TEXT REFERENCES users(id),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (module_id, issue_id)
);
CREATE INDEX module_issues_issue ON module_issues(issue_id);

CREATE TABLE comments (
    id              TEXT PRIMARY KEY,
    issue_id        TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    project_id      TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    actor_id        TEXT REFERENCES users(id),
    comment_html    TEXT NOT NULL,
    access          TEXT NOT NULL DEFAULT 'INTERNAL',
    edited_at       TEXT,
    external_source TEXT,
    external_id     TEXT,
    created_by      TEXT REFERENCES users(id),
    updated_by      TEXT REFERENCES users(id),
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL
);
CREATE INDEX comments_issue ON comments(issue_id, created_at);
CREATE INDEX comments_external ON comments(project_id, external_source, external_id);

CREATE TABLE issue_links (
    id         TEXT PRIMARY KEY,
    issue_id   TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    title      TEXT,
    url        TEXT NOT NULL,
    metadata   TEXT NOT NULL DEFAULT '{}',
    created_by TEXT REFERENCES users(id),
    updated_by TEXT REFERENCES users(id),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE (issue_id, url)
);
CREATE INDEX issue_links_issue ON issue_links(issue_id, created_at);

CREATE TABLE activities (
    id               TEXT PRIMARY KEY,
    issue_id         TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    project_id       TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    actor_id         TEXT REFERENCES users(id),
    verb             TEXT NOT NULL,
    field            TEXT,
    old_value        TEXT,
    new_value        TEXT,
    comment          TEXT NOT NULL DEFAULT '',
    old_identifier   TEXT,
    new_identifier   TEXT,
    issue_comment_id TEXT REFERENCES comments(id) ON DELETE SET NULL,
    epoch            REAL NOT NULL,
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL
);
CREATE INDEX activities_issue ON activities(issue_id, created_at);
CREATE INDEX activities_comment ON activities(issue_comment_id);
