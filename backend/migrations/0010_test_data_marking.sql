-- Test-data marking.
--
-- The development payment provider moves no money, but it produces rows that look
-- exactly like real ones: an intent, a balanced ledger transaction, an entitlement.
-- If those rows cannot be told apart, the first real transaction would sit in the
-- same table as hundreds of simulated ones and every report would be a guess.
--
-- So the difference is a column, not a convention. `simulated` is written at insert
-- time and never updated: a row is either a simulation or it is not.

ALTER TABLE payments.intents
    ADD COLUMN IF NOT EXISTS simulated boolean NOT NULL DEFAULT false;

ALTER TABLE ledger.transactions
    ADD COLUMN IF NOT EXISTS simulated boolean NOT NULL DEFAULT false;

COMMENT ON COLUMN payments.intents.simulated IS
    'true when the intent was created by the development provider: no money moved and no provider was contacted';

COMMENT ON COLUMN ledger.transactions.simulated IS
    'true when the transaction was posted from a simulated intent; excluded from every real balance and payout';

-- Reports filter on this, so it is indexed. Partial, because almost every row in a
-- live system is simulated = false.
CREATE INDEX IF NOT EXISTS intents_simulated_idx
    ON payments.intents (created_at DESC)
    WHERE simulated;

CREATE INDEX IF NOT EXISTS transactions_simulated_idx
    ON ledger.transactions (created_at DESC)
    WHERE simulated;

-- Historical rows written by the development provider before the column existed.
UPDATE payments.intents SET simulated = true WHERE provider = 'fake' AND simulated = false;
