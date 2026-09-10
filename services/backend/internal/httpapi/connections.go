package httpapi

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/ten2ten2/minoduck/services/backend/internal/platform"
	"github.com/ten2ten2/minoduck/services/backend/internal/tasks"
	"strings"
)

func providerCapabilities() any {
	return []gin.H{
		{"id": "openai", "name": "OpenAI", "credential_kind": "admin_key", "supports_usage": true, "supports_cost": true, "supports_invoice": false, "supports_account_identity_validation": false, "history_limit_days": 90, "granularity": "completed_utc_day", "schema_version": "2026-09-10.1", "warning_key": "providers.openaiWarning", "native": true},
		{"id": "anthropic", "name": "Anthropic", "credential_kind": "admin_key", "supports_usage": true, "supports_cost": true, "supports_invoice": false, "supports_account_identity_validation": false, "history_limit_days": 90, "granularity": "completed_utc_day", "schema_version": "2026-09-10.1", "warning_key": "providers.anthropicWarning", "known_exclusions": []string{"priority_tier", "aws_bedrock", "vertex"}, "native": true},
		{"id": "openrouter", "name": "OpenRouter", "credential_kind": "management_key", "supports_usage": true, "supports_cost": true, "supports_invoice": false, "supports_account_identity_validation": false, "history_limit_days": 30, "granularity": "completed_utc_day", "schema_version": "2026-09-10.1", "warning_key": "providers.openrouterWarning", "native": true},
		{"id": "csv", "name": "CSV", "credential_kind": "none", "supports_usage": false, "supports_cost": true, "supports_invoice": true, "supports_account_identity_validation": false, "warning_key": "providers.csvWarning", "native": false},
	}
}

const connectionJSON = `json_build_object('id',a.id,'provider',a.provider,'name',a.name,'external_account_ref',a.external_account_ref,'status',a.status,'credential_suffix',a.credential_suffix,'data_through',a.data_through,'last_sync_at',a.last_sync_at,'next_sync_at',a.next_sync_at,'error_code',a.error_code,'identity_verified',false)`

