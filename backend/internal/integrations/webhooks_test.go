package integrations

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
	"github.com/edcrove/provenly/backend/internal/platform/pagination"
)

func ptr[T any](v T) *T { return &v }

func TestBlockedAddresses(t *testing.T) {
	for _, ip := range []string{"127.0.0.1", "::1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254", "fe80::1", "0.0.0.0", "224.0.0.1", "ff01::1", "fc00::1",
		// Ranges the net.IP predicates miss: "this network", CGNAT / Alibaba metadata, benchmarking, documentation,
		// reserved and broadcast, Azure WireServer; IPv4-mapped, NAT64 and 6to4 forms of blocked IPv4 addresses.
		"0.1.2.3", "100.64.0.1", "100.100.100.200", "198.18.0.1", "192.0.2.1", "240.0.0.1", "255.255.255.255",
		"168.63.129.16", "::ffff:127.0.0.1", "::ffff:169.254.169.254", "64:ff9b::a9fe:a9fe", "64:ff9b:1::a00:1",
		"2002:a00:1::", "2002:7f00:1::", "2001::1", "2001:db8::1", "fec0::1"} {
		assert.True(t, blocked(net.ParseIP(ip)), ip)
	}
	for _, ip := range []string{"8.8.8.8", "140.82.112.3", "2606:4700::1111", "64:ff9b::808:808", "2002:808:808::", "100.128.0.1"} {
		assert.False(t, blocked(net.ParseIP(ip)), ip)
	}
	assert.True(t, blocked(nil), "something that is not an address is refused")
	assert.Error(t, checkDial("tcp", "no-port", nil))
	assert.ErrorIs(t, checkDial("tcp", "example.com:443", nil), errBlockedAddress)
	assert.ErrorIs(t, checkDial("tcp", "10.0.0.1:443", nil), errBlockedAddress)
	assert.NoError(t, checkDial("tcp", "8.8.8.8:443", nil))
}

// The outbound client refuses the deployment's own network after DNS resolution and never follows redirects.
func TestOutboundClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data", http.StatusFound)
	}))
	defer srv.Close()
	res, err := outboundClient(false).Get(srv.URL)
	if err == nil {
		_ = res.Body.Close()
	}
	require.Error(t, err)
	assert.Contains(t, err.Error(), errBlockedAddress.Error())
	res, err = outboundClient(true).Get(srv.URL)
	require.NoError(t, err)
	_ = res.Body.Close()
	assert.Equal(t, http.StatusFound, res.StatusCode)
}

func TestCreateWebhookValidation(t *testing.T) {
	ctx := context.Background()
	public := newFixture(t, Config{}, maintainer())
	cases := map[string]struct {
		url    string
		events []string
		field  string
	}{
		"scheme":       {"ftp://hooks.example.com", []string{EventRunCompleted}, "url"},
		"plain http":   {"http://hooks.example.com", []string{EventRunCompleted}, "url"},
		"no host":      {"https://", []string{EventRunCompleted}, "url"},
		"credentials":  {"https://u:p@hooks.example.com", []string{EventRunCompleted}, "url"},
		"unparsable":   {"https://[::1", []string{EventRunCompleted}, "url"},
		"too long":     {"https://hooks.example.com/" + strings.Repeat("a", 2000), []string{EventRunCompleted}, "url"},
		"control char": {"https://hooks.example.com/\x00", []string{EventRunCompleted}, "url"},
		"private ip":   {"https://10.0.0.5/hook", []string{EventRunCompleted}, "url"},
		"loopback ip":  {"https://[::1]/hook", []string{EventRunCompleted}, "url"},
		"no events":    {"https://hooks.example.com", nil, "events"},
		"unknown":      {"https://hooks.example.com", []string{"run.started"}, "events"},
	}
	for name, c := range cases {
		_, _, err := public.svc.CreateWebhook(ctx, "SHOP", c.url, c.events)
		e, ok := apperr.As(err)
		require.True(t, ok, name)
		assert.Equal(t, apperr.KindValidation, e.Kind, name)
		assert.Equal(t, c.field, e.Fields[0].Field, name)
	}
	// Development deployments may target private addresses over plain http.
	dev := newFixture(t, Config{AllowPrivate: true}, maintainer())
	_, _, err := dev.svc.CreateWebhook(ctx, "SHOP", "http://10.0.0.5:9000/hook", []string{EventRunCompleted})
	assert.NoError(t, err)
}

