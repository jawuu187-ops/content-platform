-- 0015_owner_activation.sql
--
-- One-time owner activation and session-bound MFA.
--
-- The owner role is the only account allowed to review age-verification
-- requests. It is granted exactly once, by consuming a deployment secret that is
-- bound to a pre-designated account, and never through the role-grant API.
--
-- Exactly one owner is a database invariant, not an application check: the
-- partial unique index below refuses a second owner even if two activations run
-- concurrently against different slots.

-- 1. Sessions remember when the second factor was verified. A boolean would say
--    "this session used MFA at some point", which is not enough for a step-up
--    window; the timestamp is.
ALTER TABLE identity.sessions
    ADD COLUMN IF NOT EXISTS mfa_at timestamptz;

-- 2. The role list gains "owner". Existing rows are untouched.
ALTER TABLE identity.role_grants
    DROP CONSTRAINT IF EXISTS role_grants_role_check;
ALTER TABLE identity.role_grants
    ADD CONSTRAINT role_grants_role_check
    CHECK (role IN ('fan', 'creator', 'creator_team', 'moderator', 'finance', 'support', 'admin', 'owner'));

-- Fail safely if a database somehow already holds more than one owner.
DO $$
DECLARE
    owners integer;
BEGIN
    SELECT count(*) INTO owners FROM identity.role_grants WHERE role = 'owner';
    IF owners > 1 THEN
        RAISE EXCEPTION '0015: refusing to migrate, % owner grants already exist', owners;
    END IF;
END $$;

CREATE UNIQUE INDEX IF NOT EXISTS role_grants_one_owner
    ON identity.role_grants (role)
    WHERE role = 'owner';

-- 3. Activation slots exist before any request, so SELECT ... FOR UPDATE always
--    locks a real row. Slot 2 stays unused while only slot 1 is configured.
CREATE TABLE IF NOT EXISTS ops.owner_activation_slots (
    slot               smallint PRIMARY KEY CHECK (slot IN (1, 2)),
    designated_user_id uuid REFERENCES identity.users (id) ON DELETE RESTRICT,
    consumed_at        timestamptz,
    consumed_by        uuid REFERENCES identity.users (id) ON DELETE SET NULL,
    created_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT activation_shape CHECK (
        (consumed_at IS NULL AND consumed_by IS NULL AND designated_user_id IS NULL)
        OR (consumed_at IS NOT NULL AND consumed_by IS NOT NULL AND designated_user_id IS NOT NULL)
    ),
    CONSTRAINT activation_self_binding CHECK (
        designated_user_id IS NULL OR designated_user_id = consumed_by
    ),
    CONSTRAINT activation_one_per_user UNIQUE (designated_user_id)
);

INSERT INTO ops.owner_activation_slots (slot) VALUES (1), (2)
ON CONFLICT (slot) DO NOTHING;

-- One-way transition: an unused slot may become consumed; a consumed slot can
-- never change again, and slots can never be inserted or deleted at runtime.
--
-- This protects ordinary application DML. A PostgreSQL superuser, or a table
-- owner with enough privilege, can disable or replace the trigger; that access
-- must stay restricted and audited, and the trigger is not claimed to stop it.
CREATE OR REPLACE FUNCTION ops.owner_activation_slots_one_way()
    RETURNS trigger
    LANGUAGE plpgsql
AS $$
BEGIN
    IF TG_OP = 'INSERT' THEN
        RAISE EXCEPTION 'owner_activation_slots: rows are created only by migration';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RAISE EXCEPTION 'owner_activation_slots: slots cannot be deleted';
    END IF;
    IF OLD.consumed_at IS NOT NULL THEN
        RAISE EXCEPTION 'owner_activation_slots: consumed slot is immutable';
    END IF;
    IF NEW.slot IS DISTINCT FROM OLD.slot
        OR NEW.created_at IS DISTINCT FROM OLD.created_at THEN
        RAISE EXCEPTION 'owner_activation_slots: slot identity cannot change';
    END IF;
    IF NEW.consumed_at IS NULL
        OR NEW.consumed_by IS NULL
        OR NEW.designated_user_id IS NULL THEN
        RAISE EXCEPTION 'owner_activation_slots: consumption must set all fields';
    END IF;
    RETURN NEW;
END $$;

DROP TRIGGER IF EXISTS owner_activation_slots_one_way ON ops.owner_activation_slots;
CREATE TRIGGER owner_activation_slots_one_way
    BEFORE INSERT OR UPDATE OR DELETE ON ops.owner_activation_slots
    FOR EACH ROW
EXECUTE FUNCTION ops.owner_activation_slots_one_way();

REVOKE ALL ON ops.owner_activation_slots FROM PUBLIC;