func (s *Server) connections(c *gin.Context, tx pgx.Tx) (any, error) {
	return platform.JSONRows(c.Request.Context(), tx, `SELECT `+connectionJSON+` FROM provider_accounts a WHERE workspace_id=$1 ORDER BY created_at,id`, c.Param("wid"))
}
func (s *Server) connection(c *gin.Context, tx pgx.Tx) (any, error) {
	var b []byte
	e := tx.QueryRow(c.Request.Context(), `SELECT `+connectionJSON+` FROM provider_accounts a WHERE workspace_id=$1 AND id=$2`, c.Param("wid"), c.Param("cid")).Scan(&b)
	return json.RawMessage(b), e
}
func (s *Server) createConnection(c *gin.Context, tx pgx.Tx) (any, error) {
	var in struct {
		Provider   string `json:"provider"`
		Name       string `json:"name"`
		AccountRef string `json:"account_ref"`
		Credential string `json:"credential"`
	}
	if e := bind(c, &in); e != nil {
		return nil, e
	}
	if (in.Provider != "openai" && in.Provider != "anthropic" && in.Provider != "openrouter" && in.Provider != "csv") || strings.TrimSpace(in.Name) == "" || len(in.Name) > 100 || strings.TrimSpace(in.AccountRef) == "" || len(in.AccountRef) > 160 || len(in.Credential) > 4096 {
		return nil, bad("INVALID_CONNECTION")
	}
	if e := s.lockAccount(c, tx); e != nil {
		return nil, e
	}
	p, e := s.plan(c, tx)
	if e != nil {
		return nil, e
	}
	var n int
	e = tx.QueryRow(c.Request.Context(), `SELECT count(*) FROM provider_accounts a JOIN workspaces w ON w.id=a.workspace_id WHERE w.billing_account_id=$1 AND a.status<>'disconnected'`, c.GetString("billing_account_id")).Scan(&n)
	if e != nil {
		return nil, e
	}
	if n >= p.Connections {
		return nil, APIError{"CONNECTION_LIMIT", 402}
	}
	var duplicate bool
	e = tx.QueryRow(c.Request.Context(), `SELECT EXISTS(SELECT 1 FROM provider_accounts a JOIN workspaces w ON w.id=a.workspace_id WHERE w.billing_account_id=$1 AND a.provider=$2 AND a.external_account_ref=$3)`, c.GetString("billing_account_id"), in.Provider, in.AccountRef).Scan(&duplicate)
	if e != nil {
		return nil, e
	}
	if duplicate {
		return nil, APIError{"ACCOUNT_ALREADY_CONNECTED", 409}
	}
	id := uuid.NewString()
	var encrypted []byte
	status, suffix := "ready", ""
	if in.Provider != "csv" {
		if len(in.Credential) < 10 {
			return nil, bad("INVALID_CREDENTIAL")
		}
		encrypted, e = platform.Encrypt(s.Config.MasterKey, in.Credential, c.Param("wid")+":"+id)
		if e != nil {
			return nil, APIError{"ENCRYPTION_NOT_CONFIGURED", 503}
		}
		suffix = in.Credential[len(in.Credential)-4:]
		status = "validating"
	}
	_, e = tx.Exec(c.Request.Context(), `INSERT INTO provider_accounts(id,workspace_id,provider,name,external_account_ref,status,credential_cipher,credential_key_id,credential_suffix,next_sync_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,now())`, id, c.Param("wid"), in.Provider, in.Name, in.AccountRef, status, encrypted, s.Config.KeyID, suffix)
	if e != nil {
		return nil, e
	}
	run, e := tasks.EnqueueSync(c.Request.Context(), tx, s.Queue, c.Param("wid"), id)
	if e != nil {
		return nil, e
	}
	c.Set("response_status", 201)
	return gin.H{"id": id, "sync_run_id": run}, platform.Audit(c.Request.Context(), tx, c.Param("wid"), session(c).UserID, "connection.created", id, gin.H{"provider": in.Provider})
}
func (s *Server) syncConnection(c *gin.Context, tx pgx.Tx) (any, error) {
	id, e := tasks.EnqueueSync(c.Request.Context(), tx, s.Queue, c.Param("wid"), c.Param("cid"))
	if e != nil {
		return nil, e
	}
	if id == "" {
		return nil, bad("SYNC_NOT_AVAILABLE")
	}
	c.Set("response_status", 202)
	return gin.H{"id": id}, nil
}
func (s *Server) replaceCredential(c *gin.Context, tx pgx.Tx) (any, error) {
	var in struct {
		Credential string `json:"credential"`
	}
	if e := bind(c, &in); e != nil {
		return nil, e
	}
	if len(in.Credential) < 10 || len(in.Credential) > 4096 {
		return nil, bad("INVALID_CREDENTIAL")
	}
	var status, provider string
	e := tx.QueryRow(c.Request.Context(), `SELECT status,provider FROM provider_accounts WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, c.Param("wid"), c.Param("cid")).Scan(&status, &provider)
	if e != nil {
		return nil, e
	}
	if provider == "csv" || status == "disconnected" {
		return nil, bad("CONNECTION_DISCONNECTED")
	}
	encrypted, e := platform.Encrypt(s.Config.MasterKey, in.Credential, c.Param("wid")+":"+c.Param("cid"))
	if e != nil {
		return nil, APIError{"ENCRYPTION_NOT_CONFIGURED", 503}
	}
	if _, e = tx.Exec(c.Request.Context(), `UPDATE provider_accounts SET credential_cipher=$1,credential_key_id=$2,credential_suffix=$3,generation=generation+1,status='validating',error_code=NULL WHERE workspace_id=$4 AND id=$5`, encrypted, s.Config.KeyID, in.Credential[len(in.Credential)-4:], c.Param("wid"), c.Param("cid")); e != nil {
		return nil, e
	}
	if _, e = tx.Exec(c.Request.Context(), `UPDATE sync_runs SET state='canceled' WHERE workspace_id=$1 AND account_id=$2 AND state IN ('pending','running')`, c.Param("wid"), c.Param("cid")); e != nil {
		return nil, e
	}
	id, e := tasks.EnqueueSync(c.Request.Context(), tx, s.Queue, c.Param("wid"), c.Param("cid"))
	if e != nil {
		return nil, e
	}
	return gin.H{"sync_run_id": id}, platform.Audit(c.Request.Context(), tx, c.Param("wid"), session(c).UserID, "credential.replaced", c.Param("cid"), gin.H{})
}
func (s *Server) disconnect(c *gin.Context, tx pgx.Tx) (any, error) {
	result, e := tx.Exec(c.Request.Context(), `UPDATE provider_accounts SET status='disconnected',credential_cipher=NULL,credential_suffix=NULL,next_sync_at=NULL,generation=generation+1 WHERE workspace_id=$1 AND id=$2`, c.Param("wid"), c.Param("cid"))
	if e != nil {
		return nil, e
	}
	if result.RowsAffected() == 0 {
		return nil, pgx.ErrNoRows
	}
	if _, e = tx.Exec(c.Request.Context(), `UPDATE sync_runs SET state='canceled' WHERE workspace_id=$1 AND account_id=$2 AND state IN ('pending','running')`, c.Param("wid"), c.Param("cid")); e != nil {
		return nil, e
	}
	return gin.H{"status": "disconnected"}, platform.Audit(c.Request.Context(), tx, c.Param("wid"), session(c).UserID, "connection.disconnected", c.Param("cid"), gin.H{})
}
func (s *Server) syncRun(c *gin.Context, tx pgx.Tx) (any, error) {
	var b []byte
	e := tx.QueryRow(c.Request.Context(), `SELECT to_jsonb(r) FROM sync_runs r WHERE workspace_id=$1 AND id=$2`, c.Param("wid"), c.Param("rid")).Scan(&b)
	return json.RawMessage(b), e
}
