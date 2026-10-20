-- Sealed secrets: value is nonce || AES-256-GCM ciphertext, bound to the name.
-- Dates are YYYY-MM-DD text, '' for none.
CREATE TABLE secrets (
  name       TEXT NOT NULL PRIMARY KEY,
  sealed     BLOB NOT NULL,
  created_at TEXT NOT NULL,
  rotated_at TEXT NOT NULL,
  expires_on TEXT NOT NULL,
  remind_on  TEXT NOT NULL
) STRICT;

-- API tokens: only the SHA-256 of the token is kept. role is the one role
-- the token acts as.
CREATE TABLE tokens (
  id         INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
  name       TEXT    NOT NULL UNIQUE,
  hash       BLOB    NOT NULL UNIQUE,
  role       TEXT    NOT NULL,
  created_at TEXT    NOT NULL,
  expires_on TEXT    NOT NULL,
  remind_on  TEXT    NOT NULL,
  revoked_at TEXT    NOT NULL
) STRICT;
