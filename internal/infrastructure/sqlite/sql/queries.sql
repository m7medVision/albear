-- name: GetVault :one
SELECT singleton_id, vault_id, format_version, active_envelope_version,
       created_at_ms, updated_at_ms
FROM vault
WHERE singleton_id = 1;

-- name: GetKeyEnvelope :one
SELECT envelope_version, vault_id,
       kdf_algorithm, kdf_version, kdf_salt,
       kdf_memory_kib, kdf_iterations, kdf_parallelism,
       wrap_algorithm, wrap_nonce, wrapped_root_key,
       canary_nonce, encrypted_canary,
       created_at_ms
FROM key_envelopes
WHERE envelope_version = ?;

-- name: ListKeyEnvelopeVersions :many
SELECT envelope_version
FROM key_envelopes
ORDER BY envelope_version;

-- name: GetRecord :one
SELECT record_id, key_version, revision,
       metadata_nonce, metadata_ciphertext,
       secret_nonce, secret_ciphertext,
       payload_version
FROM records
WHERE record_id = ?;

-- name: ListRecords :many
SELECT record_id, key_version, revision,
       metadata_nonce, metadata_ciphertext,
       secret_nonce, secret_ciphertext,
       payload_version
FROM records;

-- name: CountRecords :one
SELECT count(*) FROM records;

-- name: GetClient :one
SELECT client_id, client_kind, status, capability_mask,
       credential_hash, noise_static_pubkey,
       label_nonce, label_ciphertext,
       created_at_ms, last_seen_at_ms
FROM clients
WHERE client_id = ?;

-- name: ListClients :many
SELECT client_id, client_kind, status, capability_mask,
       credential_hash, noise_static_pubkey,
       label_nonce, label_ciphertext,
       created_at_ms, last_seen_at_ms
FROM clients
ORDER BY created_at_ms;

-- name: ListSecurityEvents :many
SELECT sequence_id, occurred_at_ms, severity, event_code,
       details_nonce, details_ciphertext
FROM security_events
ORDER BY sequence_id DESC
LIMIT ?;

-- Vault-state root inputs. Each is ordered by primary key so the serialization
-- the HMAC covers is canonical: the same catalog must always hash the same,
-- whatever order SQLite would otherwise return rows in.

-- name: ListRecordsForRoot :many
SELECT record_id, revision, key_version, payload_version,
       metadata_ciphertext, secret_ciphertext
FROM records
ORDER BY record_id;

-- name: ListClientsForRoot :many
SELECT client_id, status, capability_mask,
       credential_hash, noise_static_pubkey
FROM clients
ORDER BY client_id;

-- name: GetActiveEnvelopeDigest :one
SELECT wrapped_root_key, kdf_salt,
       kdf_memory_kib, kdf_iterations, kdf_parallelism
FROM key_envelopes
WHERE envelope_version = ?;

-- name: GetVaultState :one
SELECT state_counter, state_root, updated_at_ms
FROM vault_state
WHERE singleton_id = 1;
