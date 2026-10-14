// Package ws implements the dashboard live-push hub over coder/websocket.
//
// Clients connect at GET /ws (authenticated by the session cookie, origin
// checked). They subscribe to channels ("fleet", "alerts", "machine:<id>").
// The hub coalesces fleet state on a short ticker and pushes only changed
// machine_live rows — it NEVER touches the hypertables. Alert open/resolve
// events are pushed by the alert engine via PublishAlert.
package ws

import (
	"context"
	"encoding/json"
	"log"
	"strconv"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/labstack/echo/v4"

	"github.com/aliyalinkilic/tolikopa/server/internal/apierr"
	"github.com/aliyalinkilic/tolikopa/server/internal/auth"
	"github.com/aliyalinkilic/tolikopa/server/internal/config"
	"github.com/aliyalinkilic/tolikopa/server/internal/model"
)

const (
	sendBuffer   = 64
	fleetTick    = 3 * time.Second
	writeTimeout = 10 * time.Second
	// revalidateEvery is how often a live connection's session is re-checked
	// (token expiry + the per-user row lookup REST does on every request). It
	// bounds how long a socket can outlive its session when the revocation came
	// from somewhere DisconnectUser cannot see: token expiry, or another process
	// such as the break-glass CLI.
	revalidateEvery = 60 * time.Second
	revalidateTO    = 10 * time.Second
)

// Hub owns the set of connected clients and the fleet-diff broadcaster.
type Hub struct {
	pool *pgxpool.Pool
	cfg  *config.Config
	auth *auth.Auth

	baseCtx context.Context

	mu      sync.RWMutex
	clients map[*client]struct{}

	// lastSig is only accessed from the single ticker goroutine.
	lastSig map[string]string
}

type client struct {
	conn *websocket.Conn
	send chan []byte
	user string // panel username, for DisconnectUser

	mu   sync.Mutex
	subs map[string]bool
}

// NewHub constructs a hub. Call Run to start the fleet broadcaster.
func NewHub(pool *pgxpool.Pool, cfg *config.Config, a *auth.Auth) *Hub {
	return &Hub{
		pool:    pool,
		cfg:     cfg,
		auth:    a,
		clients: make(map[*client]struct{}),
		lastSig: make(map[string]string),
	}
}

// Run starts the coalescing fleet ticker and blocks until ctx is cancelled.
func (h *Hub) Run(ctx context.Context) {
	h.baseCtx = ctx
	t := time.NewTicker(fleetTick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			h.closeAll()
			return
		case <-t.C:
			h.tickFleet(ctx)
		}
	}
}

// Handle upgrades an HTTP request to a WebSocket after cookie authentication and
// origin verification, then serves the client until it disconnects.
func (h *Hub) Handle(c echo.Context) error {
	// Full session check (signature, expiry, account enabled, session epoch) —
	// the same one RequireAuth applies to REST calls.
	claims, err := h.auth.VerifyRequest(c.Request())
	if err != nil {
		return err
	}
	conn, err := websocket.Accept(c.Response(), c.Request(), &websocket.AcceptOptions{
		OriginPatterns: h.cfg.OriginPatterns(),
	})
	if err != nil {
		// Accept already wrote the failure response.
		return nil
	}

	base := h.baseCtx
	if base == nil {
		base = context.Background()
	}
	ctx, cancel := context.WithCancel(base)
	defer cancel()

	cl := &client{
		conn: conn,
		user: claims.Subject,
		send: make(chan []byte, sendBuffer),
		subs: make(map[string]bool),
	}
	h.add(cl)
	defer func() {
		h.remove(cl)
		conn.Close(websocket.StatusNormalClosure, "")
	}()

	// Second check, now that the client is registered. A revocation that landed
	// between the first check and add() found no connection to close; this one
	// sees its effect in the database. One after this point finds the client in
	// the hub (DisconnectUser). Together they leave no window.
	rctx, rcancel := context.WithTimeout(ctx, revalidateTO)
	err = h.auth.Revalidate(rctx, claims)
	rcancel()
	if err != nil {
		conn.Close(websocket.StatusPolicyViolation, "session revoked")
		return nil
	}

	// Periodic re-validation for the connection's lifetime.
	go func() {
		t := time.NewTicker(revalidateEvery)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				rctx, rcancel := context.WithTimeout(ctx, revalidateTO)
				err := h.auth.Revalidate(rctx, claims)
				rcancel()
				if err == nil {
					continue
				}
				if status, _ := apierr.Resolve(err); status >= 500 {
					// The check could not run (database unavailable). Do not drop
					// every dashboard on a blip: no fleet data flows without the
					// database either, and the next tick retries. Token expiry is
					// checked before the lookup, so it is enforced regardless.
					continue
				}
				h.remove(cl)
				conn.Close(websocket.StatusPolicyViolation, "session ended")
				cancel()
				return
			}
		}
	}()

	// Write pump.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-cl.send:
				wctx, wcancel := context.WithTimeout(ctx, writeTimeout)
				err := conn.Write(wctx, websocket.MessageText, msg)
				wcancel()
				if err != nil {
					cancel()
					return
				}
			}
		}
	}()

	// Read pump (blocks the handler for the connection's lifetime).
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			return nil
		}
		var sub struct {
			Subscribe []string `json:"subscribe"`
		}
		if json.Unmarshal(data, &sub) == nil && sub.Subscribe != nil {
			cl.mu.Lock()
			cl.subs = make(map[string]bool, len(sub.Subscribe))
			for _, ch := range sub.Subscribe {
				cl.subs[ch] = true
			}
			cl.mu.Unlock()
		}
	}
}

