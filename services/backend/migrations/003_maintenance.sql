ALTER TABLE workspaces ADD COLUMN next_maintenance_at timestamptz NOT NULL DEFAULT now();
ALTER TABLE subscriptions ADD COLUMN retention_grace_until timestamptz;
