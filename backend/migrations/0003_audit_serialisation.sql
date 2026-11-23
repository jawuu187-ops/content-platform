-- 0003_audit_serialisation.sql
--
-- ops.append_audit() took the advisory lock *after* selecting the predecessor
-- hash. Two concurrent writers could therefore both lock the same row (or both
-- see an empty table), compute the same prev_hash and fork the chain: the
-- verifier then reports a broken row even though no one tampered with anything.
--
-- Observed in CI when two test binaries wrote audit entries at the same time.
-- The fix is to take the transaction-scoped advisory lock first, so the
-- predecessor read and the append are one serialised unit.

CREATE OR REPLACE FUNCTION ops.append_audit(
    p_actor_type  text,
    p_actor_id    text,
    p_action      text,
    p_target_type text,
    p_target_id   text,
    p_reason      text DEFAULT NULL,
    p_metadata    jsonb DEFAULT '{}'::jsonb,
    p_request_id  text DEFAULT NULL
) RETURNS text
    LANGUAGE plpgsql
AS $$
DECLARE
    v_prev text;
    v_hash text;
BEGIN
    -- Lock first, read second. The lock is transaction-scoped, so it is released
    -- on commit or rollback and cannot leak.
    PERFORM pg_advisory_xact_lock(hashtext('ops.audit_log'));

    SELECT hash INTO v_prev FROM ops.audit_log ORDER BY id DESC LIMIT 1;

    v_hash := encode(digest(
        coalesce(v_prev, '') || '|' ||
        p_actor_type || '|' || p_actor_id || '|' || p_action || '|' ||
        p_target_type || '|' || p_target_id || '|' ||
        coalesce(p_reason, '') || '|' || p_metadata::text || '|' || coalesce(p_request_id, ''),
        'sha256'), 'hex');

    INSERT INTO ops.audit_log (
        actor_type, actor_id, action, target_type, target_id,
        reason, metadata, request_id, prev_hash, hash
    ) VALUES (
        p_actor_type, p_actor_id, p_action, p_target_type, p_target_id,
        p_reason, coalesce(p_metadata, '{}'::jsonb), p_request_id, v_prev, v_hash
    );

    RETURN v_hash;
END;
$$;
