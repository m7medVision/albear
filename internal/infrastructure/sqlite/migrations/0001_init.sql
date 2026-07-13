CREATE TABLE vault (
    singleton_id            INTEGER PRIMARY KEY
                                CHECK(singleton_id = 1),
    vault_id                BLOB NOT NULL UNIQUE
                                CHECK(length(vault_id) = 16),
    format_version          INTEGER NOT NULL,
    active_envelope_version INTEGER NOT NULL,
    created_at_ms           INTEGER NOT NULL,
    updated_at_ms           INTEGER NOT NULL
) STRICT;

CREATE TABLE key_envelopes (
    envelope_version  INTEGER PRIMARY KEY,
    vault_id          BLOB NOT NULL
                         CHECK(length(vault_id) = 16),

    kdf_algorithm     TEXT NOT NULL
                         CHECK(kdf_algorithm = 'argon2id'),
    kdf_version       INTEGER NOT NULL,
    kdf_salt          BLOB NOT NULL
                         CHECK(length(kdf_salt) >= 16),
    kdf_memory_kib    INTEGER NOT NULL
                         CHECK(kdf_memory_kib >= 65536),
    kdf_iterations    INTEGER NOT NULL
                         CHECK(kdf_iterations >= 3),
    kdf_parallelism   INTEGER NOT NULL
                         CHECK(kdf_parallelism >= 1),

    wrap_algorithm    TEXT NOT NULL
                         CHECK(wrap_algorithm = 'xchacha20poly1305'),
    wrap_nonce        BLOB NOT NULL
                         CHECK(length(wrap_nonce) = 24),
    wrapped_root_key  BLOB NOT NULL,

    canary_nonce      BLOB NOT NULL
                         CHECK(length(canary_nonce) = 24),
    encrypted_canary  BLOB NOT NULL,

    created_at_ms     INTEGER NOT NULL,

    FOREIGN KEY(vault_id)
        REFERENCES vault(vault_id)
        ON DELETE CASCADE
) STRICT;

CREATE TABLE records (
    record_id           BLOB PRIMARY KEY
                           CHECK(length(record_id) = 16),

    key_version         INTEGER NOT NULL,
    revision            INTEGER NOT NULL
                           CHECK(revision >= 1),

    metadata_nonce      BLOB NOT NULL
                           CHECK(length(metadata_nonce) = 24),
    metadata_ciphertext BLOB NOT NULL,

    secret_nonce        BLOB NOT NULL
                           CHECK(length(secret_nonce) = 24),
    secret_ciphertext   BLOB NOT NULL,

    -- key_version tags the root-key generation bound into this record's AAD.
    -- It intentionally does NOT reference key_envelopes: a master-password
    -- change replaces the envelope without touching records, because records
    -- are encrypted under subkeys of the unchanged root key (PRD 15.6).
    payload_version     INTEGER NOT NULL
) STRICT, WITHOUT ROWID;

CREATE TABLE clients (
    client_id            BLOB PRIMARY KEY
                            CHECK(length(client_id) = 16),

    client_kind          INTEGER NOT NULL,
    status               INTEGER NOT NULL,
    capability_mask      INTEGER NOT NULL,

    credential_hash      BLOB NOT NULL
                            CHECK(length(credential_hash) = 32),

    noise_static_pubkey  BLOB NOT NULL
                            CHECK(length(noise_static_pubkey) = 32),

    label_nonce          BLOB NOT NULL
                            CHECK(length(label_nonce) = 24),
    label_ciphertext     BLOB NOT NULL,

    created_at_ms        INTEGER NOT NULL,
    last_seen_at_ms      INTEGER
) STRICT, WITHOUT ROWID;

CREATE TABLE security_events (
    sequence_id         INTEGER PRIMARY KEY AUTOINCREMENT,
    occurred_at_ms      INTEGER NOT NULL,
    severity            INTEGER NOT NULL,
    event_code          INTEGER NOT NULL,

    details_nonce       BLOB,
    details_ciphertext  BLOB,

    CHECK(
        (details_nonce IS NULL AND details_ciphertext IS NULL)
        OR
        (length(details_nonce) = 24 AND details_ciphertext IS NOT NULL)
    )
) STRICT;