func TestCreateWebhook(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, Config{}, maintainer())
	hook, secret, err := f.svc.CreateWebhook(ctx, "SHOP", "https://hooks.example.com/p", []string{EventRunCompleted, EventRunCompleted})
	require.NoError(t, err)
	assert.Equal(t, []string{EventRunCompleted}, hook.Events)
	assert.Equal(t, "maria", hook.CreatedBy)
	assert.Regexp(t, `^whsec_[0-9a-f]{48}$`, secret)
	stored := f.repo.hooks[hook.ID].Secret
	assert.NotContains(t, stored, secret, "the secret is stored sealed")
	opened, err := f.box.Open(stored)
	require.NoError(t, err)
	assert.Equal(t, secret, opened)
	assert.Nil(t, hook.LastDelivery)

	_, _, err = f.svc.CreateWebhook(ctx, "NOPE", "https://hooks.example.com", []string{EventRunCompleted})
	assert.Equal(t, apperr.KindNotFound, kindOf(err))
	for role, kind := range map[authz.Role]apperr.Kind{authz.RoleNone: apperr.KindNotFound, authz.RoleViewer: apperr.KindForbidden, authz.RoleMember: apperr.KindForbidden} {
		g := newFixture(t, Config{}, fakeAccess{role: role})
		_, _, err = g.svc.CreateWebhook(ctx, "SHOP", "https://hooks.example.com", []string{EventRunCompleted})
		assert.Equal(t, kind, kindOf(err), role.String())
	}
	g := newFixture(t, Config{}, fakeAccess{role: authz.RoleMaintainer, actorErr: errBoom})
	_, _, err = g.svc.CreateWebhook(ctx, "SHOP", "https://hooks.example.com", []string{EventRunCompleted})
	assert.ErrorIs(t, err, errBoom)
	f.repo.errs["CreateWebhook"] = errBoom
	_, _, err = f.svc.CreateWebhook(ctx, "SHOP", "https://hooks.example.com", []string{EventRunCompleted})
	assert.ErrorIs(t, err, errBoom)
}

func TestListWebhooks(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, Config{}, maintainer())
	a, _, _ := f.svc.CreateWebhook(ctx, "SHOP", "https://a.example.com", []string{EventRunCompleted})
	_, _, _ = f.svc.CreateWebhook(ctx, "SHOP", "https://b.example.com", []string{EventRunCompleted})
	_, _, _ = f.svc.CreateWebhook(ctx, "OTHER", "https://c.example.com", []string{EventRunCompleted})
	_, err := f.svc.Ping(ctx, "SHOP", a.ID)
	require.NoError(t, err)
	hooks, err := f.svc.Webhooks(ctx, "SHOP")
	require.NoError(t, err)
	require.Len(t, hooks, 2)
	require.NotNil(t, hooks[0].LastDelivery)
	assert.Equal(t, EventPing, hooks[0].LastDelivery.Event)
	assert.Nil(t, hooks[1].LastDelivery)

	_, err = f.svc.Webhooks(ctx, "NOPE")
	assert.Equal(t, apperr.KindNotFound, kindOf(err))
	f.repo.errs["LastDeliveries"] = errBoom
	_, err = f.svc.Webhooks(ctx, "SHOP")
	assert.ErrorIs(t, err, errBoom)
	f.repo.errs["ListWebhooks"] = errBoom
	_, err = f.svc.Webhooks(ctx, "SHOP")
	assert.ErrorIs(t, err, errBoom)
}

