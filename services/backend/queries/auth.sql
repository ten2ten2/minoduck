-- name: TenantMembership :one
SELECT m.role, w.billing_account_id, w.billing_suspended
FROM workspace_members m JOIN workspaces w ON w.id=m.workspace_id
WHERE m.workspace_id=sqlc.arg(workspace_id) AND m.user_id=sqlc.arg(user_id)
AND w.deletion_requested_at IS NULL AND NOT m.billing_suspended;

-- name: SessionUser :one
SELECT u.id,u.email,u.locale,u.theme,s.csrf_token,u.locale_explicit
FROM sessions s JOIN users u ON u.id=s.user_id
WHERE s.token_hash=sqlc.arg(token_hash) AND s.expires_at>now();
