package httpapi

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/ten2ten2/minoduck/services/backend/internal/ledger"
	"github.com/ten2ten2/minoduck/services/backend/internal/platform"
	"github.com/ten2ten2/minoduck/services/backend/internal/tasks"
	"strings"
)

func (s *Server) alerts(c *gin.Context, tx pgx.Tx) (any, error) {
	return platform.JSONRows(c.Request.Context(), tx, `SELECT to_jsonb(r)||jsonb_build_object('amount',amount::text) FROM alert_rules r WHERE workspace_id=$1 ORDER BY created_at,id`, c.Param("wid"))
}

type alertInput struct {
	Name     string `json:"name"`
	Kind     string `json:"kind"`
	Currency string `json:"currency"`
	Amount   string `json:"amount"`
	Enabled  bool   `json:"enabled"`
}

func validateAlert(v alertInput) error {
	a, e := ledger.Amount(v.Amount)
	if e != nil || !a.IsPositive() {
		return bad("INVALID_AMOUNT")
	}
	if strings.TrimSpace(v.Name) == "" || len(v.Name) > 100 || !ledger.ValidCurrency(v.Currency) || (v.Kind != "budget" && v.Kind != "spike" && v.Kind != "sync_failure") {
		return bad("INVALID_ALERT")
	}
	return nil
}
func (s *Server) createAlert(c *gin.Context, tx pgx.Tx) (any, error) {
	var in alertInput
	if e := bind(c, &in); e != nil {
		return nil, e
	}
	if e := validateAlert(in); e != nil {
		return nil, e
	}
	if e := s.lockAccount(c, tx); e != nil {
		return nil, e
	}
	p, e := s.plan(c, tx)
	if e != nil {
		return nil, e
	}
	if in.Kind != "budget" && !p.Details {
		return nil, APIError{"UPGRADE_REQUIRED", 402}
	}
	var count int
	e = tx.QueryRow(c.Request.Context(), `SELECT count(*) FROM alert_rules a JOIN workspaces w ON w.id=a.workspace_id WHERE w.billing_account_id=$1`, c.GetString("billing_account_id")).Scan(&count)
	if e != nil {
		return nil, e
	}
	if count >= p.Budgets {
		return nil, APIError{"BUDGET_LIMIT", 402}
	}
	id := uuid.NewString()
	_, e = tx.Exec(c.Request.Context(), `INSERT INTO alert_rules(id,workspace_id,name,kind,currency,amount,enabled) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, c.Param("wid"), in.Name, in.Kind, in.Currency, in.Amount, in.Enabled)
	if e != nil {
		return nil, e
	}
	if _, e = s.Queue.InsertTx(c.Request.Context(), tx, tasks.Args{Task: "insights", WorkspaceID: c.Param("wid"), ResourceID: c.Param("wid")}, nil); e != nil {
		return nil, e
	}
	c.Set("response_status", 201)
	return gin.H{"id": id}, nil
}
func (s *Server) updateAlert(c *gin.Context, tx pgx.Tx) (any, error) {
	var in alertInput
	if e := bind(c, &in); e != nil {
		return nil, e
	}
	if e := validateAlert(in); e != nil {
		return nil, e
	}
	if in.Kind != "budget" {
		if e := s.paid(c, tx); e != nil {
			return nil, e
		}
	}
	r, e := tx.Exec(c.Request.Context(), `UPDATE alert_rules SET name=$1,kind=$2,currency=$3,amount=$4,enabled=$5 WHERE workspace_id=$6 AND id=$7`, in.Name, in.Kind, in.Currency, in.Amount, in.Enabled, c.Param("wid"), c.Param("aid"))
	if e != nil {
		return nil, e
	}
	if r.RowsAffected() == 0 {
		return nil, pgx.ErrNoRows
	}
	return gin.H{"status": "saved"}, nil
}
func (s *Server) deleteAlert(c *gin.Context, tx pgx.Tx) (any, error) {
	r, e := tx.Exec(c.Request.Context(), `DELETE FROM alert_rules WHERE workspace_id=$1 AND id=$2`, c.Param("wid"), c.Param("aid"))
	if e != nil {
		return nil, e
	}
	if r.RowsAffected() == 0 {
		return nil, pgx.ErrNoRows
	}
	return gin.H{"status": "deleted"}, nil
}
func (s *Server) insights(c *gin.Context, tx pgx.Tx) (any, error) {
	p, e := s.plan(c, tx)
	if e != nil {
		return nil, e
	}
	if !p.Details {
		return platform.JSONRows(c.Request.Context(), tx, `SELECT json_build_object('id',id,'kind',kind,'currency',currency,'estimated_savings',estimated_savings::text,'state',state,'generated_at',generated_at,'details_locked',true) FROM insights WHERE workspace_id=$1 ORDER BY generated_at DESC,id LIMIT 100`, c.Param("wid"))
	}
	return platform.JSONRows(c.Request.Context(), tx, `SELECT to_jsonb(i)||jsonb_build_object('estimated_savings',estimated_savings::text,'details_locked',false) FROM insights i WHERE workspace_id=$1 ORDER BY generated_at DESC,id LIMIT 100`, c.Param("wid"))
}
func (s *Server) updateInsight(c *gin.Context, tx pgx.Tx) (any, error) {
	if e := s.paid(c, tx); e != nil {
		return nil, e
	}
	var in struct {
		State string `json:"state"`
	}
	if e := bind(c, &in); e != nil {
		return nil, e
	}
	if in.State != "open" && in.State != "applied" && in.State != "dismissed" {
		return nil, bad("INVALID_STATE")
	}
	r, e := tx.Exec(c.Request.Context(), `UPDATE insights SET state=$1 WHERE workspace_id=$2 AND id=$3`, in.State, c.Param("wid"), c.Param("iid"))
	if e != nil {
		return nil, e
	}
	if r.RowsAffected() == 0 {
		return nil, pgx.ErrNoRows
	}
	return gin.H{"status": "saved"}, platform.Audit(c.Request.Context(), tx, c.Param("wid"), session(c).UserID, "insight.updated", c.Param("iid"), in)
}
func (s *Server) exports(c *gin.Context, tx pgx.Tx) (any, error) {
	return platform.JSONRows(c.Request.Context(), tx, `SELECT json_build_object('id',id,'state',state,'expires_at',expires_at,'created_at',created_at) FROM exports WHERE workspace_id=$1 ORDER BY created_at DESC LIMIT 100`, c.Param("wid"))
}
func (s *Server) createExport(c *gin.Context, tx pgx.Tx) (any, error) {
	if e := s.paid(c, tx); e != nil {
		return nil, e
	}
	f, e := s.filter(c, tx)
	if e != nil {
		return nil, e
	}
	filters, _ := json.Marshal(f)
	id := uuid.NewString()
	_, e = tx.Exec(c.Request.Context(), `INSERT INTO exports(id,workspace_id,filters,locale) VALUES($1,$2,$3,$4)`, id, c.Param("wid"), filters, session(c).Locale)
	if e != nil {
		return nil, e
	}
	_, e = s.Queue.InsertTx(c.Request.Context(), tx, tasks.Args{Task: "export", WorkspaceID: c.Param("wid"), ResourceID: id}, nil)
	c.Set("response_status", 202)
	return gin.H{"id": id, "status": "pending"}, e
}
func (s *Server) exportStatus(c *gin.Context, tx pgx.Tx) (any, error) {
	var b []byte
	e := tx.QueryRow(c.Request.Context(), `SELECT json_build_object('id',id,'state',state,'expires_at',expires_at) FROM exports WHERE workspace_id=$1 AND id=$2`, c.Param("wid"), c.Param("eid")).Scan(&b)
	return json.RawMessage(b), e
}
func (s *Server) downloadExport(c *gin.Context) {
	if _, e := uuid.Parse(c.Param("wid")); e != nil {
		s.fail(c, bad("INVALID_WORKSPACE"))
		return
	}
	if _, e := uuid.Parse(c.Param("eid")); e != nil {
		s.fail(c, bad("INVALID_ID"))
		return
	}
	ctx := c.Request.Context()
	tx, e := platform.TenantTx(ctx, s.DB, c.Param("wid"))
	if e != nil {
		s.fail(c, e)
		return
	}
	defer tx.Rollback(ctx)
	var key string
	e = tx.QueryRow(ctx, `SELECT x.object_key FROM exports x JOIN workspace_members m ON m.workspace_id=x.workspace_id JOIN workspaces w ON w.id=x.workspace_id WHERE x.workspace_id=$1 AND x.id=$2 AND m.user_id=$3 AND w.deletion_requested_at IS NULL AND x.state='ready' AND x.expires_at>now()`, c.Param("wid"), c.Param("eid"), session(c).UserID).Scan(&key)
	if e != nil {
		s.fail(c, pgx.ErrNoRows)
		return
	}
	var account string
	if e = tx.QueryRow(ctx, `SELECT billing_account_id FROM workspaces WHERE id=$1`, c.Param("wid")).Scan(&account); e != nil {
		s.fail(c, e)
		return
	}
	c.Set("billing_account_id", account)
	if e = s.paid(c, tx); e != nil {
		s.fail(c, e)
		return
	}
	data, e := s.Objects.Get(ctx, key)
	if e != nil {
		s.fail(c, APIError{"STORAGE_UNAVAILABLE", 503})
		return
	}
	c.Header("Content-Disposition", `attachment; filename="minoduck-costs.csv"`)
	c.Data(200, "text/csv; charset=utf-8", data)
}