func TestUpdateWebhook(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, Config{}, maintainer())
	hook, _, _ := f.svc.CreateWebhook(ctx, "SHOP", "https://a.example.com", []string{EventRunCompleted})

	for name, in := range map[string]UpdateWebhookInput{
		"empty":  {},
		"url":    {URL: ptr("http://a.example.com")},
		"events": {Events: []string{}},
	} {
		_, err := f.svc.UpdateWebhook(ctx, "SHOP", hook.ID, in)
		assert.Equal(t, apperr.KindValidation, kindOf(err), name)
	}
	updated, err := f.svc.UpdateWebhook(ctx, "SHOP", hook.ID, UpdateWebhookInput{URL: ptr("https://b.example.com"), Events: []string{EventRunCompleted}, Active: ptr(false)})
	require.NoError(t, err)
	assert.Equal(t, "https://b.example.com", updated.URL)
	assert.False(t, updated.Active)

	_, err = f.svc.UpdateWebhook(ctx, "SHOP", 99, UpdateWebhookInput{Active: ptr(true)})
	e, _ := apperr.As(err)
	require.NotNil(t, e)
	assert.Equal(t, "webhook 99 not found", e.Message)
	_, err = f.svc.UpdateWebhook(ctx, "OTHER", hook.ID, UpdateWebhookInput{Active: ptr(true)})
	assert.Equal(t, apperr.KindNotFound, kindOf(err), "a webhook of another project is not found")
	_, err = f.svc.UpdateWebhook(ctx, "NOPE", hook.ID, UpdateWebhookInput{Active: ptr(true)})
	assert.Equal(t, apperr.KindNotFound, kindOf(err))

	f.repo.errs["LastDeliveries"] = errBoom
	_, err = f.svc.UpdateWebhook(ctx, "SHOP", hook.ID, UpdateWebhookInput{Active: ptr(true)})
	assert.ErrorIs(t, err, errBoom)
	f.repo.errs["GetWebhookAfterUpdate"] = errBoom
	_, err = f.svc.UpdateWebhook(ctx, "SHOP", hook.ID, UpdateWebhookInput{Active: ptr(true)})
	assert.ErrorIs(t, err, errBoom)
	delete(f.repo.errs, "GetWebhook")
	delete(f.repo.errs, "GetWebhookAfterUpdate")
	f.repo.errs["UpdateWebhook"] = errBoom
	_, err = f.svc.UpdateWebhook(ctx, "SHOP", hook.ID, UpdateWebhookInput{Active: ptr(true)})
	assert.ErrorIs(t, err, errBoom)
	f.repo.errs["GetWebhook"] = errBoom
	_, err = f.svc.UpdateWebhook(ctx, "SHOP", hook.ID, UpdateWebhookInput{Active: ptr(true)})
	assert.ErrorIs(t, err, errBoom)
}

func TestPingAndDeliveries(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, Config{}, maintainer())
	hook, _, _ := f.svc.CreateWebhook(ctx, "SHOP", "https://a.example.com", []string{EventRunCompleted})
	for range 3 {
		d, err := f.svc.Ping(ctx, "SHOP", hook.ID)
		require.NoError(t, err)
		assert.Equal(t, DeliveryPending, d.Status)
		var body map[string]any
		require.NoError(t, json.Unmarshal(d.Payload, &body))
		assert.Equal(t, EventPing, body["event"])
	}
	page, err := f.svc.Deliveries(ctx, "SHOP", hook.ID, pagination.Page{Number: 1, Size: 2})
	require.NoError(t, err)
	assert.Equal(t, int64(3), page.Total)
	assert.Equal(t, []int64{3, 2}, []int64{page.Items[0].ID, page.Items[1].ID}, "newest first")

	_, err = f.svc.Deliveries(ctx, "SHOP", 99, pagination.Default())
	assert.Equal(t, apperr.KindNotFound, kindOf(err))
	f.repo.errs["CountDeliveries"] = errBoom
	_, err = f.svc.Deliveries(ctx, "SHOP", hook.ID, pagination.Default())
	assert.ErrorIs(t, err, errBoom)
	f.repo.errs["ListDeliveries"] = errBoom
	_, err = f.svc.Deliveries(ctx, "SHOP", hook.ID, pagination.Default())
	assert.ErrorIs(t, err, errBoom)

	_, err = f.svc.Ping(ctx, "SHOP", 99)
	assert.Equal(t, apperr.KindNotFound, kindOf(err))
	f.repo.errs["InsertDelivery"] = errBoom
	_, err = f.svc.Ping(ctx, "SHOP", hook.ID)
	assert.ErrorIs(t, err, errBoom)
}