// Broadcast marshals msg once and delivers it to every client subscribed to the
// given channel. Slow clients (full send buffer) are dropped.
func (h *Hub) Broadcast(channel string, msg model.WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.mu.RLock()
	targets := make([]*client, 0, len(h.clients))
	for cl := range h.clients {
		cl.mu.Lock()
		subbed := cl.subs[channel]
		cl.mu.Unlock()
		if subbed {
			targets = append(targets, cl)
		}
	}
	h.mu.RUnlock()

	for _, cl := range targets {
		select {
		case cl.send <- data:
		default:
			// Buffer full: drop the slow consumer.
			go h.drop(cl)
		}
	}
}

// PublishAlert pushes an alert lifecycle event to "alerts" subscribers. It
// satisfies the alerts.Publisher interface.
func (h *Hub) PublishAlert(kind string, payload any) {
	h.Broadcast("alerts", model.WSMessage{Type: kind, Payload: payload})
}

func (h *Hub) add(cl *client) {
	h.mu.Lock()
	h.clients[cl] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) remove(cl *client) {
	h.mu.Lock()
	delete(h.clients, cl)
	h.mu.Unlock()
}

func (h *Hub) drop(cl *client) {
	h.remove(cl)
	cl.conn.Close(websocket.StatusPolicyViolation, "slow consumer")
}

// DisconnectUser closes every live connection of a panel user. A WebSocket is
// authenticated once, at upgrade, so without this a disabled or deleted account
// (or one whose password was just reset) would keep receiving the fleet feed
// until it happened to reconnect. Registered with auth.OnSessionsRevoked; a
// still-valid client simply reconnects with its current cookie.
func (h *Hub) DisconnectUser(username string) {
	h.mu.Lock()
	var victims []*client
	for cl := range h.clients {
		if cl.user == username {
			victims = append(victims, cl)
			delete(h.clients, cl)
		}
	}
	h.mu.Unlock()
	for _, cl := range victims {
		// Close in the background: the close handshake can wait on the peer.
		go cl.conn.Close(websocket.StatusPolicyViolation, "session revoked")
	}
}

func (h *Hub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for cl := range h.clients {
		cl.conn.Close(websocket.StatusGoingAway, "server shutdown")
		delete(h.clients, cl)
	}
}

