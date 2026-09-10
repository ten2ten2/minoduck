CREATE TABLE users (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), email text NOT NULL UNIQUE,
 locale text NOT NULL DEFAULT 'en' CHECK(locale IN ('en','zh-hans','zh-hant')),
 theme text NOT NULL DEFAULT 'system' CHECK(theme IN ('light','dark','system')),
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE login_tokens (
 token_hash text PRIMARY KEY, email text NOT NULL, browser_hash text NOT NULL,
 expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX login_email_recent ON login_tokens(email,created_at);
CREATE TABLE oauth_states (
 state_hash text PRIMARY KEY, browser_hash text NOT NULL, verifier text NOT NULL,
 nonce text NOT NULL, expires_at timestamptz NOT NULL
);
CREATE TABLE sessions (
 token_hash text PRIMARY KEY, user_id uuid NOT NULL REFERENCES users ON DELETE CASCADE,
 csrf_token text NOT NULL, expires_at timestamptz NOT NULL, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE billing_accounts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), owner_id uuid NOT NULL UNIQUE REFERENCES users,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE subscriptions (
 billing_account_id uuid PRIMARY KEY REFERENCES billing_accounts,
 provider_customer_id text UNIQUE, provider_subscription_id text UNIQUE,
 plan_code text NOT NULL DEFAULT 'free' CHECK(plan_code IN ('free','starter','team','business')),
 billing_interval text CHECK(billing_interval IN ('month','year')),
 status text NOT NULL DEFAULT 'free', current_period_start timestamptz, current_period_end timestamptz,
 cancel_at_period_end boolean NOT NULL DEFAULT false, grace_period_until timestamptz,
 scheduled_plan text, scheduled_interval text, provider_schedule_id text,
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE billing_events (
 provider text NOT NULL DEFAULT 'stripe', provider_event_id text NOT NULL,
 event_type text NOT NULL, processed_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(provider,provider_event_id)
);
CREATE TABLE checkout_requests (
 billing_account_id uuid PRIMARY KEY REFERENCES billing_accounts,
 request_key text NOT NULL, plan_code text NOT NULL, billing_interval text NOT NULL,
 session_id text, url text, expires_at timestamptz NOT NULL
);
CREATE TABLE workspaces (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), billing_account_id uuid NOT NULL REFERENCES billing_accounts,
 slug text NOT NULL UNIQUE CHECK(slug ~ '^[a-z0-9][a-z0-9-]{1,47}$'), name text NOT NULL,
 deletion_requested_at timestamptz, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE workspace_members (
 workspace_id uuid NOT NULL REFERENCES workspaces ON DELETE CASCADE,
 user_id uuid NOT NULL REFERENCES users, role text NOT NULL CHECK(role IN ('owner','admin','viewer')),
 created_at timestamptz NOT NULL DEFAULT now(), PRIMARY KEY(workspace_id,user_id)
);
CREATE TABLE invitations (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id uuid NOT NULL REFERENCES workspaces ON DELETE CASCADE,
 email text NOT NULL, role text NOT NULL CHECK(role IN ('admin','viewer')),
 token_hash text NOT NULL UNIQUE, expires_at timestamptz NOT NULL, accepted_at timestamptz,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE provider_accounts (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id uuid NOT NULL REFERENCES workspaces ON DELETE CASCADE,
 provider text NOT NULL CHECK(provider IN ('openai','anthropic','openrouter','csv')),
 name text NOT NULL, external_account_ref text NOT NULL,
 status text NOT NULL DEFAULT 'validating', credential_cipher bytea, credential_key_id text, credential_suffix text,
 generation integer NOT NULL DEFAULT 1, next_sync_at timestamptz, last_sync_at timestamptz,
 data_through timestamptz, error_code text, created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,id), UNIQUE(workspace_id,provider,external_account_ref)
);
CREATE TABLE source_batches (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id uuid NOT NULL REFERENCES workspaces ON DELETE CASCADE,
 account_id uuid NOT NULL, object_key text NOT NULL, content_hash text NOT NULL,
 source_scope text NOT NULL, cost_kind text NOT NULL, source_timezone text NOT NULL, granularity text NOT NULL,
 state text NOT NULL, preview jsonb NOT NULL DEFAULT '{}', created_at timestamptz NOT NULL DEFAULT now(),
 committed_at timestamptz, UNIQUE(workspace_id,id),
 FOREIGN KEY(workspace_id,account_id) REFERENCES provider_accounts(workspace_id,id)
);
CREATE INDEX source_hash_idx ON source_batches(workspace_id,account_id,content_hash);
CREATE TABLE sync_runs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id uuid NOT NULL REFERENCES workspaces ON DELETE CASCADE,
 account_id uuid NOT NULL, generation integer NOT NULL, state text NOT NULL DEFAULT 'pending',
 period_start timestamptz NOT NULL, period_end timestamptz NOT NULL, error_code text,
 started_at timestamptz, finished_at timestamptz, created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,id), FOREIGN KEY(workspace_id,account_id) REFERENCES provider_accounts(workspace_id,id)
);
CREATE UNIQUE INDEX sync_active_idx ON sync_runs(account_id) WHERE state IN ('pending','running');
CREATE TABLE sync_checkpoints (
 workspace_id uuid NOT NULL REFERENCES workspaces ON DELETE CASCADE, account_id uuid NOT NULL,
 resource_kind text NOT NULL, data_through timestamptz NOT NULL,
 PRIMARY KEY(account_id,resource_kind), FOREIGN KEY(workspace_id,account_id) REFERENCES provider_accounts(workspace_id,id)
);
CREATE TABLE cost_entries (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id uuid NOT NULL REFERENCES workspaces ON DELETE CASCADE,
 account_id uuid NOT NULL, source_record_key text NOT NULL, revision integer NOT NULL,
 is_current boolean NOT NULL DEFAULT true, source_batch_id uuid NOT NULL, source_record_ref text NOT NULL,
 period_start timestamptz NOT NULL, period_end timestamptz NOT NULL CHECK(period_end>period_start),
 source_timezone text NOT NULL, billing_provider text NOT NULL, model_vendor text NOT NULL DEFAULT '',
 raw_model_name text NOT NULL DEFAULT '', charge_category text NOT NULL, cost_kind text NOT NULL,
 source_scope text NOT NULL, provider_project_ref text NOT NULL DEFAULT '',
 amount numeric(30,12) NOT NULL, currency char(3) NOT NULL,
 coverage_status text NOT NULL, source_quality text NOT NULL DEFAULT 'reported',
 finalization_status text NOT NULL DEFAULT 'provisional', price_basis text,
 dimensions jsonb NOT NULL DEFAULT '{}', ingested_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,id), FOREIGN KEY(workspace_id,account_id) REFERENCES provider_accounts(workspace_id,id),
 FOREIGN KEY(workspace_id,source_batch_id) REFERENCES source_batches(workspace_id,id)
);
CREATE UNIQUE INDEX current_cost_key ON cost_entries(workspace_id,account_id,source_record_key) WHERE is_current;
CREATE INDEX costs_period_idx ON cost_entries(workspace_id,period_start,id) WHERE is_current;
CREATE INDEX costs_account_idx ON cost_entries(workspace_id,account_id,period_start) WHERE is_current;
CREATE TABLE usage_buckets (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id uuid NOT NULL REFERENCES workspaces ON DELETE CASCADE,
 account_id uuid NOT NULL, source_batch_id uuid NOT NULL, source_record_key text NOT NULL,
 period_start timestamptz NOT NULL, period_end timestamptz NOT NULL, model text NOT NULL,
 metrics jsonb NOT NULL, dimensions jsonb NOT NULL DEFAULT '{}',
 UNIQUE(workspace_id,account_id,source_record_key),
 FOREIGN KEY(workspace_id,account_id) REFERENCES provider_accounts(workspace_id,id),
 FOREIGN KEY(workspace_id,source_batch_id) REFERENCES source_batches(workspace_id,id)
);
CREATE TABLE invoices (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id uuid NOT NULL REFERENCES workspaces ON DELETE CASCADE,
 account_id uuid NOT NULL, reference text NOT NULL, currency char(3) NOT NULL,
 period_start timestamptz NOT NULL, period_end timestamptz NOT NULL CHECK(period_end>period_start),
 amount numeric(30,12) NOT NULL, adjustment numeric(30,12) NOT NULL DEFAULT 0,
 source_scope text NOT NULL, coverage_confirmed boolean NOT NULL DEFAULT false,
 evidence_note text NOT NULL, source_type text NOT NULL DEFAULT 'manual', created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,id), UNIQUE(workspace_id,account_id,reference),
 FOREIGN KEY(workspace_id,account_id) REFERENCES provider_accounts(workspace_id,id)
);
CREATE TABLE reconciliation_runs (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id uuid NOT NULL REFERENCES workspaces ON DELETE CASCADE,
 invoice_id uuid NOT NULL, run_version integer NOT NULL, level text NOT NULL,
 expected numeric(30,12), billed numeric(30,12) NOT NULL, difference numeric(30,12),
 currency char(3) NOT NULL, tolerance numeric(30,12) NOT NULL DEFAULT 0.01, match_status text NOT NULL,
 evidence jsonb NOT NULL, handling_status text NOT NULL DEFAULT 'open' CHECK(handling_status IN ('open','explained','ignored')),
 handling_note text NOT NULL DEFAULT '', created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,id), UNIQUE(invoice_id,run_version),
 FOREIGN KEY(workspace_id,invoice_id) REFERENCES invoices(workspace_id,id)
);
CREATE TABLE alert_rules (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id uuid NOT NULL REFERENCES workspaces ON DELETE CASCADE,
 name text NOT NULL, kind text NOT NULL CHECK(kind IN ('budget','spike','sync_failure')),
 currency char(3) NOT NULL, amount numeric(30,12) NOT NULL CHECK(amount>0), enabled boolean NOT NULL DEFAULT true,
 created_at timestamptz NOT NULL DEFAULT now(), UNIQUE(workspace_id,id)
);
CREATE TABLE insights (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id uuid NOT NULL REFERENCES workspaces ON DELETE CASCADE,
 rule_key text NOT NULL, kind text NOT NULL, currency char(3) NOT NULL,
 evidence jsonb NOT NULL, estimated_savings numeric(30,12),
 state text NOT NULL DEFAULT 'open' CHECK(state IN ('open','applied','dismissed')),
 generated_at timestamptz NOT NULL DEFAULT now(), UNIQUE(workspace_id,rule_key)
);
CREATE TABLE notification_deliveries (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id uuid REFERENCES workspaces ON DELETE CASCADE,
 dedupe_key text NOT NULL UNIQUE, recipient text NOT NULL, locale text NOT NULL,
 template text NOT NULL, payload jsonb NOT NULL, state text NOT NULL DEFAULT 'pending',
 sent_at timestamptz, created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE exports (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id uuid NOT NULL REFERENCES workspaces ON DELETE CASCADE,
 filters jsonb NOT NULL, locale text NOT NULL, state text NOT NULL DEFAULT 'pending', object_key text,
 expires_at timestamptz NOT NULL DEFAULT now()+interval '24 hours', created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(workspace_id,id)
);
CREATE TABLE audit_events (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(), workspace_id uuid REFERENCES workspaces ON DELETE CASCADE,
 actor_id uuid REFERENCES users, action text NOT NULL, subject_id text, details jsonb NOT NULL DEFAULT '{}',
 created_at timestamptz NOT NULL DEFAULT now()
);
-- Durable tombstones intentionally survive workspace deletion and must be replayed after restore.
CREATE TABLE deletion_tombstones (workspace_id uuid PRIMARY KEY, requested_by uuid NOT NULL, requested_at timestamptz NOT NULL DEFAULT now(), completed_at timestamptz);

-- Financial rows are protected even when an application query omits a WHERE clause.
-- Use a non-superuser, non-BYPASSRLS runtime role. Every request/job uses SET LOCAL.
DO $$ DECLARE t text; BEGIN
 FOREACH t IN ARRAY ARRAY['source_batches','sync_checkpoints','cost_entries','usage_buckets','invoices','reconciliation_runs','insights','exports','audit_events'] LOOP
  EXECUTE format('ALTER TABLE %I ENABLE ROW LEVEL SECURITY', t);
  EXECUTE format('ALTER TABLE %I FORCE ROW LEVEL SECURITY', t);
  EXECUTE format('CREATE POLICY tenant_scope ON %I USING (workspace_id = nullif(current_setting(''app.workspace_id'',true),'''')::uuid) WITH CHECK (workspace_id = nullif(current_setting(''app.workspace_id'',true),'''')::uuid)',t);
 END LOOP;
END $$;