func TestRunCompleted(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, Config{}, maintainer())
	run := execution.TestRun{ID: 7, ProjectID: 2, Provider: "github", ProviderRunID: "1", RunAttempt: 1, Status: execution.RunCompleted}

	f.svc.RunCompleted(ctx, run)
	assert.Empty(t, f.repo.deliveries, "no webhooks: nothing queued")

	a, _, _ := f.svc.CreateWebhook(ctx, "SHOP", "https://a.example.com", []string{EventRunCompleted})
	b, _, _ := f.svc.CreateWebhook(ctx, "SHOP", "https://b.example.com", []string{EventRunCompleted})
	_, _ = f.svc.UpdateWebhook(ctx, "SHOP", b.ID, UpdateWebhookInput{Active: ptr(false)})
	_, _, _ = f.svc.CreateWebhook(ctx, "OTHER", "https://c.example.com", []string{EventRunCompleted})
	f.svc.RunCompleted(ctx, run)
	require.Len(t, f.repo.deliveries, 1, "only the active webhook of the run's project")
	d := f.repo.deliveries[0]
	assert.Equal(t, a.ID, d.WebhookID)
	var body struct {
		Event   string            `json:"event"`
		Project map[string]string `json:"project"`
		Run     struct {
			ID     int64  `json:"id"`
			Status string `json:"executionStatus"`
		} `json:"run"`
	}
	require.NoError(t, json.Unmarshal(d.Payload, &body))
	assert.Equal(t, EventRunCompleted, body.Event)
	assert.Equal(t, map[string]string{"key": "SHOP", "name": "Shop"}, body.Project)
	assert.Equal(t, int64(7), body.Run.ID)
	assert.Equal(t, "completed", body.Run.Status)

	// Failures are logged, never returned (recording the run must not fail).
	f.repo.errs["InsertDelivery"] = errBoom
	f.svc.RunCompleted(ctx, run)
	f.cat.byIDErr = errBoom
	f.svc.RunCompleted(ctx, run)
	f.repo.errs["ListSubscribedWebhooks"] = errBoom
	f.svc.RunCompleted(ctx, run)
	assert.Len(t, f.repo.deliveries, 1)
}

func TestSign(t *testing.T) {
	// Reference value: printf '1700000000.{"a":1}' | openssl dgst -sha256 -hmac whsec_test
	assert.Equal(t, "sha256=38877139021993b830af32feea6e18a8da83eb2f6e49ee50bd9e4cf4ca4d3789", Sign("whsec_test", "1700000000", []byte(`{"a":1}`)))
	assert.Equal(t, Sign("s", "1", []byte("b")), Sign("s", "1", []byte("b")))
	assert.NotEqual(t, Sign("s", "1", []byte("b")), Sign("s", "2", []byte("b")), "the timestamp is signed")
	assert.NotEqual(t, Sign("s", "1", []byte("b")), Sign("t", "1", []byte("b")))
}

// receiver is a webhook endpoint answering with the next status of codes (the last one repeats).
type receiver struct {
	mu       sync.Mutex
	codes    []int
	requests []*http.Request
	bodies   [][]byte
}

func (rc *receiver) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	rc.requests, rc.bodies = append(rc.requests, r), append(rc.bodies, body)
	code := rc.codes[min(len(rc.requests)-1, len(rc.codes)-1)]
	w.WriteHeader(code)
}

