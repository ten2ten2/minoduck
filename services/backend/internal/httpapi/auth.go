package httpapi

import (
	"context"
	"crypto/subtle"
	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5"
	"github.com/ten2ten2/minoduck/services/backend/internal/platform"
	"golang.org/x/oauth2"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
)

func (s *Server) setCookie(c *gin.Context, name, value string, age int) {
	http.SetCookie(c.Writer, &http.Cookie{Name: name, Value: value, Path: "/", Secure: s.Config.Env == "production", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: age})
}
func (s *Server) flowCookie() string {
	if s.Config.Env == "production" {
		return "__Host-md_login"
	}
	return "md_login"
}
func (s *Server) emailStart(c *gin.Context) {
	var in struct {
		Email  string `json:"email"`
		Locale string `json:"locale"`
	}
	if e := bind(c, &in); e != nil {
		s.fail(c, e)
		return
	}
	address, e := mail.ParseAddress(strings.TrimSpace(in.Email))
	if e != nil || address.Address != strings.TrimSpace(in.Email) || len(in.Email) > 254 {
		s.fail(c, bad("INVALID_EMAIL"))
		return
	}
	email := strings.ToLower(address.Address)
	ctx := c.Request.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		s.fail(c, e)
		return
	}
	defer tx.Rollback(ctx)
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, email); e != nil {
		s.fail(c, e)
		return
	}
	var n int
	e = tx.QueryRow(ctx, `SELECT count(*) FROM login_tokens WHERE email=$1 AND created_at>now()-interval '1 minute'`, email).Scan(&n)
	if e != nil {
		s.fail(c, e)
		return
	}
	if n > 0 {
		s.fail(c, APIError{"RATE_LIMITED", 429})
		return
	}
	token, browser := platform.RandomToken(), platform.RandomToken()
	_, e = tx.Exec(ctx, `INSERT INTO login_tokens(token_hash,email,browser_hash,expires_at) VALUES($1,$2,$3,now()+interval '15 minutes')`, platform.TokenHash(token), email, platform.TokenHash(browser))
	if e != nil {
		s.fail(c, e)
		return
	}
	if e = tx.Commit(ctx); e != nil {
		s.fail(c, e)
		return
	}
	s.setCookie(c, s.flowCookie(), browser, 900)
	link := s.Config.AppURL + "/login?token=" + url.QueryEscape(token)
	subject, body := "Sign in to MinoDuck", "Open this link in the browser where you requested it. It expires in 15 minutes:\n\n"+link
	if in.Locale == "zh-hans" {
		subject = "登录 MinoDuck"
		body = "请在发起登录的浏览器中打开以下链接，15 分钟内有效：\n\n" + link
	}
	if in.Locale == "zh-hant" {
		subject = "登入 MinoDuck"
		body = "請在發起登入的瀏覽器中開啟以下連結，15 分鐘內有效：\n\n" + link
	}
	if s.Config.Env != "production" && s.Config.ResendKey == "" {
		c.JSON(202, gin.H{"status": "email_sent", "development_link": link})
		return
	}
	if e = platform.SendMail(ctx, s.Config, email, subject, body, "login-"+platform.TokenHash(token)); e != nil {
		_, _ = s.DB.Exec(ctx, `DELETE FROM login_tokens WHERE token_hash=$1`, platform.TokenHash(token))
		s.fail(c, APIError{"EMAIL_DELIVERY_FAILED", 503})
		return
	}
	c.JSON(202, gin.H{"status": "email_sent"})
}
func (s *Server) emailVerify(c *gin.Context) {
	var in struct {
		Token string `json:"token"`
	}
	if e := bind(c, &in); e != nil {
		s.fail(c, e)
		return
	}
	browser, e := c.Cookie(s.flowCookie())
	if e != nil || len(in.Token) > 128 {
		s.fail(c, bad("INVALID_LOGIN_LINK"))
		return
	}
	ctx := c.Request.Context()
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		s.fail(c, e)
		return
	}
	defer tx.Rollback(ctx)
	var email string
	e = tx.QueryRow(ctx, `DELETE FROM login_tokens WHERE token_hash=$1 AND browser_hash=$2 AND expires_at>now() RETURNING email`, platform.TokenHash(in.Token), platform.TokenHash(browser)).Scan(&email)
	if e != nil {
		s.fail(c, bad("INVALID_LOGIN_LINK"))
		return
	}
	token, e := s.newSession(c, tx, email)
	if e != nil {
		s.fail(c, e)
		return
	}
	if e = tx.Commit(ctx); e != nil {
		s.fail(c, e)
		return
	}
	s.setCookie(c, s.cookieName(), token, 30*86400)
	s.setCookie(c, s.flowCookie(), "", -1)
	c.JSON(200, gin.H{"status": "authenticated"})
}
func (s *Server) newSession(c *gin.Context, tx pgx.Tx, email string) (string, error) {
	ctx := c.Request.Context()
	var uid string
	e := tx.QueryRow(ctx, `INSERT INTO users(email) VALUES($1) ON CONFLICT(email) DO UPDATE SET email=excluded.email RETURNING id`, email).Scan(&uid)
	if e != nil {
		return "", e
	}
	if old, e := c.Cookie(s.cookieName()); e == nil {
		if _, e = tx.Exec(ctx, `DELETE FROM sessions WHERE token_hash=$1`, platform.TokenHash(old)); e != nil {
			return "", e
		}
	}
	token := platform.RandomToken()
	_, e = tx.Exec(ctx, `INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at) VALUES($1,$2,$3,now()+interval '30 days')`, platform.TokenHash(token), uid, platform.RandomToken())
	return token, e
}
func (s *Server) googleStart(c *gin.Context) {
	if s.Config.GoogleID == "" || s.Config.GoogleSecret == "" {
		s.fail(c, APIError{"GOOGLE_NOT_CONFIGURED", 503})
		return
	}
	state, browser, nonce, verifier := platform.RandomToken(), platform.RandomToken(), platform.RandomToken(), oauth2.GenerateVerifier()
	_, e := s.DB.Exec(c.Request.Context(), `INSERT INTO oauth_states(state_hash,browser_hash,verifier,nonce,expires_at) VALUES($1,$2,$3,$4,now()+interval '10 minutes')`, platform.TokenHash(state), platform.TokenHash(browser), verifier, nonce)
	if e != nil {
		s.fail(c, e)
		return
	}
	s.setCookie(c, s.flowCookie(), browser, 600)
	conf := s.googleConfig()
	c.Redirect(302, conf.AuthCodeURL(state, oauth2.S256ChallengeOption(verifier), oidc.Nonce(nonce)))
}
func (s *Server) googleConfig() oauth2.Config {
	return oauth2.Config{ClientID: s.Config.GoogleID, ClientSecret: s.Config.GoogleSecret, RedirectURL: s.Config.AppURL + "/api/v1/auth/google/callback", Scopes: []string{oidc.ScopeOpenID, "email"}, Endpoint: oauth2.Endpoint{AuthURL: "https://accounts.google.com/o/oauth2/v2/auth", TokenURL: "https://oauth2.googleapis.com/token"}}
}
func googleHTTPClient() *http.Client {
	return &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}
