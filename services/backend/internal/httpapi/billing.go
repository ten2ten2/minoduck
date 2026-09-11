package httpapi

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
	stripe "github.com/stripe/stripe-go/v86"
	"github.com/ten2ten2/minoduck/services/backend/internal/platform"
	"github.com/ten2ten2/minoduck/services/backend/internal/subscriptions"
	"io"
	"strconv"
	"strings"
	"time"
)

func (s *Server) stripe() *stripe.Client {
	return subscriptions.NewStripe(s.Config.StripeKey, s.StripeHTTP)
}
func (s *Server) billingOwner(c *gin.Context, tx pgx.Tx) error {
	var id string
	e := tx.QueryRow(c.Request.Context(), `SELECT owner_id FROM billing_accounts WHERE id=$1 FOR UPDATE`, c.GetString("billing_account_id")).Scan(&id)
	if e != nil {
		return e
	}
	if id != session(c).UserID {
		return forbidden("OWNER_REQUIRED")
	}
	return nil
}
func (s *Server) billingURL(c *gin.Context, tx pgx.Tx) string {
	var slug string
	if e := tx.QueryRow(c.Request.Context(), `SELECT slug FROM workspaces WHERE id=$1`, c.Param("wid")).Scan(&slug); e != nil {
		return s.Config.AppURL + "/onboarding"
	}
	return s.Config.AppURL + "/w/" + slug + "/settings/billing"
}
func (s *Server) subscription(c *gin.Context, tx pgx.Tx) (any, error) {
	p, e := s.plan(c, tx)
	if e != nil {
		return nil, e
	}
	var b []byte
	e = tx.QueryRow(c.Request.Context(), `SELECT json_build_object('has_subscription',provider_subscription_id IS NOT NULL,'plan_code',plan_code,'billing_interval',billing_interval,'status',status,'current_period_end',current_period_end,'cancel_at_period_end',cancel_at_period_end,'grace_period_until',grace_period_until,'scheduled_plan',scheduled_plan,'scheduled_interval',scheduled_interval) FROM subscriptions WHERE billing_account_id=$1`, c.GetString("billing_account_id")).Scan(&b)
	if e != nil {
		return nil, e
	}
	// Team's spend allowance covers the billing account, not each workspace separately.
	rows, e := tx.Query(c.Request.Context(), `SELECT w.id FROM workspaces w JOIN workspace_members m ON m.workspace_id=w.id WHERE w.billing_account_id=$1 AND w.deletion_requested_at IS NULL AND m.user_id=$2`, c.GetString("billing_account_id"), session(c).UserID)
	if e != nil {
		return nil, e
	}
	ids := []string{}
	for rows.Next() {
		var id string
		if e = rows.Scan(&id); e != nil {
			rows.Close()
			return nil, e
		}
		ids = append(ids, id)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nil, e
	}
	now := time.Now().UTC()
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	amount := decimal.Zero
	currencies := map[string]string{}
	for _, wid := range ids {
		if _, e = tx.Exec(c.Request.Context(), `SELECT set_config('app.workspace_id',$1,true)`, wid); e != nil {
			return nil, e
		}
		rs, e := tx.Query(c.Request.Context(), `SELECT currency,sum(amount)::text FROM cost_entries WHERE workspace_id=$1 AND is_current AND cost_kind='actual' AND period_start>=$2 AND period_end<=$3 GROUP BY currency`, wid, month, month.AddDate(0, 1, 0))
		if e != nil {
			return nil, e
		}
		for rs.Next() {
			var cur, a string
			if e = rs.Scan(&cur, &a); e != nil {
				rs.Close()
				return nil, e
			}
			v, _ := decimal.NewFromString(a)
			old, _ := decimal.NewFromString(currencies[cur])
			currencies[cur] = old.Add(v).String()
			if cur == "USD" {
				amount = amount.Add(v)
			}
		}
		e = rs.Err()
		rs.Close()
		if e != nil {
			return nil, e
		}
	}
	if _, e = tx.Exec(c.Request.Context(), `SELECT set_config('app.workspace_id',$1,true)`, c.Param("wid")); e != nil {
		return nil, e
	}
	limit, _ := decimal.NewFromString(p.SpendLimit)
	level := "normal"
	if amount.GreaterThanOrEqual(limit) {
		level = "exceeded"
	} else if amount.GreaterThanOrEqual(limit.Mul(decimal.NewFromInt(8)).Div(decimal.NewFromInt(10))) {
		level = "approaching"
	}
	return gin.H{"subscription": json.RawMessage(b), "entitlements": p, "managed_spend": currencies, "spend_level": level, "non_usd_requires_review": len(currencies) > 1 || (len(currencies) == 1 && currencies["USD"] == ""), "soft_limit": true, "stripe_configured": s.Config.StripeKey != ""}, nil
}

