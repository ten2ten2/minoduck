package httpapi

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/ten2ten2/minoduck/services/backend/internal/platform"
	"github.com/ten2ten2/minoduck/services/backend/internal/tasks"
	"net/mail"
	"regexp"
	"strings"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,47}$`)

func (s *Server) listWorkspaces(c *gin.Context) {
	rows, e := s.DB.Query(c.Request.Context(), `SELECT json_build_object('id',w.id,'slug',w.slug,'name',w.name,'role',m.role) FROM workspaces w JOIN workspace_members m ON m.workspace_id=w.id WHERE m.user_id=$1 AND w.deletion_requested_at IS NULL ORDER BY w.created_at,w.id`, session(c).UserID)
	if e != nil {
		s.fail(c, e)
		return
	}
	defer rows.Close()
	data := []json.RawMessage{}
	for rows.Next() {
		var b []byte
		if e = rows.Scan(&b); e != nil {
			s.fail(c, e)
			return
		}
		data = append(data, b)
	}
	if e = rows.Err(); e != nil {
		s.fail(c, e)
		return
	}
	c.JSON(200, data)
}
func (s *Server) createWorkspace(c *gin.Context) {
	var in struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if e := bind(c, &in); e != nil {
		s.fail(c, e)
		return
	}
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 100 || !slugPattern.MatchString(in.Slug) {
		s.fail(c, bad("INVALID_WORKSPACE"))
		return
	}
	ctx := c.Request.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		s.fail(c, e)
		return
	}
	defer tx.Rollback(ctx)
	var account string
	e = tx.QueryRow(ctx, `INSERT INTO billing_accounts(owner_id) VALUES($1) ON CONFLICT(owner_id) DO UPDATE SET owner_id=excluded.owner_id RETURNING id`, session(c).UserID).Scan(&account)
	if e != nil {
		s.fail(c, e)
		return
	}
	_, e = tx.Exec(ctx, `INSERT INTO subscriptions(billing_account_id) VALUES($1) ON CONFLICT DO NOTHING`, account)
	if e != nil {
		s.fail(c, e)
		return
	}
	c.Set("billing_account_id", account)
	p, e := s.plan(c, tx)
	if e != nil {
		s.fail(c, e)
		return
	}
	var n int
	e = tx.QueryRow(ctx, `SELECT count(*) FROM workspaces WHERE billing_account_id=$1 AND deletion_requested_at IS NULL`, account).Scan(&n)
	if e != nil {
		s.fail(c, e)
		return
	}
	if n >= p.Workspaces {
		s.fail(c, APIError{"WORKSPACE_LIMIT", 402})
		return
	}
	var id string
	e = tx.QueryRow(ctx, `INSERT INTO workspaces(billing_account_id,name,slug) VALUES($1,$2,$3) ON CONFLICT(slug) DO NOTHING RETURNING id`, account, in.Name, in.Slug).Scan(&id)
	if e == pgx.ErrNoRows {
		s.fail(c, APIError{"SLUG_TAKEN", 409})
		return
	}
	if e != nil {
		s.fail(c, e)
		return
	}
	if _, e = tx.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,'owner')`, id, session(c).UserID); e != nil {
		s.fail(c, e)
		return
	}
	if e = tx.Commit(ctx); e != nil {
		s.fail(c, e)
		return
	}
	c.JSON(201, gin.H{"id": id, "slug": in.Slug, "name": in.Name, "role": "owner"})
}
func (s *Server) workspace(c *gin.Context, tx pgx.Tx) (any, error) {
	var b []byte
	e := tx.QueryRow(c.Request.Context(), `SELECT json_build_object('id',id,'slug',slug,'name',name) FROM workspaces WHERE id=$1`, c.Param("wid")).Scan(&b)
	return json.RawMessage(b), e
}
func (s *Server) updateWorkspace(c *gin.Context, tx pgx.Tx) (any, error) {
	var in struct {
		Name string `json:"name"`
	}
	if e := bind(c, &in); e != nil {
		return nil, e
	}
	if strings.TrimSpace(in.Name) == "" || len(in.Name) > 100 {
		return nil, bad("INVALID_WORKSPACE")
	}
	_, e := tx.Exec(c.Request.Context(), `UPDATE workspaces SET name=$1 WHERE id=$2`, in.Name, c.Param("wid"))
	if e != nil {
		return nil, e
	}
	return gin.H{"status": "saved"}, platform.Audit(c.Request.Context(), tx, c.Param("wid"), session(c).UserID, "workspace.updated", c.Param("wid"), in)
}
func (s *Server) members(c *gin.Context, tx pgx.Tx) (any, error) {
	return platform.JSONRows(c.Request.Context(), tx, `SELECT json_build_object('id',u.id,'email',u.email,'role',m.role,'created_at',m.created_at) FROM workspace_members m JOIN users u ON u.id=m.user_id WHERE m.workspace_id=$1 ORDER BY m.created_at,u.id`, c.Param("wid"))
}
func (s *Server) lockAccount(c *gin.Context, tx pgx.Tx) error {
	_, e := tx.Exec(c.Request.Context(), `SELECT id FROM billing_accounts WHERE id=$1 FOR UPDATE`, c.GetString("billing_account_id"))
	return e
}
func (s *Server) invite(c *gin.Context, tx pgx.Tx) (any, error) {
	var in struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if e := bind(c, &in); e != nil {
		return nil, e
	}
	a, e := mail.ParseAddress(in.Email)
	if e != nil || a.Address != in.Email || len(in.Email) > 254 || (in.Role != "admin" && in.Role != "viewer") {
		return nil, bad("INVALID_INVITATION")
	}
	if in.Role == "admin" && c.GetString("role") != "owner" {
		return nil, forbidden("INSUFFICIENT_ROLE")
	}
	if e = s.lockAccount(c, tx); e != nil {
		return nil, e
	}
	p, e := s.plan(c, tx)
	if e != nil {
		return nil, e
	}
	email := strings.ToLower(a.Address)
	var count int
	e = tx.QueryRow(c.Request.Context(), `SELECT count(DISTINCT email) FROM (SELECT u.email FROM workspace_members m JOIN users u ON u.id=m.user_id JOIN workspaces w ON w.id=m.workspace_id WHERE w.billing_account_id=$1 UNION SELECT i.email FROM invitations i JOIN workspaces w ON w.id=i.workspace_id WHERE w.billing_account_id=$1 AND i.accepted_at IS NULL AND i.expires_at>now() UNION SELECT $2::text) seats`, c.GetString("billing_account_id"), email).Scan(&count)
	if e != nil {
		return nil, e
	}
	if count > p.Members {
		return nil, APIError{"MEMBER_LIMIT", 402}
	}
	// The account lock serializes invitation changes across workspaces. Rotate
	// an existing token for this workspace/email so only the latest link works.
	// If delivery fails, the surrounding transaction rolls this deletion back.
	if _, e = tx.Exec(c.Request.Context(), `DELETE FROM invitations WHERE workspace_id=$1 AND email=$2 AND accepted_at IS NULL`, c.Param("wid"), email); e != nil {
		return nil, e
	}
	id, token := uuid.NewString(), platform.RandomToken()
	_, e = tx.Exec(c.Request.Context(), `INSERT INTO invitations(id,workspace_id,email,role,token_hash,expires_at) VALUES($1,$2,$3,$4,$5,now()+interval '7 days')`, id, c.Param("wid"), email, in.Role, platform.TokenHash(token))
	if e != nil {
		return nil, e
	}
	link := s.Config.AppURL + "/invitations/accept?token=" + token
	audit := func() error {
		return platform.Audit(c.Request.Context(), tx, c.Param("wid"), session(c).UserID, "member.invited", id, gin.H{"role": in.Role})
	}
	if s.Config.Env != "production" && s.Config.ResendKey == "" {
		return gin.H{"id": id, "development_link": link}, audit()
	}
	subject, body := "Your MinoDuck invitation", "Sign in with "+email+", then open this invitation:\n"+link
	if session(c).Locale == "zh-hans" {
		subject = "MinoDuck 邀请"
		body = "请使用 " + email + " 登录，再打开邀请：\n" + link
	}
	if session(c).Locale == "zh-hant" {
		subject = "MinoDuck 邀請"
		body = "請使用 " + email + " 登入，再開啟邀請：\n" + link
	}
	if e = platform.SendMail(c.Request.Context(), s.Config, email, subject, body, id); e != nil {
		return nil, APIError{"EMAIL_DELIVERY_FAILED", 503}
	}
	return gin.H{"id": id}, audit()
}
func (s *Server) acceptInvitation(c *gin.Context) {
	var in struct {
		Token string `json:"token"`
	}
	if e := bind(c, &in); e != nil {
		s.fail(c, e)
		return
	}
	ctx := c.Request.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		s.fail(c, e)
		return
	}
	defer tx.Rollback(ctx)
	var id, wid, role, account string
	e = tx.QueryRow(ctx, `SELECT i.id,i.workspace_id,i.role,w.billing_account_id FROM invitations i JOIN workspaces w ON w.id=i.workspace_id WHERE i.token_hash=$1 AND i.email=$2 AND i.expires_at>now() AND i.accepted_at IS NULL AND w.deletion_requested_at IS NULL FOR UPDATE OF i`, platform.TokenHash(in.Token), session(c).Email).Scan(&id, &wid, &role, &account)
	if e != nil {
		s.fail(c, bad("INVALID_INVITATION"))
		return
	}
	c.Set("billing_account_id", account)
	if e = s.lockAccount(c, tx); e != nil {
		s.fail(c, e)
		return
	}
	p, e := s.plan(c, tx)
	if e != nil {
		s.fail(c, e)
		return
	}
	var n int
	e = tx.QueryRow(ctx, `SELECT count(DISTINCT user_id) FROM (SELECT m.user_id FROM workspace_members m JOIN workspaces w ON w.id=m.workspace_id WHERE w.billing_account_id=$1 UNION SELECT $2::uuid) s`, account, session(c).UserID).Scan(&n)
	if e != nil {
		s.fail(c, e)
		return
	}
	if n > p.Members {
		s.fail(c, APIError{"MEMBER_LIMIT", 402})
		return
	}
	if _, e = tx.Exec(ctx, `INSERT INTO workspace_members(workspace_id,user_id,role) VALUES($1,$2,$3) ON CONFLICT DO NOTHING`, wid, session(c).UserID, role); e != nil {
		s.fail(c, e)
		return
	}
	if _, e = tx.Exec(ctx, `UPDATE invitations SET accepted_at=now() WHERE id=$1`, id); e != nil {
		s.fail(c, e)
		return
	}
	if e = tx.Commit(ctx); e != nil {
		s.fail(c, e)
		return
	}
	c.JSON(200, gin.H{"workspace_id": wid})
}
func (s *Server) removeMember(c *gin.Context, tx pgx.Tx) (any, error) {
	var role string
	e := tx.QueryRow(c.Request.Context(), `SELECT role FROM workspace_members WHERE workspace_id=$1 AND user_id=$2 FOR UPDATE`, c.Param("wid"), c.Param("memberId")).Scan(&role)
	if e != nil {
		return nil, e
	}
	if role == "owner" || (role == "admin" && c.GetString("role") != "owner") {
		return nil, forbidden("OWNER_REQUIRED")
	}
	_, e = tx.Exec(c.Request.Context(), `DELETE FROM workspace_members WHERE workspace_id=$1 AND user_id=$2`, c.Param("wid"), c.Param("memberId"))
	if e != nil {
		return nil, e
	}
	return gin.H{"status": "removed"}, platform.Audit(c.Request.Context(), tx, c.Param("wid"), session(c).UserID, "member.removed", c.Param("memberId"), gin.H{})
}
func (s *Server) deleteWorkspace(c *gin.Context, tx pgx.Tx) (any, error) {
	var in struct {
		Confirm string `json:"confirm"`
	}
	if e := bind(c, &in); e != nil {
		return nil, e
	}
	if in.Confirm != c.Param("wid") {
		return nil, bad("CONFIRMATION_REQUIRED")
	}
	ctx := c.Request.Context()
	wid := c.Param("wid")
	if _, e := tx.Exec(ctx, `UPDATE workspaces SET deletion_requested_at=now() WHERE id=$1`, wid); e != nil {
		return nil, e
	}
	if _, e := tx.Exec(ctx, `UPDATE provider_accounts SET credential_cipher=NULL,status='disconnected',generation=generation+1,next_sync_at=NULL WHERE workspace_id=$1`, wid); e != nil {
		return nil, e
	}
	if _, e := tx.Exec(ctx, `INSERT INTO deletion_tombstones(workspace_id,requested_by) VALUES($1,$2) ON CONFLICT DO NOTHING`, wid, session(c).UserID); e != nil {
		return nil, e
	}
	_, e := s.Queue.InsertTx(ctx, tx, tasks.Args{Task: "delete_workspace", WorkspaceID: wid, ResourceID: wid}, nil)
	c.Set("response_status", 202)
	return gin.H{"status": "deletion_scheduled"}, e
}
