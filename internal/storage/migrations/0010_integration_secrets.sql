CREATE TABLE integration_secrets (
    name TEXT PRIMARY KEY,
    ciphertext TEXT NOT NULL,
    updated_at TEXT NOT NULL
);
