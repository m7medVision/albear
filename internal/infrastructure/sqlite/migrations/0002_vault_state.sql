-- Authenticated, monotonic vault-state anchor.
--
-- The AEAD on each payload binds it to its own identity, but nothing
-- authenticates the *set* of rows. Deleting a record, reverting a client from
-- revoked back to approved, or restoring an old key_envelopes row to undo a
-- password change all leave every remaining ciphertext perfectly valid. This
-- table holds an HMAC over the whole catalog, keyed by a root-key subkey that
-- exists only while the vault is unlocked, plus a counter that only ever goes
-- up.
--
-- Singleton, like `vault`: one row describes one vault.
CREATE TABLE vault_state (
    singleton_id  INTEGER PRIMARY KEY
                     CHECK(singleton_id = 1),

    -- Increments once per mutating transaction. A stored counter below one
    -- this process has already seen means the file went backwards.
    state_counter INTEGER NOT NULL
                     CHECK(state_counter >= 0),

    -- HMAC-SHA256 over the canonical serialization of the catalog.
    state_root    BLOB NOT NULL
                     CHECK(length(state_root) = 32),

    updated_at_ms INTEGER NOT NULL
) STRICT;