type planRequest struct {
	Plan     string `json:"plan"`
	Interval string `json:"billing_interval"`
}

func (s *Server) planInput(c *gin.Context) (planRequest, string, error) {
	var in planRequest
	if e := bind(c, &in); e != nil {
		return in, "", e
	}
	if (in.Plan != "starter" && in.Plan != "team") || (in.Interval != "month" && in.Interval != "year") {
		return in, "", bad("INVALID_PLAN")
	}
	price := s.Config.Prices[in.Plan+":"+in.Interval]
	if price == "" || s.Config.StripeKey == "" {
		return in, "", APIError{"STRIPE_NOT_CONFIGURED", 503}
	}
	return in, price, nil
}
func (s *Server) checkout(c *gin.Context, tx pgx.Tx) (any, error) {
	in, price, e := s.planInput(c)
	if e != nil {
		return nil, e
	}
	if e = s.billingOwner(c, tx); e != nil {
		return nil, e
	}
	ctx := c.Request.Context()
	account := c.GetString("billing_account_id")
	var customer, sub *string
	e = tx.QueryRow(ctx, `SELECT provider_customer_id,provider_subscription_id FROM subscriptions WHERE billing_account_id=$1 FOR UPDATE`, account).Scan(&customer, &sub)
	if e != nil {
		return nil, e
	}
	if sub != nil {
		return nil, APIError{"SUBSCRIPTION_ALREADY_EXISTS", 409}
	}
	client := s.stripe()
	if customer == nil {
		out, e := client.V1Customers.Create(ctx, &stripe.CustomerCreateParams{
			Params: stripe.Params{IdempotencyKey: stripe.String("customer-" + account)},
			Email:  stripe.String(session(c).Email), Metadata: map[string]string{"billing_account_id": account},
		})
		if e != nil {
			return nil, APIError{"STRIPE_UNAVAILABLE", 503}
		}
		customer = &out.ID
		if _, e = tx.Exec(ctx, `UPDATE subscriptions SET provider_customer_id=$1 WHERE billing_account_id=$2`, out.ID, account); e != nil {
			return nil, e
		}
	}
	var key, oldPlan, oldInterval string
	var oldURL *string
	var expiry time.Time
	e = tx.QueryRow(ctx, `SELECT request_key,plan_code,billing_interval,url,expires_at FROM checkout_requests WHERE billing_account_id=$1`, account).Scan(&key, &oldPlan, &oldInterval, &oldURL, &expiry)
	if e != nil && e != pgx.ErrNoRows {
		return nil, e
	}
	if e == nil && expiry.After(time.Now()) {
		if oldPlan != in.Plan || oldInterval != in.Interval {
			return nil, APIError{"CHECKOUT_ALREADY_PENDING", 409}
		}
		if oldURL != nil {
			return gin.H{"url": *oldURL}, nil
		}
	}
	// Stable across database rollback and retries within the same 30-minute window.
	key = account + "-" + in.Plan + "-" + in.Interval + "-" + strconv.FormatInt(time.Now().Unix()/1800, 10)
	returnURL := s.billingURL(c, tx)
	out, e := client.V1CheckoutSessions.Create(ctx, &stripe.CheckoutSessionCreateParams{
		Params: stripe.Params{IdempotencyKey: stripe.String(key)},
		Mode:   stripe.String("subscription"), Customer: customer,
		LineItems:  []*stripe.CheckoutSessionCreateLineItemParams{{Price: stripe.String(price), Quantity: stripe.Int64(1)}},
		SuccessURL: stripe.String(returnURL + "?checkout=success"), CancelURL: stripe.String(returnURL + "?checkout=canceled"),
		SubscriptionData: &stripe.CheckoutSessionCreateSubscriptionDataParams{
			Metadata:    map[string]string{"billing_account_id": account},
			BillingMode: &stripe.CheckoutSessionCreateSubscriptionDataBillingModeParams{Type: stripe.String("flexible")},
		},
		ClientReferenceID: stripe.String(account), ExpiresAt: stripe.Int64((time.Now().Unix()/1800 + 2) * 1800),
	})
	if e != nil {
		return nil, APIError{"STRIPE_UNAVAILABLE", 503}
	}
	_, e = tx.Exec(ctx, `INSERT INTO checkout_requests(billing_account_id,request_key,plan_code,billing_interval,session_id,url,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(billing_account_id) DO UPDATE SET request_key=excluded.request_key,plan_code=excluded.plan_code,billing_interval=excluded.billing_interval,session_id=excluded.session_id,url=excluded.url,expires_at=excluded.expires_at`, account, key, in.Plan, in.Interval, out.ID, out.URL, time.Unix(out.ExpiresAt, 0))
	return gin.H{"url": out.URL}, e
}
func (s *Server) portal(c *gin.Context, tx pgx.Tx) (any, error) {
	if e := s.billingOwner(c, tx); e != nil {
		return nil, e
	}
	var customer *string
	e := tx.QueryRow(c.Request.Context(), `SELECT provider_customer_id FROM subscriptions WHERE billing_account_id=$1`, c.GetString("billing_account_id")).Scan(&customer)
	if e != nil {
		return nil, e
	}
	if customer == nil {
		return nil, bad("NO_BILLING_CUSTOMER")
	}
	if s.Config.StripePortalConfig == "" {
		return nil, APIError{"STRIPE_PORTAL_NOT_CONFIGURED", 503}
	}
	out, e := s.stripe().V1BillingPortalSessions.Create(c.Request.Context(), &stripe.BillingPortalSessionCreateParams{
		Params:   stripe.Params{IdempotencyKey: stripe.String(uuid.NewString())},
		Customer: customer, ReturnURL: stripe.String(s.billingURL(c, tx)), Configuration: stripe.String(s.Config.StripePortalConfig),
	})
	if e != nil {
		return nil, APIError{"STRIPE_UNAVAILABLE", 503}
	}
	return gin.H{"url": out.URL}, nil
}
func (s *Server) changeSubscription(c *gin.Context, tx pgx.Tx) (any, error) {
	in, price, e := s.planInput(c)
	if e != nil {
		return nil, e
	}
	if e = s.billingOwner(c, tx); e != nil {
		return nil, e
	}
	var sid *string
	e = tx.QueryRow(c.Request.Context(), `SELECT provider_subscription_id FROM subscriptions WHERE billing_account_id=$1`, c.GetString("billing_account_id")).Scan(&sid)
	if e != nil {
		return nil, e
	}
	if sid == nil {
		return nil, bad("NO_SUBSCRIPTION")
	}
	client := s.stripe()
	live, e := client.V1Subscriptions.Retrieve(c.Request.Context(), *sid, nil)
	if e != nil {
		return nil, APIError{"STRIPE_UNAVAILABLE", 503}
	}
	item := subscriptionItem(live)
	if live.Status != stripe.SubscriptionStatusActive || item == nil {
		return nil, bad("SUBSCRIPTION_NOT_ACTIVE")
	}
	oldPlan, oldInterval := s.mapPrice(item.Price.ID)
	if oldPlan == "" {
		return nil, bad("UNRECOGNIZED_PRICE")
	}
	mode := subscriptionChangeMode(oldPlan, oldInterval, in.Plan, in.Interval, live.Schedule != nil)
	if mode == "unchanged" {
		return gin.H{"status": "unchanged"}, nil
	}
	if mode == "cancel_scheduled" {
		if e = s.releaseScheduledChange(c, tx, client, live.Schedule.ID); e != nil {
			return nil, e
		}
		return gin.H{"status": "schedule_canceled"}, nil
	}
	key := "change-" + *sid + "-" + in.Plan + "-" + in.Interval + "-" + strconv.FormatInt(item.CurrentPeriodStart, 10)
	if mode == "deferred" {
		scheduleID := ""
		if live.Schedule != nil {
			scheduleID = live.Schedule.ID
		} else {
			schedule, e := client.V1SubscriptionSchedules.Create(c.Request.Context(), &stripe.SubscriptionScheduleCreateParams{
				Params: stripe.Params{IdempotencyKey: stripe.String(key + "-create")}, FromSubscription: sid,
			})
			if e != nil {
				return nil, APIError{"STRIPE_UNAVAILABLE", 503}
			}
			scheduleID = schedule.ID
		}
		if _, e = client.V1SubscriptionSchedules.Update(c.Request.Context(), scheduleID, scheduleParams(item, price, in.Interval, key+"-phases")); e != nil {
			return nil, APIError{"STRIPE_UNAVAILABLE", 503}
		}
		_, e = tx.Exec(c.Request.Context(), `UPDATE subscriptions SET scheduled_plan=$1,scheduled_interval=$2,provider_schedule_id=$3 WHERE billing_account_id=$4`, in.Plan, in.Interval, scheduleID, c.GetString("billing_account_id"))
		return gin.H{"status": "scheduled", "effective_at": time.Unix(item.CurrentPeriodEnd, 0)}, e
	}
	if mode == "release_then_immediate" {
		if e = s.releaseScheduledChange(c, tx, client, live.Schedule.ID); e != nil {
			return nil, e
		}
	}
	params := &stripe.SubscriptionUpdateParams{
		Params:            stripe.Params{IdempotencyKey: stripe.String(key)},
		Items:             []*stripe.SubscriptionUpdateItemParams{{ID: stripe.String(item.ID), Price: stripe.String(price)}},
		ProrationBehavior: stripe.String("always_invoice"), PaymentBehavior: stripe.String("pending_if_incomplete"),
	}
	if oldInterval != in.Interval {
		params.BillingCycleAnchorNow = stripe.Bool(true)
	}
	if _, e = client.V1Subscriptions.Update(c.Request.Context(), *sid, params); e != nil {
		return nil, APIError{"STRIPE_UNAVAILABLE", 503}
	}
	return gin.H{"status": "awaiting_webhook"}, nil
}
func (s *Server) releaseScheduledChange(c *gin.Context, tx pgx.Tx, client *stripe.Client, scheduleID string) error {
	if _, e := client.V1SubscriptionSchedules.Release(c.Request.Context(), scheduleID, &stripe.SubscriptionScheduleReleaseParams{
		Params: stripe.Params{IdempotencyKey: stripe.String("release-" + scheduleID)},
	}); e != nil {
		return APIError{"STRIPE_UNAVAILABLE", 503}
	}
	_, e := tx.Exec(c.Request.Context(), `UPDATE subscriptions SET scheduled_plan=NULL,scheduled_interval=NULL,provider_schedule_id=NULL WHERE billing_account_id=$1`, c.GetString("billing_account_id"))
	return e
}
func (s *Server) cancelSubscription(c *gin.Context, tx pgx.Tx) (any, error) {
	if e := s.billingOwner(c, tx); e != nil {
		return nil, e
	}
	var id *string
	e := tx.QueryRow(c.Request.Context(), `SELECT provider_subscription_id FROM subscriptions WHERE billing_account_id=$1`, c.GetString("billing_account_id")).Scan(&id)
	if e != nil {
		return nil, e
	}
	if id == nil {
		return nil, bad("NO_SUBSCRIPTION")
	}
	client := s.stripe()
	live, e := client.V1Subscriptions.Retrieve(c.Request.Context(), *id, nil)
	if e != nil {
		return nil, APIError{"STRIPE_UNAVAILABLE", 503}
	}
	if live.Schedule != nil {
		if _, e = client.V1SubscriptionSchedules.Release(c.Request.Context(), live.Schedule.ID, &stripe.SubscriptionScheduleReleaseParams{
			Params: stripe.Params{IdempotencyKey: stripe.String("release-" + live.Schedule.ID)},
		}); e != nil {
			return nil, APIError{"STRIPE_UNAVAILABLE", 503}
		}
	}
	_, e = client.V1Subscriptions.Update(c.Request.Context(), *id, &stripe.SubscriptionUpdateParams{
		Params: stripe.Params{IdempotencyKey: stripe.String("cancel-" + *id)}, CancelAtPeriodEnd: stripe.Bool(true),
	})
	if e != nil {
		return nil, APIError{"STRIPE_UNAVAILABLE", 503}
	}
	return gin.H{"status": "awaiting_webhook"}, nil
}
func (s *Server) mapPrice(id string) (string, string) {
	for key, v := range s.Config.Prices {
		if v != "" && v == id {
			parts := strings.Split(key, ":")
			return parts[0], parts[1]
		}
	}
	return "", ""
}