func TestDeliverDue(t *testing.T) {
	ctx := context.Background()
	rc := &receiver{codes: []int{http.StatusNoContent}}
	srv := httptest.NewServer(rc)
	defer srv.Close()
	f := newFixture(t, Config{AllowPrivate: true}, maintainer())
	hook, secret, err := f.svc.CreateWebhook(ctx, "SHOP", srv.URL+"/hook", []string{EventRunCompleted})
	require.NoError(t, err)
	_, _ = f.svc.Ping(ctx, "SHOP", hook.ID)
	_, _ = f.svc.Ping(ctx, "SHOP", hook.ID)

	n, err := f.svc.DeliverDue(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
	require.Len(t, rc.requests, 2)
	r := rc.requests[0]
	assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
	assert.Equal(t, EventPing, r.Header.Get("X-Provenly-Event"))
	assert.Equal(t, "1", r.Header.Get("X-Provenly-Delivery"))
	ts := r.Header.Get("X-Provenly-Timestamp")
	assert.Equal(t, "1791201600", ts)
	assert.Equal(t, Sign(secret, ts, rc.bodies[0]), r.Header.Get("X-Provenly-Signature"), "receivers verify with the secret")
	d := f.repo.deliveries[0]
	assert.Equal(t, DeliverySucceeded, d.Status)
	assert.Equal(t, int32(1), d.Attempts)
	assert.Equal(t, int32(204), *d.LastStatusCode)

	n, err = f.svc.DeliverDue(ctx)
	require.NoError(t, err)
	assert.Zero(t, n, "nothing due")
}

func TestDeliveryRetries(t *testing.T) {
	ctx := context.Background()
	rc := &receiver{codes: []int{http.StatusInternalServerError}}
	srv := httptest.NewServer(rc)
	defer srv.Close()
	f := newFixture(t, Config{AllowPrivate: true}, maintainer())
	hook, _, _ := f.svc.CreateWebhook(ctx, "SHOP", srv.URL, []string{EventRunCompleted})
	_, _ = f.svc.Ping(ctx, "SHOP", hook.ID)

	waits := []time.Duration{10 * time.Second, time.Minute, 5 * time.Minute, 30 * time.Minute}
	for i := range MaxDeliveryAttempts {
		_, err := f.svc.DeliverDue(ctx)
		require.NoError(t, err)
		d := &f.repo.deliveries[0]
		assert.Equal(t, int32(i+1), d.Attempts)
		assert.Equal(t, "the endpoint answered 500", d.LastError)
		if i < MaxDeliveryAttempts-1 {
			assert.Equal(t, DeliveryPending, d.Status)
			assert.Equal(t, now.Add(waits[i]), d.NextAttemptAt)
			d.NextAttemptAt = now // make it due again
		} else {
			assert.Equal(t, DeliveryFailed, d.Status, "failed for good after the last attempt")
		}
	}
	assert.Len(t, rc.requests, MaxDeliveryAttempts)
}

func TestDeliveryFailures(t *testing.T) {
	ctx := context.Background()
	f := newFixture(t, Config{AllowPrivate: true}, maintainer())
	// Unreachable endpoint: retried, without a status code.
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	hook, _, _ := f.svc.CreateWebhook(ctx, "SHOP", closed.URL, []string{EventRunCompleted})
	_, _ = f.svc.Ping(ctx, "SHOP", hook.ID)
	_, err := f.svc.DeliverDue(ctx)
	require.NoError(t, err)
	d := f.repo.deliveries[0]
	assert.Equal(t, DeliveryPending, d.Status)
	assert.Nil(t, d.LastStatusCode)
	assert.Contains(t, d.LastError, "connect")

	// A paused webhook fails its pending deliveries without sending them.
	f.repo.deliveries[0].NextAttemptAt = now
	_, _ = f.svc.UpdateWebhook(ctx, "SHOP", hook.ID, UpdateWebhookInput{Active: ptr(false)})
	_, err = f.svc.DeliverDue(ctx)
	require.NoError(t, err)
	assert.Equal(t, DeliveryFailed, f.repo.deliveries[0].Status)
	assert.Equal(t, "the webhook is paused", f.repo.deliveries[0].LastError)

	// A secret sealed with another key (PROVENLY_SECRETS_KEY changed) cannot sign: failed.
	other, _, _ := f.svc.CreateWebhook(ctx, "SHOP", closed.URL, []string{EventRunCompleted})
	w := f.repo.hooks[other.ID]
	w.Secret = "v1:AAAA"
	f.repo.hooks[other.ID] = w
	_, _ = f.svc.Ping(ctx, "SHOP", other.ID)
	_, err = f.svc.DeliverDue(ctx)
	require.NoError(t, err)
	assert.Equal(t, DeliveryFailed, f.repo.deliveries[1].Status)
	assert.Contains(t, f.repo.deliveries[1].LastError, "PROVENLY_SECRETS_KEY")

	_, _ = f.svc.Ping(ctx, "SHOP", other.ID)
	f.repo.errs["FinishAttempt"] = errBoom
	_, err = f.svc.DeliverDue(ctx)
	assert.ErrorIs(t, err, errBoom)
	f.repo.errs["GetWebhookByID"] = errBoom
	_, err = f.svc.DeliverDue(ctx)
	assert.ErrorIs(t, err, errBoom)
	f.repo.errs["ClaimDueDeliveries"] = errBoom
	_, err = f.svc.DeliverDue(ctx)
	assert.ErrorIs(t, err, errBoom)
}

func TestTruncate(t *testing.T) {
	assert.Equal(t, "ab", truncate("abc", 2))
	assert.Equal(t, "añ", truncate("añb", 2))
	assert.Equal(t, "abc", truncate("abc", 5))
}

// Run sends due deliveries on every tick, logs failures and stops with its context.
func TestRun(t *testing.T) {
	f := newFixture(t, Config{}, maintainer())
	ctx, cancel := context.WithCancel(context.Background())
	f.repo.errs["ClaimDueDeliveries"] = errBoom
	f.repo.onClaim = func(n int) {
		if n == 3 {
			cancel()
		}
	}
	done := make(chan struct{})
	go func() {
		f.svc.Run(ctx, time.Millisecond)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop")
	}
	assert.Equal(t, 3, f.repo.claims)
}

// Finished deliveries older than the retention are purged in batches until a short batch; no retention keeps all
// (card #51).
func TestPurgeDeliveries(t *testing.T) {
	keep := newFixture(t, Config{}, maintainer())
	keep.repo.purgeable = 5
	n, err := keep.svc.PurgeDeliveries(context.Background())
	require.NoError(t, err)
	assert.Zero(t, n)
	assert.Empty(t, keep.repo.purges, "no retention: never purged")

	f := newFixture(t, Config{DeliveryRetention: 90 * 24 * time.Hour}, maintainer())
	f.repo.purgeable = 2*purgeBatch + 7
	n, err = f.svc.PurgeDeliveries(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(2*purgeBatch+7), n)
	require.Len(t, f.repo.purges, 3, "two full batches, then a short one")
	assert.Equal(t, now.Add(-90*24*time.Hour), f.repo.purges[0])

	f.repo.purgeable = purgeBatch + 1
	f.repo.errs["PurgeDeliveries"] = errBoom
	n, err = f.svc.PurgeDeliveries(context.Background())
	assert.ErrorIs(t, err, errBoom)
	assert.Equal(t, int64(purgeBatch), n, "what was deleted before the failure")
}

// Run purges when it starts and then once per hour, and logs a failed purge without stopping.
func TestRunPurges(t *testing.T) {
	f := newFixture(t, Config{DeliveryRetention: time.Hour}, maintainer())
	f.repo.errs["PurgeDeliveries"] = errBoom
	ctx, cancel := context.WithCancel(context.Background())
	f.repo.onClaim = func(n int) {
		if n == 3 {
			cancel()
		}
	}
	f.svc.Run(ctx, time.Millisecond)
	assert.Len(t, f.repo.purges, 1, "the clock did not move: one purge for three passes")
}