// tickFleet reads the hot machine_live table, computes which rows changed since
// the last tick, and broadcasts them as a single coalesced fleet.update.
func (h *Hub) tickFleet(ctx context.Context) {
	h.mu.RLock()
	n := len(h.clients)
	h.mu.RUnlock()
	if n == 0 {
		return
	}

	rows, err := h.pool.Query(ctx, `
		SELECT m.machine_id, m.hostname, m.kind, m.domain,
		       ml.ts, ml.cpu_pct, ml.mem_used, ml.mem_total, ml.disk_min_free_pct,
		       ml.logon_user, ml.session_state, ml.open_alerts, m.agent_version, m.labels, m.tags,
		       m.pending_updates, m.reboot_pending, m.updates_checked_at,
		       m.battery_present, m.battery_percent, m.battery_ac, m.battery_charging,
		       m.battery_health_pct, m.battery_design_mwh, m.battery_full_mwh, m.battery_cycles, m.battery_updated_at
		FROM machine m
		JOIN machine_live ml ON ml.machine_id = m.machine_id
		WHERE m.archived_at IS NULL`)
	if err != nil {
		log.Printf("ws: fleet query: %v", err)
		return
	}
	defer rows.Close()

	var changed []model.FleetMachine
	warn := h.cfg.OfflineWarn()
	now := time.Now()
	for rows.Next() {
		fm, sig, err := scanFleet(rows, now, warn)
		if err != nil {
			log.Printf("ws: fleet scan: %v", err)
			return
		}
		if h.lastSig[fm.MachineID] != sig {
			h.lastSig[fm.MachineID] = sig
			changed = append(changed, fm)
		}
	}
	if len(changed) > 0 {
		h.Broadcast("fleet", model.WSMessage{
			Type:    "fleet.update",
			Payload: map[string]any{"machines": changed},
		})
		// Per-machine channel (docs/API.md §8): the machine page subscribes to
		// "machine:<id>" for its live CPU/RAM strip. Only sent when someone is
		// subscribed to that machine, so the fan-out stays proportional to viewers.
		for _, fm := range changed {
			ch := "machine:" + fm.MachineID
			if !h.hasSubscriber(ch) {
				continue
			}
			h.Broadcast(ch, metricMessage(fm))
		}
	}
}

// metricMessage builds the machine.metric push for one fleet row.
func metricMessage(fm model.FleetMachine) model.WSMessage {
	return model.WSMessage{Type: "machine.metric", Payload: model.MachineMetricPush{
		MachineID: fm.MachineID, T: fm.LastSeen, CPUPct: fm.CPUPct, MemRatio: fm.MemRatio,
	}}
}

// hasSubscriber reports whether any connected client listens on channel.
func (h *Hub) hasSubscriber(channel string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for cl := range h.clients {
		cl.mu.Lock()
		ok := cl.subs[channel]
		cl.mu.Unlock()
		if ok {
			return true
		}
	}
	return false
}

// scanFleet scans one machine_live+machine row into a FleetMachine and returns a
// change signature used to coalesce broadcasts.
func scanFleet(rows interface {
	Scan(dest ...any) error
}, now time.Time, warn time.Duration) (model.FleetMachine, string, error) {
	var (
		fm          model.FleetMachine
		domain      *string
		ts          time.Time
		cpu         *float64
		memUsed     *int64
		memTotal    *int64
		diskFree    *float64
		logonUser   *string
		sessState   *string
		agentVer    *string
		labelsBytes []byte
		pendUpd     *int
		upChecked   *time.Time
		bat         model.BatteryCols
	)
	if err := rows.Scan(&fm.MachineID, &fm.Hostname, &fm.Kind, &domain,
		&ts, &cpu, &memUsed, &memTotal, &diskFree,
		&logonUser, &sessState, &fm.OpenAlerts, &agentVer, &labelsBytes, &fm.Tags,
		&pendUpd, &fm.RebootPending, &upChecked,
		&bat.Present, &bat.Percent, &bat.AC, &bat.Charging,
		&bat.Health, &bat.Design, &bat.Full, &bat.Cycles, &bat.UpdatedAt); err != nil {
		return fm, "", err
	}
	if fm.Tags == nil {
		fm.Tags = []string{}
	}
	fm.PendingUpdates = pendUpd
	fm.UpdatesCheckedAt = upChecked
	fm.Battery = bat.DTO()
	fm.LastSeen = ts
	fm.Online = now.Sub(ts) < warn
	if domain != nil {
		fm.Domain = *domain
	}
	if cpu != nil {
		fm.CPUPct = *cpu
	}
	if memUsed != nil && memTotal != nil && *memTotal > 0 {
		fm.MemRatio = float64(*memUsed) / float64(*memTotal)
	}
	if diskFree != nil {
		fm.DiskMinFreePct = *diskFree
	}
	if logonUser != nil {
		fm.LogonUser = *logonUser
	}
	if sessState != nil {
		fm.SessionState = *sessState
	}
	if agentVer != nil {
		fm.AgentVersion = *agentVer
	}
	if len(labelsBytes) > 0 {
		_ = json.Unmarshal(labelsBytes, &fm.Labels)
	}
	sig := strconv.FormatInt(ts.UnixNano(), 10) + ":" +
		strconv.Itoa(fm.OpenAlerts) + ":" +
		strconv.FormatBool(fm.Online) + ":" +
		fm.SessionState + ":" +
		fm.LogonUser
	return fm, sig, nil
}
