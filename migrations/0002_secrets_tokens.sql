-- The secret store: each value sealed with AES-256-GCM, the name as the
-- associated data. No statement returns a value to a caller outside the
-- vault, and none shows it.
CREATE TABLE secrets (
  name       TEXT NOT NULL PRIMARY KEY,
  sealed     TEXT NOT NULL,
  created_at TEXT NOT NULL,
  rotated_at TEXT,
  expires_at TEXT,
  remind_at  TEXT
) STRICT;

-- API tokens: only the SHA-256 of a token is kept; the token itself is shown
-- once, when it is issued.
CREATE TABLE tokens (
  id         INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
  name       TEXT NOT NULL UNIQUE,
  hash       TEXT NOT NULL UNIQUE,
  scope      TEXT NOT NULL,
  created_at TEXT NOT NULL,
  expires_at TEXT,
  remind_at  TEXT,
  revoked_at TEXT
) STRICT;
