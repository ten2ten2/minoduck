package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/ten2ten2/minoduck/services/backend/internal/dbgen"
	"github.com/ten2ten2/minoduck/services/backend/internal/platform"
	"github.com/ten2ten2/minoduck/services/backend/internal/subscriptions"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

type Server struct {
	Config     platform.Config
	DB         *pgxpool.Pool
	Queue      *river.Client[pgx.Tx]
	Objects    platform.Objects
	StripeHTTP *http.Client
}
type APIError struct {
	Code   string
	Status int
}

func (e APIError) Error() string  { return e.Code }
func bad(code string) error       { return APIError{code, 400} }
func forbidden(code string) error { return APIError{code, 403} }

type Session struct {
	UserID, Email, Locale, Theme, CSRF, Hash string
	LocaleExplicit                           bool
}

func session(c *gin.Context) Session { return c.MustGet("session").(Session) }
func (s *Server) Router() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.CustomRecovery(func(c *gin.Context, _ any) { s.fail(c, APIError{"INTERNAL_ERROR", 500}) }))
	r.Use(func(c *gin.Context) {
		c.Set("request_id", uuid.NewString())
		c.Header("X-Request-ID", c.GetString("request_id"))
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("Cache-Control", "private, no-store")
		if s.Config.Env == "production" {
			c.Header("Strict-Transport-Security", "max-age=31536000")
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 21*1024*1024)
		c.Next()
	})
	r.GET("/healthz", func(c *gin.Context) { c.JSON(200, gin.H{"status": "ok"}) })
	r.GET("/readyz", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if e := s.DB.Ping(ctx); e != nil {
			c.JSON(503, gin.H{"status": "unavailable"})
			return
		}
		c.JSON(200, gin.H{"status": "ready"})
	})
	r.POST("/api/v1/webhooks/stripe", s.stripeWebhook)
	api := r.Group("/api/v1")
	api.Use(s.serviceIdentity)
	api.POST("/auth/email/start", s.origin, s.emailStart)
	api.POST("/auth/email/verify", s.origin, s.emailVerify)
	api.GET("/auth/google/start", s.googleStart)
	api.GET("/auth/google/callback", s.googleCallback)
	api.GET("/providers", func(c *gin.Context) { c.JSON(200, providerCapabilities()) })
	api.Use(s.authenticate)
	api.GET("/me", s.me)
	api.PATCH("/me/preferences", s.preferences)
	api.POST("/auth/logout", s.logout)
	api.GET("/workspaces", s.listWorkspaces)
	api.POST("/workspaces", s.createWorkspace)
	api.POST("/invitations/accept", s.acceptInvitation)
	w := api.Group("/workspaces/:wid")
	w.GET("", s.tenant("viewer", s.workspace))
	w.PATCH("", s.tenant("admin", s.updateWorkspace))
	w.GET("/members", s.tenant("viewer", s.members))
	w.POST("/invitations", s.tenant("admin", s.invite))
	w.DELETE("/members/:memberId", s.tenant("admin", s.removeMember))
	w.POST("/deletion-requests", s.tenant("owner", s.deleteWorkspace))
	w.GET("/connections", s.tenant("viewer", s.connections))
	w.POST("/connections", s.tenant("admin", s.createConnection))
	w.GET("/connections/:cid", s.tenant("viewer", s.connection))
	w.POST("/connections/:cid/sync", s.tenant("admin", s.syncConnection))
	w.POST("/connections/:cid/credentials", s.tenant("admin", s.replaceCredential))
	w.DELETE("/connections/:cid", s.tenant("admin", s.disconnect))
	w.GET("/sync-runs/:rid", s.tenant("viewer", s.syncRun))
	w.POST("/imports", s.tenant("admin", s.upload))
	w.GET("/imports/:iid/preview", s.tenant("viewer", s.importPreview))
	w.POST("/imports/:iid/commit", s.tenant("admin", s.commitImport))
	w.GET("/overview", s.tenant("viewer", s.overview))
	w.GET("/costs", s.tenant("viewer", s.costs))
	w.GET("/costs/:eid", s.tenant("viewer", s.costDetail))
	w.GET("/invoices", s.tenant("viewer", s.invoices))
	w.POST("/invoices", s.tenant("admin", s.createInvoice))
	w.GET("/invoices/:iid", s.tenant("viewer", s.invoice))
	w.GET("/reconciliation-runs", s.tenant("viewer", s.reconciliations))
	w.POST("/reconciliation-runs", s.tenant("admin", s.reconcile))
	w.GET("/reconciliation-runs/:rid", s.tenant("viewer", s.reconciliation))
	w.PATCH("/reconciliation-items/:rid", s.tenant("admin", s.handleReconciliation))
	w.GET("/insights", s.tenant("viewer", s.insights))
	w.GET("/prices", s.tenant("viewer", s.prices))
	w.POST("/prices", s.tenant("admin", s.createPrice))
	w.POST("/price-comparisons", s.tenant("admin", s.comparePrices))
	w.POST("/usage-reconciliation-runs", s.tenant("admin", s.reconcileUsage))
	w.PATCH("/insights/:iid", s.tenant("admin", s.updateInsight))
	w.GET("/alert-rules", s.tenant("viewer", s.alerts))
	w.POST("/alert-rules", s.tenant("admin", s.createAlert))
	w.PATCH("/alert-rules/:aid", s.tenant("admin", s.updateAlert))
	w.DELETE("/alert-rules/:aid", s.tenant("admin", s.deleteAlert))
	w.POST("/exports", s.tenant("viewer", s.createExport))
	w.GET("/exports", s.tenant("viewer", s.exports))
	w.GET("/exports/:eid", s.tenant("viewer", s.exportStatus))
	w.GET("/exports/:eid/download", s.downloadExport)
	w.GET("/subscription", s.tenant("viewer", s.subscription))
	w.POST("/subscription/checkout", s.tenant("owner", s.checkout))
	w.POST("/subscription/portal", s.tenant("owner", s.portal))
	w.POST("/subscription/change", s.tenant("owner", s.changeSubscription))
	w.POST("/subscription/cancel", s.tenant("owner", s.cancelSubscription))
	w.POST("/subscription/resume", s.tenant("owner", s.resumeSubscription))
	return r
}
func (s *Server) serviceIdentity(c *gin.Context) {
	if subtle.ConstantTimeCompare([]byte(c.GetHeader("X-MinoDuck-Service")), []byte(s.Config.BFFToken)) != 1 {
		s.fail(c, APIError{"UNAUTHORIZED", 401})
		c.Abort()
		return
	}
	c.Next()
}
func (s *Server) origin(c *gin.Context) {
	if c.GetHeader("Origin") != s.Config.AppURL {
		s.fail(c, forbidden("INVALID_ORIGIN"))
		c.Abort()
		return
	}
	c.Next()
}
func (s *Server) cookieName() string {
	if s.Config.Env == "production" {
		return "__Host-md_session"
	}
	return "md_session"
}
func (s *Server) authenticate(c *gin.Context) {
	token, e := c.Cookie(s.cookieName())
	if e != nil {
		s.fail(c, APIError{"UNAUTHORIZED", 401})
		c.Abort()
		return
	}
	a := Session{Hash: platform.TokenHash(token)}
	record, e := dbgen.New(s.DB).SessionUser(c.Request.Context(), a.Hash)
	a.UserID, a.Email, a.Locale, a.Theme, a.CSRF, a.LocaleExplicit = record.ID, record.Email, record.Locale, record.Theme, record.CsrfToken, record.LocaleExplicit
	if e != nil {
		s.fail(c, APIError{"UNAUTHORIZED", 401})
		c.Abort()
		return
	}
	if c.Request.Method != "GET" && c.Request.Method != "HEAD" {
		if c.GetHeader("Origin") != s.Config.AppURL || subtle.ConstantTimeCompare([]byte(c.GetHeader("X-CSRF-Token")), []byte(a.CSRF)) != 1 {
			s.fail(c, forbidden("INVALID_CSRF"))
			c.Abort()
			return
		}
	}
	c.Set("session", a)
	c.Next()
}
func (s *Server) fail(c *gin.Context, err error) {
	e := APIError{"INTERNAL_ERROR", 500}
	var known APIError
	if errors.As(err, &known) {
		e = known
	} else if errors.Is(err, pgx.ErrNoRows) {
		e = APIError{"NOT_FOUND", 404}
	}
	if e.Status == 500 {
		slog.Error("request failed", "request_id", c.GetString("request_id"), "route", c.FullPath(), "error_type", strings.SplitN(err.Error(), ":", 2)[0])
	}
	c.JSON(e.Status, gin.H{"error": gin.H{"code": e.Code, "message_key": "errors." + e.Code, "retryable": e.Status >= 500, "request_id": c.GetString("request_id")}})
}

