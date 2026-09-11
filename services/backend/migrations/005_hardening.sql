-- Tenant isolation and durable lifecycle state added after the initial MVP schema.
ALTER TABLE sync_runs ENABLE ROW LEVEL SECURITY;
ALTER TABLE sync_runs FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON sync_runs
  USING (workspace_id = nullif(current_setting('app.workspace_id', true), '')::uuid)
  WITH CHECK (workspace_id = nullif(current_setting('app.workspace_id', true), '')::uuid);

ALTER TABLE workspaces ADD COLUMN billing_suspended boolean NOT NULL DEFAULT false;
ALTER TABLE workspace_members ADD COLUMN billing_suspended boolean NOT NULL DEFAULT false;
ALTER TABLE provider_accounts
  ADD COLUMN provider_identity text,
  ADD COLUMN provider_scope text NOT NULL DEFAULT 'account'
    CHECK (provider_scope IN ('account', 'organization', 'workspace')),
  ADD COLUMN identity_verified boolean NOT NULL DEFAULT false,
  ADD COLUMN identity_verified_at timestamptz,
  ADD COLUMN billing_suspended boolean NOT NULL DEFAULT false,
  ADD COLUMN backfill_cursor timestamptz;
ALTER TABLE alert_rules ADD COLUMN billing_suspended boolean NOT NULL DEFAULT false;

ALTER TABLE source_batches
  ADD COLUMN period_start timestamptz,
  ADD COLUMN period_end timestamptz,
  ADD COLUMN sync_run_id uuid,
  ADD CONSTRAINT source_batch_sync_run_fk
    FOREIGN KEY(workspace_id, sync_run_id) REFERENCES sync_runs(workspace_id, id);
UPDATE source_batches
SET period_start = nullif(preview->>'period_start', '')::timestamptz,
    period_end = nullif(preview->>'period_end', '')::timestamptz
WHERE preview ? 'period_start' AND preview ? 'period_end';
CREATE UNIQUE INDEX source_native_snapshot_unique
  ON source_batches(workspace_id, account_id, source_scope, content_hash, period_start, period_end)
  WHERE state = 'committed' AND source_scope = 'native-cost' AND sync_run_id IS NOT NULL;
-- Preserve the current row (then the oldest historical row) for a duplicated
-- revision and move only later duplicates above the existing maximum.
WITH ranked AS (
  SELECT id, workspace_id, account_id, source_record_key, revision, is_current, ingested_at,
         max(revision) OVER (PARTITION BY workspace_id, account_id, source_record_key) AS max_revision,
         row_number() OVER (PARTITION BY workspace_id, account_id, source_record_key, revision ORDER BY is_current DESC, ingested_at, id) AS duplicate_number
  FROM cost_entries
), duplicates AS (
  SELECT id,
         max_revision + row_number() OVER (PARTITION BY workspace_id, account_id, source_record_key ORDER BY revision, is_current DESC, ingested_at, id) AS new_revision
  FROM ranked
  WHERE duplicate_number > 1
)
UPDATE cost_entries c SET revision=d.new_revision
FROM duplicates d WHERE c.id=d.id;
CREATE UNIQUE INDEX cost_revision_unique
  ON cost_entries(workspace_id, account_id, source_record_key, revision);

CREATE TABLE object_intents (
  workspace_id uuid NOT NULL REFERENCES workspaces ON DELETE CASCADE,
  object_key text NOT NULL,
  purpose text NOT NULL CHECK (purpose IN ('source', 'export')),
  state text NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'deleting')),
  created_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY(workspace_id, object_key)
);
ALTER TABLE object_intents ENABLE ROW LEVEL SECURITY;
ALTER TABLE object_intents FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_scope ON object_intents
  USING (workspace_id = nullif(current_setting('app.workspace_id', true), '')::uuid)
  WITH CHECK (workspace_id = nullif(current_setting('app.workspace_id', true), '')::uuid);
CREATE INDEX object_intents_cleanup ON object_intents(workspace_id, state, created_at);

ALTER TABLE subscriptions ADD COLUMN grace_invoice_id text;
ALTER TABLE billing_events
  ADD COLUMN outcome text NOT NULL DEFAULT 'processed'
    CHECK (outcome IN ('received', 'processed', 'ignored', 'quarantined', 'failed')),
  ADD COLUMN last_error text,
  ADD COLUMN attempts integer NOT NULL DEFAULT 1,
  ADD COLUMN received_at timestamptz NOT NULL DEFAULT now(),
  ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE billing_events ALTER COLUMN outcome SET DEFAULT 'received';
ALTER TABLE billing_events ALTER COLUMN processed_at DROP NOT NULL;
ALTER TABLE billing_events ALTER COLUMN processed_at DROP DEFAULT;

CREATE TABLE billing_commands (
  id uuid PRIMARY KEY,
  billing_account_id uuid NOT NULL REFERENCES billing_accounts ON DELETE CASCADE,
  command_type text NOT NULL,
  fingerprint text NOT NULL,
  idempotency_key text NOT NULL UNIQUE,
  state text NOT NULL DEFAULT 'pending'
    CHECK (state IN ('pending', 'requires_action', 'submitted', 'succeeded', 'canceled', 'failed')),
  provider_object_id text,
  result jsonb NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(billing_account_id, id)
);
CREATE INDEX billing_commands_account ON billing_commands(billing_account_id, created_at DESC);

CREATE TABLE auth_rate_events (
  id bigserial PRIMARY KEY,
  kind text NOT NULL CHECK (kind IN ('email', 'google')),
  subject_hash text NOT NULL,
  client_hash text NOT NULL,
  created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX auth_rate_subject ON auth_rate_events(kind, subject_hash, created_at DESC);
CREATE INDEX auth_rate_client ON auth_rate_events(kind, client_hash, created_at DESC);