func subscriptionChangeMode(fromPlan, fromInterval, toPlan, toInterval string, hasSchedule bool) string {
	if fromPlan == toPlan && fromInterval == toInterval {
		if hasSchedule {
			return "cancel_scheduled"
		}
		return "unchanged"
	}
	if subscriptions.DeferredChange(fromPlan, toPlan, fromInterval, toInterval) {
		return "deferred"
	}
	if hasSchedule {
		return "release_then_immediate"
	}
	return "immediate"
}

func subscriptionItem(live *stripe.Subscription) *stripe.SubscriptionItem {
	if live.Items == nil || len(live.Items.Data) != 1 {
		return nil
	}
	item := live.Items.Data[0]
	if item == nil || item.Price == nil || item.Price.Recurring == nil {
		return nil
	}
	return item
}

func scheduleParams(current *stripe.SubscriptionItem, price, interval, key string) *stripe.SubscriptionScheduleUpdateParams {
	return &stripe.SubscriptionScheduleUpdateParams{
		Params:      stripe.Params{IdempotencyKey: stripe.String(key)},
		EndBehavior: stripe.String("release"), ProrationBehavior: stripe.String("none"),
		Phases: []*stripe.SubscriptionScheduleUpdatePhaseParams{
			{
				StartDate: stripe.Int64(current.CurrentPeriodStart), EndDate: stripe.Int64(current.CurrentPeriodEnd),
				Items:             []*stripe.SubscriptionScheduleUpdatePhaseItemParams{{Price: stripe.String(current.Price.ID), Quantity: stripe.Int64(1)}},
				ProrationBehavior: stripe.String("none"),
			},
			{
				Duration:          &stripe.SubscriptionScheduleUpdatePhaseDurationParams{Interval: stripe.String(interval), IntervalCount: stripe.Int64(1)},
				Items:             []*stripe.SubscriptionScheduleUpdatePhaseItemParams{{Price: stripe.String(price), Quantity: stripe.Int64(1)}},
				ProrationBehavior: stripe.String("none"),
			},
		},
	}
}

