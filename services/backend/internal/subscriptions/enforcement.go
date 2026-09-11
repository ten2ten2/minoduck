package subscriptions

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// LockAccount serializes quota/resource mutations, entitlement application and
// Stripe state changes across every workspace sharing one billing account.
func LockAccount(ctx context.Context, tx pgx.Tx, account string) error {
	_, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "billing-account:"+account)
	return err
}

// ApplyEntitlements deterministically suspends existing resources above the
// account-wide plan limits. Suspended rows are retained for recovery after an
// upgrade, but they cannot authorize users, sync providers or send alerts.
func ApplyEntitlements(ctx context.Context, tx pgx.Tx, account string, plan Plan) error {
	statements := []struct {
		SQL  string
		Args []any
	}{
		{`WITH ranked AS (
 SELECT id,row_number() OVER(ORDER BY created_at,id) AS position
 FROM workspaces WHERE billing_account_id=$1 AND deletion_requested_at IS NULL
) UPDATE workspaces w SET billing_suspended=(r.position>$2)
FROM ranked r WHERE w.id=r.id`, []any{account, plan.Workspaces}},
		{`WITH owner AS (SELECT owner_id FROM billing_accounts WHERE id=$1), ranked AS (
 SELECT user_id,row_number() OVER(ORDER BY bool_or(user_id=owner.owner_id) DESC,min(m.created_at),user_id) AS position
 FROM workspace_members m JOIN workspaces w ON w.id=m.workspace_id CROSS JOIN owner
 WHERE w.billing_account_id=$1 AND w.deletion_requested_at IS NULL
 GROUP BY user_id
) UPDATE workspace_members m SET billing_suspended=(r.position>$2)
FROM ranked r,workspaces w WHERE m.user_id=r.user_id AND m.workspace_id=w.id AND w.billing_account_id=$1`, []any{account, plan.Members}},
		{`WITH ranked AS (
 SELECT a.id,a.workspace_id,row_number() OVER(ORDER BY a.created_at,a.id) AS position
 FROM provider_accounts a JOIN workspaces w ON w.id=a.workspace_id
 WHERE w.billing_account_id=$1 AND w.deletion_requested_at IS NULL AND a.status<>'disconnected'
) UPDATE provider_accounts a SET billing_suspended=(r.position>$2 OR w.billing_suspended)
FROM ranked r,workspaces w WHERE a.id=r.id AND a.workspace_id=w.id`, []any{account, plan.Connections}},
		{`WITH ranked AS (
 SELECT a.id,a.workspace_id,row_number() OVER(ORDER BY a.created_at,a.id) AS position
 FROM alert_rules a JOIN workspaces w ON w.id=a.workspace_id
 WHERE w.billing_account_id=$1 AND w.deletion_requested_at IS NULL
) UPDATE alert_rules a SET billing_suspended=(r.position>$2 OR w.billing_suspended)
FROM ranked r,workspaces w WHERE a.id=r.id AND a.workspace_id=w.id`, []any{account, plan.Budgets}},
	}
	for _, statement := range statements {
		if _, err := tx.Exec(ctx, statement.SQL, statement.Args...); err != nil {
			return err
		}
	}
	return nil
}
