CREATE TABLE github_issue_visits (
    repository_id TEXT NOT NULL,
    issue_number INTEGER NOT NULL,
    visit_number INTEGER NOT NULL CHECK (visit_number >= 1),
    matching_key TEXT NOT NULL,
    request_key TEXT NOT NULL,
    work_id TEXT,
    open INTEGER NOT NULL DEFAULT 1 CHECK (open IN (0, 1)),
    opened_at INTEGER NOT NULL,
    closed_at INTEGER,
    PRIMARY KEY (repository_id, issue_number)
);

CREATE INDEX github_issue_visits_open
ON github_issue_visits(repository_id, open, issue_number);