func stripeEventSubject(event stripe.Event) (customerID, subscriptionID string, err error) {
	var customer *stripe.Customer
	var subscription *stripe.Subscription
	switch event.Type {
	case stripe.EventTypeCheckoutSessionCompleted:
		var session stripe.CheckoutSession
		if err = json.Unmarshal(event.Data.Raw, &session); err != nil {
			return
		}
		customer, subscription = session.Customer, session.Subscription
	case stripe.EventTypeCustomerSubscriptionCreated, stripe.EventTypeCustomerSubscriptionUpdated, stripe.EventTypeCustomerSubscriptionDeleted:
		var sub stripe.Subscription
		if err = json.Unmarshal(event.Data.Raw, &sub); err != nil {
			return
		}
		customer, subscription = sub.Customer, &sub
	case stripe.EventTypeInvoicePaid, stripe.EventTypeInvoicePaymentFailed:
		var invoice stripe.Invoice
		if err = json.Unmarshal(event.Data.Raw, &invoice); err != nil {
			return
		}
		customer = invoice.Customer
		if invoice.Parent != nil && invoice.Parent.SubscriptionDetails != nil {
			subscription = invoice.Parent.SubscriptionDetails.Subscription
		}
	}
	if customer != nil {
		customerID = customer.ID
	}
	if subscription != nil {
		subscriptionID = subscription.ID
	}
	return
}