func googleContext(parent context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	return oidc.ClientContext(ctx, googleHTTPClient()), cancel
}
func (s *Server) googleCallback(c *gin.Context) {
	ctx := c.Request.Context()
	browser, _ := c.Cookie(s.flowCookie())
	if browser == "" {
		s.fail(c, bad("INVALID_OAUTH_STATE"))
		return
	}
	var verifier, nonce string
	e := s.DB.QueryRow(ctx, `DELETE FROM oauth_states WHERE state_hash=$1 AND browser_hash=$2 AND expires_at>now() RETURNING verifier,nonce`, platform.TokenHash(c.Query("state")), platform.TokenHash(browser)).Scan(&verifier, &nonce)
	if e != nil {
		s.fail(c, bad("INVALID_OAUTH_STATE"))
		return
	}
	outbound, cancel := googleContext(ctx)
	defer cancel()
	conf := s.googleConfig()
	token, e := conf.Exchange(outbound, c.Query("code"), oauth2.VerifierOption(verifier))
	if e != nil {
		s.fail(c, bad("OAUTH_FAILED"))
		return
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		s.fail(c, bad("OAUTH_FAILED"))
		return
	}
	provider, e := oidc.NewProvider(outbound, "https://accounts.google.com")
	if e != nil {
		s.fail(c, APIError{"OAUTH_UNAVAILABLE", 503})
		return
	}
	id, e := provider.Verifier(&oidc.Config{ClientID: s.Config.GoogleID}).Verify(outbound, raw)
	if e != nil || id == nil {
		s.fail(c, bad("OAUTH_FAILED"))
		return
	}
	var claims struct {
		Email    string `json:"email"`
		Verified bool   `json:"email_verified"`
		Nonce    string `json:"nonce"`
	}
	if e = id.Claims(&claims); e != nil || !claims.Verified || subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(nonce)) != 1 {
		s.fail(c, bad("OAUTH_FAILED"))
		return
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		s.fail(c, e)
		return
	}
	defer tx.Rollback(ctx)
	sessionToken, e := s.newSession(c, tx, strings.ToLower(claims.Email))
	if e != nil {
		s.fail(c, e)
		return
	}
	if e = tx.Commit(ctx); e != nil {
		s.fail(c, e)
		return
	}
	s.setCookie(c, s.cookieName(), sessionToken, 30*86400)
	s.setCookie(c, s.flowCookie(), "", -1)
	c.Redirect(303, s.Config.AppURL+"/onboarding")
}
func (s *Server) me(c *gin.Context) {
	a := session(c)
	c.JSON(200, gin.H{"id": a.UserID, "email": a.Email, "locale": a.Locale, "theme": a.Theme, "csrf_token": a.CSRF, "locale_explicit": a.LocaleExplicit})
}
func (s *Server) preferences(c *gin.Context) {
	var in struct {
		Locale string `json:"locale"`
		Theme  string `json:"theme"`
	}
	if e := bind(c, &in); e != nil {
		s.fail(c, e)
		return
	}
	if in.Locale != "en" && in.Locale != "zh-hans" && in.Locale != "zh-hant" {
		s.fail(c, bad("INVALID_LOCALE"))
		return
	}
	if in.Theme != "light" && in.Theme != "dark" && in.Theme != "system" {
		s.fail(c, bad("INVALID_THEME"))
		return
	}
	_, e := s.DB.Exec(c.Request.Context(), `UPDATE users SET locale=$1,theme=$2,locale_explicit=true WHERE id=$3`, in.Locale, in.Theme, session(c).UserID)
	if e != nil {
		s.fail(c, e)
		return
	}
	c.JSON(200, gin.H{"status": "saved"})
}
func (s *Server) logout(c *gin.Context) {
	_, e := s.DB.Exec(c.Request.Context(), `DELETE FROM sessions WHERE token_hash=$1`, session(c).Hash)
	if e != nil {
		s.fail(c, e)
		return
	}
	s.setCookie(c, s.cookieName(), "", -1)
	c.JSON(200, gin.H{"status": "signed_out"})
}
