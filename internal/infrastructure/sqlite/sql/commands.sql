-- name: InsertVault :exec
INSERT INTO vault (
    singleton_id, vault_id, format_version, active_envelope_version,
    created_at_ms, updated_at_ms
) VALUES (1, ?, ?, ?, ?, ?);

-- name: SetActiveEnvelope :exec
UPDATE vault
SET active_envelope_version = ?, updated_at_ms = ?
WHERE singleton_id = 1;

-- name: InsertKeyEnvelope :exec
INSERT INTO key_envelopes (
    envelope_version, vault_id,
    kdf_algorithm, kdf_version, kdf_salt,
    kdf_memory_kib, kdf_iterations, kdf_parallelism,
    wrap_algorithm, wrap_nonce, wrapped_root_key,
    canary_nonce, encrypted_canary,
    created_at_ms
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: DeleteKeyEnvelope :exec
DELETE FROM key_envelopes
WHERE envelope_version = ?;

-- name: InsertRecord :exec
INSERT INTO records (
    record_id, key_version, revision,
    metadata_nonce, metadata_ciphertext,
    secret_nonce, secret_ciphertext,
    payload_version
) VALUES (?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdateRecord :execrows
UPDATE records
SET revision = ?,
    metadata_nonce = ?,
    metadata_ciphertext = ?,
    secret_nonce = ?,
    secret_ciphertext = ?,
    payload_version = ?
WHERE record_id = ? AND revision = ?;

-- name: DeleteRecord :execrows
DELETE FROM records
WHERE record_id = ?;

-- name: InsertClient :exec
INSERT INTO clients (
    client_id, client_kind, status, capability_mask,
    credential_hash, noise_static_pubkey,
    label_nonce, label_ciphertext,
    created_at_ms, last_seen_at_ms
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, NULL);

-- name: UpdateClientStatus :execrows
UPDATE clients
SET status = ?
WHERE client_id = ?;

-- name: UpdateClientLastSeen :exec
UPDATE clients
SET last_seen_at_ms = ?
WHERE client_id = ?;

-- name: DeleteClient :execrows
DELETE FROM clients
WHERE client_id = ?;

-- name: InsertSecurityEvent :exec
INSERT INTO security_events (
    occurred_at_ms, severity, event_code,
    details_nonce, details_ciphertext
) VALUES (?, ?, ?, ?, ?);

-- name: UpsertVaultState :exec
INSERT INTO vault_state (singleton_id, state_counter, state_root, updated_at_ms)
VALUES (1, ?, ?, ?)
ON CONFLICT(singleton_id) DO UPDATE SET
    state_counter = excluded.state_counter,
    state_root    = excluded.state_root,
    updated_at_ms = excluded.updated_at_ms;