func (s *Server) stripeWebhook(c *gin.Context) {
	body, e := io.ReadAll(io.LimitReader(c.Request.Body, 1024*1024+1))
	if e != nil || len(body) > 1024*1024 {
		s.fail(c, bad("INVALID_WEBHOOK"))
		return
	}
	event, e := subscriptions.ParseStripeEvent(body, c.GetHeader("Stripe-Signature"), s.Config.StripeWebhookSecret)
	if e != nil {
		s.fail(c, bad("INVALID_STRIPE_SIGNATURE"))
		return
	}
	if event.ID == "" || event.Data == nil {
		s.fail(c, bad("INVALID_WEBHOOK"))
		return
	}
	customerID, sid, e := stripeEventSubject(event)
	if e != nil {
		s.fail(c, bad("INVALID_WEBHOOK"))
		return
	}
	if sid == "" {
		c.JSON(200, gin.H{"status": "ignored"})
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
	e = tx.QueryRow(ctx, `SELECT billing_account_id FROM subscriptions WHERE provider_customer_id=$1 FOR UPDATE`, customerID).Scan(&account)
	if e == pgx.ErrNoRows {
		s.fail(c, APIError{"BILLING_CUSTOMER_PENDING", 503})
		return
	}
	if e != nil {
		s.fail(c, e)
		return
	}
	var processed bool
	e = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM billing_events WHERE provider='stripe' AND provider_event_id=$1)`, event.ID).Scan(&processed)
	if e != nil {
		s.fail(c, e)
		return
	}
	if processed {
		c.JSON(200, gin.H{"status": "duplicate"})
		return
	}
	// Read Stripe *after* acquiring the account lock. Event delivery order is irrelevant.
	client := s.stripe()
	live, e := client.V1Subscriptions.Retrieve(ctx, sid, nil)
	if e != nil {
		s.fail(c, APIError{"STRIPE_UNAVAILABLE", 503})
		return
	}
	item := subscriptionItem(live)
	if live.Customer == nil || live.Customer.ID != customerID || live.Metadata["billing_account_id"] != account || item == nil {
		s.fail(c, bad("SUBSCRIPTION_SCOPE_MISMATCH"))
		return
	}
	var current *string
	e = tx.QueryRow(ctx, `SELECT provider_subscription_id FROM subscriptions WHERE billing_account_id=$1`, account).Scan(&current)
	if e != nil {
		s.fail(c, e)
		return
	}
	// An old canceled subscription cannot revoke a replacement subscription.
	if current != nil && *current != sid {
		if live.Status == "canceled" || live.Status == "incomplete_expired" {
			c.JSON(200, gin.H{"status": "obsolete"})
			return
		}
		s.fail(c, APIError{"DUPLICATE_SUBSCRIPTION_REVIEW", 409})
		return
	}
	plan, interval := s.mapPrice(item.Price.ID)
	if plan == "" {
		s.fail(c, bad("UNRECOGNIZED_PRICE"))
		return
	}
	var grace *time.Time
	e = tx.QueryRow(ctx, `SELECT grace_period_until FROM subscriptions WHERE billing_account_id=$1`, account).Scan(&grace)
	if e != nil {
		s.fail(c, e)
		return
	}
	if live.Status == "past_due" && grace == nil {
		// Derive from Stripe's current invoice rather than delayed webhook arrival.
		if live.LatestInvoice == nil || live.LatestInvoice.ID == "" {
			s.fail(c, APIError{"INVOICE_STATE_PENDING", 503})
			return
		}
		inv, e := client.V1Invoices.Retrieve(ctx, live.LatestInvoice.ID, nil)
		if e != nil {
			s.fail(c, APIError{"STRIPE_UNAVAILABLE", 503})
			return
		}
		until := time.Unix(inv.Created, 0).Add(7 * 24 * time.Hour)
		grace = &until
	}
	if live.Status != "past_due" {
		grace = nil
	}
	var persistSID any = sid
	if live.Status == "canceled" || live.Status == "incomplete_expired" {
		plan = "free"
		persistSID = nil
	}
	var scheduleID *string
	if live.Schedule != nil {
		scheduleID = &live.Schedule.ID
	}
	_, e = tx.Exec(ctx, `UPDATE subscriptions SET provider_subscription_id=$1,plan_code=$2,billing_interval=$3,status=$4,current_period_start=$5,current_period_end=$6,cancel_at_period_end=$7,grace_period_until=$8,scheduled_plan=CASE WHEN scheduled_plan=$2 AND scheduled_interval=$3 OR $9::text IS NULL THEN NULL ELSE scheduled_plan END,scheduled_interval=CASE WHEN scheduled_plan=$2 AND scheduled_interval=$3 OR $9::text IS NULL THEN NULL ELSE scheduled_interval END,provider_schedule_id=$9,retention_grace_until=CASE WHEN plan_code='team' AND $2 IN ('starter','free') OR plan_code='starter' AND $2='free' THEN now()+interval '30 days' ELSE retention_grace_until END,updated_at=now() WHERE billing_account_id=$10`, persistSID, plan, interval, live.Status, time.Unix(item.CurrentPeriodStart, 0), time.Unix(item.CurrentPeriodEnd, 0), live.CancelAtPeriodEnd, grace, scheduleID, account)
	if e != nil {
		s.fail(c, e)
		return
	}
	if _, e = tx.Exec(ctx, `INSERT INTO billing_events(provider_event_id,event_type) VALUES($1,$2)`, event.ID, event.Type); e != nil {
		s.fail(c, e)
		return
	}
	if _, e = tx.Exec(ctx, `DELETE FROM checkout_requests WHERE billing_account_id=$1`, account); e != nil {
		s.fail(c, e)
		return
	}
	if e = tx.Commit(ctx); e != nil {
		s.fail(c, e)
		return
	}
	c.JSON(200, gin.H{"status": "processed"})
}

var _ = platform.RandomToken