type tenantHandler func(*gin.Context, pgx.Tx) (any, error)

func (s *Server) tenant(role string, h tenantHandler) gin.HandlerFunc {
	return func(c *gin.Context) {
		wid := c.Param("wid")
		if _, e := uuid.Parse(wid); e != nil {
			s.fail(c, bad("INVALID_WORKSPACE"))
			return
		}
		for _, p := range c.Params {
			if p.Key != "wid" {
				if _, e := uuid.Parse(p.Value); e != nil {
					s.fail(c, bad("INVALID_ID"))
					return
				}
			}
		}
		tx, e := platform.TenantTx(c.Request.Context(), s.DB, wid)
		if e != nil {
			s.fail(c, e)
			return
		}
		defer tx.Rollback(c.Request.Context())
		membership, e := dbgen.New(tx).TenantMembership(c.Request.Context(), dbgen.TenantMembershipParams{WorkspaceID: wid, UserID: session(c).UserID})
		actual, account := membership.Role, membership.BillingAccountID
		if e != nil {
			s.fail(c, APIError{"NOT_FOUND", 404})
			return
		}
		ranks := map[string]int{"viewer": 1, "admin": 2, "owner": 3}
		if ranks[actual] < ranks[role] {
			s.fail(c, forbidden("INSUFFICIENT_ROLE"))
			return
		}
		c.Set("role", actual)
		c.Set("billing_account_id", account)
		result, e := h(c, tx)
		if e != nil {
			s.fail(c, e)
			return
		}
		if e = tx.Commit(c.Request.Context()); e != nil {
			s.fail(c, e)
			return
		}
		status := c.GetInt("response_status")
		if status == 0 {
			status = 200
		}
		c.JSON(status, result)
	}
}
func (s *Server) plan(c *gin.Context, tx pgx.Tx) (subscriptions.Plan, error) {
	var code, status string
	var grace *time.Time
	e := tx.QueryRow(c.Request.Context(), `SELECT plan_code,status,grace_period_until FROM subscriptions WHERE billing_account_id=$1`, c.GetString("billing_account_id")).Scan(&code, &status, &grace)
	if e != nil {
		return subscriptions.Plans["free"], e
	}
	return subscriptions.Effective(code, status, grace, time.Now()), nil
}
func bind(c *gin.Context, v any) error {
	d := json.NewDecoder(c.Request.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(v); e != nil {
		return bad("INVALID_REQUEST")
	}
	if e := d.Decode(&struct{}{}); e != io.EOF {
		return bad("INVALID_REQUEST")
	}
	return nil
}
func (s *Server) paid(c *gin.Context, tx pgx.Tx) error {
	p, e := s.plan(c, tx)
	if e != nil {
		return e
	}
	if !p.Details {
		return APIError{"UPGRADE_REQUIRED", 402}
	}
	return nil
}
