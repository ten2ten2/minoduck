package httpapi

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/ten2ten2/minoduck/services/backend/internal/ledger"
	"github.com/ten2ten2/minoduck/services/backend/internal/platform"
	"github.com/ten2ten2/minoduck/services/backend/internal/tasks"
	"io"
)

func (s *Server) upload(c *gin.Context, tx pgx.Tx) (any, error) {
	f, e := c.FormFile("file")
	if e != nil {
		return nil, bad("INVALID_FILE")
	}
	if f.Size > ledger.MaxFileBytes {
		return nil, bad("FILE_TOO_LARGE")
	}
	file, e := f.Open()
	if e != nil {
		return nil, bad("INVALID_FILE")
	}
	defer file.Close()
	data, e := io.ReadAll(io.LimitReader(file, ledger.MaxFileBytes+1))
	if e != nil {
		return nil, bad("INVALID_FILE")
	}
	aid := c.PostForm("account_id")
	if _, e = uuid.Parse(aid); e != nil {
		return nil, bad("INVALID_CONNECTION")
	}
	var provider, status string
	e = tx.QueryRow(c.Request.Context(), `SELECT provider,status FROM provider_accounts WHERE workspace_id=$1 AND id=$2`, c.Param("wid"), aid).Scan(&provider, &status)
	if e != nil {
		return nil, e
	}
	if status == "disconnected" {
		return nil, bad("CONNECTION_DISCONNECTED")
	}
	scope, zone, kind, granularity := c.PostForm("source_scope"), c.PostForm("timezone"), c.PostForm("cost_kind"), c.PostForm("granularity")
	if scope == "native-cost" {
		return nil, bad("RESERVED_SOURCE_SCOPE")
	}
	p, e := ledger.ParseCSV(data, provider, scope, zone, kind, granularity)
	if e != nil {
		return nil, bad(e.Error())
	}
	id := uuid.NewString()
	key := c.Param("wid") + "/" + id + ".csv"
	state := "preview_ready"
	if p.Rejected > 0 {
		state = "rejected"
	}
	if e = s.Objects.Put(c.Request.Context(), key, data); e != nil {
		return nil, APIError{"STORAGE_UNAVAILABLE", 503}
	}
	preview, _ := json.Marshal(p)
	_, e = tx.Exec(c.Request.Context(), `INSERT INTO source_batches(id,workspace_id,account_id,object_key,content_hash,source_scope,cost_kind,source_timezone,granularity,state,preview) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`, id, c.Param("wid"), aid, key, p.Hash, scope, kind, zone, granularity, state, preview)
	if e != nil {
		return nil, e
	}
	c.Set("response_status", 201)
	return gin.H{"id": id, "state": state, "preview": p}, nil
}
func (s *Server) importPreview(c *gin.Context, tx pgx.Tx) (any, error) {
	var b []byte
	e := tx.QueryRow(c.Request.Context(), `SELECT json_build_object('id',id,'state',state,'preview',preview) FROM source_batches WHERE workspace_id=$1 AND id=$2`, c.Param("wid"), c.Param("iid")).Scan(&b)
	return json.RawMessage(b), e
}
func (s *Server) commitImport(c *gin.Context, tx pgx.Tx) (any, error) {
	var in struct {
		ConfirmCorrections bool `json:"confirm_corrections"`
	}
	if e := bind(c, &in); e != nil {
		return nil, e
	}
	ctx := c.Request.Context()
	var aid, key, scope, zone, kind, granularity, state, hash string
	e := tx.QueryRow(ctx, `SELECT account_id,object_key,source_scope,source_timezone,cost_kind,granularity,state,content_hash FROM source_batches WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, c.Param("wid"), c.Param("iid")).Scan(&aid, &key, &scope, &zone, &kind, &granularity, &state, &hash)
	if e != nil {
		return nil, e
	}
	if state == "committed" {
		return gin.H{"status": "already_committed", "changed": 0}, nil
	}
	if state != "preview_ready" {
		return nil, bad("IMPORT_HAS_ERRORS")
	}
	var provider, status string
	e = tx.QueryRow(ctx, `SELECT provider,status FROM provider_accounts WHERE workspace_id=$1 AND id=$2 FOR UPDATE`, c.Param("wid"), aid).Scan(&provider, &status)
	if e != nil {
		return nil, e
	}
	if status == "disconnected" {
		return nil, bad("CONNECTION_DISCONNECTED")
	}
	data, e := s.Objects.Get(ctx, key)
	if e != nil {
		return nil, APIError{"STORAGE_UNAVAILABLE", 503}
	}
	if ledger.Hash(data) != hash {
		return nil, bad("SOURCE_HASH_MISMATCH")
	}
	p, e := ledger.ParseCSV(data, provider, scope, zone, kind, granularity)
	if e != nil || p.Rejected > 0 {
		return nil, bad("IMPORT_HAS_ERRORS")
	}
	// Distinct source scopes for one account may be mirrors. Never silently sum them.
	var overlap bool
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cost_entries WHERE workspace_id=$1 AND account_id=$2 AND cost_kind=$3 AND is_current AND period_start<$5 AND period_end>$4 AND source_scope<>$6)`, c.Param("wid"), aid, kind, p.Start, p.End, scope).Scan(&overlap)
	if e != nil {
		return nil, e
	}
	if overlap {
		return nil, APIError{"POSSIBLE_SOURCE_OVERLAP", 409}
	}
	if !in.ConfirmCorrections {
		var corrections bool
		keys := make([]string, len(p.Entries))
		for i, v := range p.Entries {
			keys[i] = v.Key
		}
		e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM cost_entries WHERE workspace_id=$1 AND account_id=$2 AND is_current AND source_record_key=ANY($3))`, c.Param("wid"), aid, keys).Scan(&corrections)
		if e != nil {
			return nil, e
		}
		if corrections {
			return nil, APIError{"CONFIRM_CORRECTIONS", 409}
		}
	}
	changed, e := ledger.Publish(ctx, tx, c.Param("wid"), aid, c.Param("iid"), p.Entries)
	if e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, `UPDATE source_batches SET state='committed',committed_at=now() WHERE workspace_id=$1 AND id=$2`, c.Param("wid"), c.Param("iid")); e != nil {
		return nil, e
	}
	if _, e = tx.Exec(ctx, `UPDATE provider_accounts SET last_sync_at=now(),data_through=greatest(data_through,$1) WHERE workspace_id=$2 AND id=$3`, p.End, c.Param("wid"), aid); e != nil {
		return nil, e
	}
	if _, e = s.Queue.InsertTx(ctx, tx, tasks.Args{Task: "insights", WorkspaceID: c.Param("wid"), ResourceID: c.Param("wid")}, nil); e != nil {
		return nil, e
	}
	return gin.H{"status": "committed", "changed": changed}, platform.Audit(ctx, tx, c.Param("wid"), session(c).UserID, "import.committed", c.Param("iid"), gin.H{"changed": changed, "hash": hash})
}
