package metrics

import (
	"database/sql"
	"sort"
	"strings"
	"sync"
	"time"
)

// The operational counters (2.5-0).
//
// Why a registry of named numbers instead of the log: the 2026-10 review's
// standing finding was "no dropped-frame counters anywhere", and buglist.md
// M8/M10 record silent drops whose only trace is a `log.Printf`. A log line is
// not observable — nobody greps a live server for a number, and a drop storm
// buries the evidence it is supposed to leave. So every place a message can go
// missing now moves a named counter, and the log line stays only as a
// rate-limited hint beside it.
//
// The names are fixed here, as constants, because they are the contract: every
// one is listed by name in `docs/architecture.md`, and 2.5-1's `/metrics` and
// 2.5-2's console panel read this registry rather than inventing their own
// vocabulary. Renaming one is therefore an API change, not a rename — a
// dashboard and an operator's alert rule point at the string.
//
// Grammar: `domain.what.reason` for a drop (something was not delivered), and
// `domain.what` for a quantity. A drop site's reason says *why* the message was
// lost, because "the viewer froze" and "the alert engine fell behind" are
// different incidents that one "dropped" total cannot tell apart.
const (
	// --- WebSocket drop sites (the `select { … default: }` sites in server/ws) ---

	// Hub.BroadcastMessage: the hub's own broadcast queue is full and this
	// instance has no Redis to hand the message to, so it is discarded.
	WSBroadcastFull = "ws.broadcast.full"
	// Hub.BroadcastMessage: the same queue-full moment, but Redis took the
	// message directly so the other instances still get it (M8's fallback).
	// Not a loss on this instance — a degradation, and it must read as one.
	WSBroadcastFallback = "ws.broadcast.fallback"
	// Hub.Run: a local client's send buffer was full while a broadcast was
	// fanned out; that client did not receive this message.
	WSBroadcastLocal = "ws.broadcast.local"
	// Hub.handleRemoteBroadcast: a client on this instance could not take a
	// broadcast that arrived from another instance.
	WSBroadcastRemote = "ws.broadcast.remote"
	// Hub.handleRemoteBroadcast: a Redis echo of this instance's own publish,
	// suppressed instead of delivered twice (M8). It counts duplicates that
	// were *not* delivered, so it is a mechanism working: a nonzero value is
	// expected on every multi-instance install, which is why it is not a drop.
	WSBroadcastSuppressed = "ws.broadcast.suppressed"
	// Hub.handleRemoteTargeted: a client on this instance could not take a
	// message addressed to it by another instance (H2's cross-instance path).
	WSTargetedRemote = "ws.targeted.remote"
	// Hub.SendToDevice: the device's connection could not take the message.
	WSSendDevice = "ws.send.device"
	// Hub.SendToUser: one of the user's browser connections was full.
	WSSendUser = "ws.send.user"
	// Hub.SendToViewer: the native viewer attached to a session was full.
	WSSendViewer = "ws.send.viewer"
	// Hub.sendRemoteExe: the remote-control executable's connection was full.
	WSSendRemoteExe = "ws.send.remote_exe"
	// Hub.sendAgent: the agent service's connection was full.
	WSSendAgent = "ws.send.agent"
	// The app-level ping reply to a browser or remote exe (H1).
	WSSendPong = "ws.send.pong"
	// The same ping reply on a native viewer connection.
	WSSendViewerPong = "ws.send.viewer_pong"
	// Hub.handleRemoteTextMessage: a message the remote exe sent to its viewer
	// could not be queued for the viewer.
	WSSendViewerRelay = "ws.send.viewer_relay"
	// The `binary_frames` capability announcement to a remote exe: a slow exe
	// that misses it falls back to HTTP frame upload, which works but is the
	// slower path.
	WSSendBinaryFrames = "ws.send.binary_frames"
	// The `server_version` answer to a heartbeat: the agent learns about an
	// update on its next heartbeat instead of this one.
	WSSendServerVersion = "ws.send.server_version"
	// Hub.sendViewerFrame: a queued frame was evicted so the viewer always sees
	// the newest screen. Perishable by design, and counted so "the screen
	// froze" has a number behind it.
	WSViewerFrameEvicted = "ws.viewer_frame.evicted"
	// Hub.sendViewerFrame: even the eviction found no room, so the new frame
	// was discarded outright.
	WSViewerFrameFull = "ws.viewer_frame.full"
	// The alert worker's input queue was full: this sample will never be
	// measured against its thresholds, which is an alert that did not fire.
	WSAlertQueueFull = "ws.alert_queue.full"
	// Hub.relayRemoteFrame: the session owner has no browser connection to
	// carry the captured frame, so the frame is thrown away. The console shows
	// a session that is live and black; this counter is why.
	WSSessionFrameUnconnected = "ws.session_frame.unconnected"

	// --- The rest of what the operator is told (2.5-2 reads these) -------------

	// Ingest counters. They are counts, not rates: a rate is two samples of a
	// count, and the snapshot carries `SampledAt` so the reader can divide.
	// Split per transport because an agent heartbeating over HTTP and one
	// streaming over the WebSocket fail in different ways.
	//
	// `IngestMetricsHTTP` is the one name in this block that **nothing moves
	// yet**: `DeviceHandler.ReportMetrics` binds its payload into a local named
	// `metrics`, which shadows this package for the rest of that function, so
	// the increment does not go in on a rename done blind. It is left declared
	// and marked, not quietly emitted from the wrong place and not silently
	// dropped. A counter that is read but never written would be the exact
	// dishonesty this whole change exists to remove.
	IngestHeartbeatWS   = "ingest.heartbeat.ws"
	IngestHeartbeatHTTP = "ingest.heartbeat.http"
	IngestMetricsWS     = "ingest.metrics.ws"
	// IngestMetricsHTTP — DECLARED, NOT EMITTED (see the note above).
	IngestMetricsHTTP = "ingest.metrics.http"

	// AlertEvalLatency is the histogram the release gate needs: 2.5-8 has to
	// show the alert engine's p95 under the read deadline that produced M5.
	AlertEvalLatency = "alert.eval.latency"

	// SessionEnd is moved where a session visibly ends: the viewer socket
	// closing (server/ws/viewer.go). `SessionStart` is **declared but not
	// emitted** — sessions become active from more than one path (the API's
	// POST, the device accepting over the WebSocket), and instrumenting only one
	// of them would make start/end disagree in the direction that matters.
	// Until every start path is covered, the pair is not a fact, so only the end
	// is claimed.
	SessionStart = "session.start" // DECLARED, NOT EMITTED
	SessionEnd   = "session.end"

	// DECLARED, NOT EMITTED. Both of the following belong to the
	// queued-offline-deploy ledger that 2.5-5 and 2.5-6 build, which does not
	// exist on this branch. They are named here so the vocabulary is settled
	// once, and `docs/architecture.md` marks them as not yet emitted; nothing
	// reads them as a measurement until a call site moves them.
	//
	// DeployQueuedDepth is the queued-offline-deploy ledger's depth: the answer
	// to "why hasn't this unit patched yet" before anyone opens a database. A
	// gauge, because it goes down as well as up.
	DeployQueuedDepth = "deploy.queued.depth"
	// DeployOutcome counts a recorded per-device outcome, labelled by `kind`
	// (deploy, rollback) and `result` (success, failed, …). The ledger holds
	// the detail; this is the shape of it.
	DeployOutcome = "deploy.outcome"

	// WSConnectionsActive is the live-connection gauge, labelled `role`
	// (user, device, remote, viewer). "Active user connections" is this gauge
	// with role="user": a user may hold several (tabs, the unit page and its
	// session viewer), and a fleet with 40 consoles open is a different
	// incident from one console in 40 tabs.
	WSConnectionsActive = "ws.connections.active"

	// Redis transitions. A degraded Redis is not an outage — the hub keeps
	// working locally and loses only cross-instance delivery — so what the
	// operator needs is the transition, and 2.5-2 states it with its time:
	// "Redis degraded 11 minutes ago" is a different sentence from "Redis is
	// degraded".
	RedisDegradedTransitions  = "redis.degraded.transitions"
	RedisRecoveredTransitions = "redis.recovered.transitions"
	RedisDegradedState        = "redis.degraded"

	// DECLARED, NOT EMITTED. The pool reaches a snapshot through `DBPoolStats`,
	// which carries these same six numbers as struct fields — the registry names
	// exist so 2.5-1 has something to print, not because a writer moves them.
	// `SetDBPool` has no caller yet either, so today the snapshot honestly
	// reports `Available: false` rather than a pool of zero connections. Wiring
	// the call belongs with 2.5-2, which is the surface that shows it.
	//
	// DB pool. Stated plainly, because it is not what the plan's shorthand
	// implies: `sql.DB.Stats()` has no instantaneous "waiting" count. It
	// reports WaitCount/WaitDuration as cumulative totals — connections that
	// *had* to wait, ever — so `db.pool.wait_total` is a total, not a queue
	// depth. The instantaneous read on the same pressure is in_use against
	// max_open; a pool pinned there is that fact.
	DBPoolOpen           = "db.pool.open"
	DBPoolIdle           = "db.pool.idle"
	DBPoolInUse          = "db.pool.in_use"
	DBPoolMaxOpen        = "db.pool.max_open"
	DBPoolWaitTotal      = "db.pool.wait_total"
	DBPoolWaitDurationMS = "db.pool.wait_duration_ms"
)

// Labels are a metric's dimensions. The set is deliberately tiny and named in
// `docs/architecture.md`: `role`, `kind`, `result`. A device key is never a
// label — C3's rule applied to this surface, because a metrics payload is
// readable by whoever holds the metrics token, and a fleet's device keys are
// credentials.
type Labels map[string]string

// kindOf distinguishes the two shapes a number can have, because a reader
// treats them differently: a counter's absolute value means nothing, only its
// movement does; a gauge's value is the fact.
type kindOf int

const (
	kindCounter kindOf = iota
	kindGauge
)

// metricID is the registry key. Labels are canonicalised into a sorted string,
// so {"a":"1","b":"2"} and {"b":"2","a":"1"} are one metric, not two.
type metricID struct {
	name   string
	labels string
}

type metricValue struct {
	kind kindOf
	val  float64
}

// histogram is one named distribution. Counts are cumulative (Prometheus' `le`
// grammar: Counts[i] holds the observations ≤ Buckets[i]) so 2.5-1 can print
// them without arithmetic.
type histogram struct {
	buckets []float64
	counts  []uint64
	sum     float64
	count   uint64
}

// alertEvalBuckets brackets the latency the release gate cares about:
// sub-millisecond when the device scope is cached, seconds when the database is
// the bottleneck, and past 10 s an evaluation is the M5 incident. Seconds.
var alertEvalBuckets = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

var (
	countersMu  sync.Mutex
	registry    = map[metricID]metricValue{}
	histograms  = map[string]*histogram{}
	loggedAt    = map[string]time.Time{}
	dbPool      *sql.DB
	redisState  = redisHealth{}
	logClockNow = time.Now
)

// redisHealth is the degraded state plus when it last changed. The timestamp is
// why this is a struct and not just a gauge: 2.5-2 must state staleness
// honestly, and a 0/1 number cannot carry "for eleven minutes".
type redisHealth struct {
	degraded bool
	since    time.Time
}

func canonicalLabels(l Labels) string {
	if len(l) == 0 {
		return ""
	}
	keys := make([]string, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, len(keys))
	for i, k := range keys {
		pairs[i] = k + "=" + l[k]
	}
	return strings.Join(pairs, ",")
}

// addLocked moves one counter. countersMu must be held.
func addLocked(id metricID, delta float64) {
	v := registry[id]
	v.kind = kindCounter
	v.val += delta
	registry[id] = v
}

// setLocked records one gauge. countersMu must be held.
func setLocked(id metricID, value float64) {
	registry[id] = metricValue{kind: kindGauge, val: value}
}

// Add increments a counter. Counters never move down: a drop site that wants to
// "undo" a drop is a bug, not a use case.
func Add(name string, labels Labels, delta uint64) {
	if delta == 0 {
		return
	}
	countersMu.Lock()
	addLocked(metricID{name: name, labels: canonicalLabels(labels)}, float64(delta))
	countersMu.Unlock()
}

// Inc adds one to an unlabelled counter.
func Inc(name string) { Add(name, nil, 1) }

// SetGauge records a value that can go down as well as up.
func SetGauge(name string, labels Labels, value int64) {
	countersMu.Lock()
	setLocked(metricID{name: name, labels: canonicalLabels(labels)}, float64(value))
	countersMu.Unlock()
}

// AddGauge moves a gauge by a delta. The caller owns the sign.
func AddGauge(name string, labels Labels, delta int64) {
	id := metricID{name: name, labels: canonicalLabels(labels)}
	countersMu.Lock()
	v := registry[id]
	v.kind = kindGauge
	v.val += float64(delta)
	registry[id] = v
	countersMu.Unlock()
}

// Value reads one metric. An untouched metric reads 0, which is the honest
// answer for both shapes: nothing counted, nothing queued.
func Value(name string, labels Labels) float64 {
	countersMu.Lock()
	defer countersMu.Unlock()
	return registry[metricID{name: name, labels: canonicalLabels(labels)}].val
}

// observe records one value into a named histogram, creating it on first use.
func observe(name string, buckets []float64, seconds float64) {
	countersMu.Lock()
	defer countersMu.Unlock()
	h := histograms[name]
	if h == nil {
		h = &histogram{buckets: buckets, counts: make([]uint64, len(buckets))}
		histograms[name] = h
	}
	for i, b := range h.buckets {
		if seconds <= b {
			h.counts[i]++
		}
	}
	h.sum += seconds
	h.count++
}

// ObserveAlertEval records one alert-engine evaluation's wall time into
// AlertEvalLatency. Each caller times its own call: the engine is reached from
// the WebSocket metrics path and from the HTTP one, and a percentile that
// quietly covers only one of them is worse than none.
func ObserveAlertEval(d time.Duration) {
	if d < 0 {
		d = 0
	}
	observe(AlertEvalLatency, alertEvalBuckets, d.Seconds())
}

// AllowLog reports whether a log line for `key` may be written now: at most one
// per window, per key. The keys are the drop-site constants above, so the map
// cannot grow past the number of sites — a throttle keyed on the varying part of
// a message (a session id, a device key) grows without bound in exactly the
// storm it exists to survive. The trade is stated: the second slow *session*
// inside a window is counted and not logged.
//
// `logClockNow` is a variable so a test can move time instead of sleeping out a
// window; production never reassigns it.
func AllowLog(key string, window time.Duration) bool {
	now := logClockNow()
	countersMu.Lock()
	defer countersMu.Unlock()
	if last, ok := loggedAt[key]; ok && now.Sub(last) < window {
		return false
	}
	loggedAt[key] = now
	return true
}

// SetDBPool points the snapshot at the server's database pool. Never calling
// this, or passing nil, leaves the pool section reporting Available: false — a
// health endpoint that says nothing about the pool is honest; one that says
// `open=0` on a server holding 100 connections is not.
//
// The stats are read when a snapshot is taken, never from a request path:
// `sql.DB.Stats()` walks the pool's state, and paying that per heartbeat is a
// cost the fleet carries for a number nobody is looking at.
func SetDBPool(db *sql.DB) {
	countersMu.Lock()
	dbPool = db
	countersMu.Unlock()
}

// DBPoolStats is one reading of the pool.
type DBPoolStats struct {
	// Available is false when no pool is registered or it cannot be read.
	Available      bool
	Open           int
	Idle           int
	InUse          int
	MaxOpen        int
	WaitTotal      int64
	WaitDurationMS int64
	SampledAt      time.Time
}

// dbPoolStats reads the registered pool, if there is one.
func dbPoolStats(now time.Time) DBPoolStats {
	countersMu.Lock()
	db := dbPool
	countersMu.Unlock()
	if db == nil {
		return DBPoolStats{SampledAt: now}
	}
	s := db.Stats()
	return DBPoolStats{
		Available:      true,
		Open:           s.OpenConnections,
		Idle:           s.Idle,
		InUse:          s.InUse,
		MaxOpen:        s.MaxOpenConnections,
		WaitTotal:      s.WaitCount,
		WaitDurationMS: int64(s.WaitDuration / time.Millisecond),
		SampledAt:      now,
	}
}

// RedisDegraded records that Redis could not be reached. Only the first failure
// in a streak moves the counters: a Redis that stays down keeps failing, and
// counting one degradation per failed publish tells the operator about hundreds
// of incidents when there was one.
func RedisDegraded() {
	now := logClockNow()
	countersMu.Lock()
	if !redisState.degraded {
		redisState.degraded = true
		redisState.since = now
		addLocked(metricID{name: RedisDegradedTransitions}, 1)
		setLocked(metricID{name: RedisDegradedState}, 1)
	}
	countersMu.Unlock()
}

// RedisRecovered records a Redis operation that succeeded after a degraded
// streak, and closes the incident RedisDegraded opened.
func RedisRecovered() {
	now := logClockNow()
	countersMu.Lock()
	if redisState.degraded {
		redisState.degraded = false
		redisState.since = now
		addLocked(metricID{name: RedisRecoveredTransitions}, 1)
		setLocked(metricID{name: RedisDegradedState}, 0)
	}
	countersMu.Unlock()
}

// RedisHealth is the degraded state with the time it last changed.
type RedisHealth struct {
	Degraded bool
	Since    time.Time
}

// Metric is one named number in a snapshot.
type Metric struct {
	Name   string
	Labels Labels
	Value  float64
}

// Histogram is one named distribution in a snapshot.
type Histogram struct {
	Name    string
	Buckets []float64
	Counts  []uint64
	Sum     float64
	Count   uint64
}

// MetricsSnapshot is everything this process knows about itself, in one read.
// It is the single source for both consumers the plan names — 2.5-1's `/metrics`
// exposition and 2.5-2's health detail endpoint — so the two can never disagree
// about what a name means.
//
// It is process-local by design. A fleet of three instances has three
// snapshots; merging them is the console's job, and merging here would hide
// which instance dropped the frames.
type MetricsSnapshot struct {
	SampledAt  time.Time
	Counters   []Metric
	Gauges     []Metric
	Histograms []Histogram
	Redis      RedisHealth
	DBPool     DBPoolStats
}

// Snapshot reads the registry. The slices are built fresh and sorted by name
// then labels, so two readers of unchanged state see the same order and a test
// can assert on it.
func Snapshot() MetricsSnapshot {
	now := logClockNow()
	snap := MetricsSnapshot{
		SampledAt: now,
		DBPool:    dbPoolStats(now),
	}

	countersMu.Lock()
	snap.Redis = RedisHealth{Degraded: redisState.degraded, Since: redisState.since}
	for id, v := range registry {
		m := Metric{Name: id.name, Value: v.val}
		if id.labels != "" {
			m.Labels = parseLabels(id.labels)
		}
		if v.kind == kindGauge {
			snap.Gauges = append(snap.Gauges, m)
		} else {
			snap.Counters = append(snap.Counters, m)
		}
	}
	for name, h := range histograms {
		counts := make([]uint64, len(h.counts))
		copy(counts, h.counts)
		buckets := make([]float64, len(h.buckets))
		copy(buckets, h.buckets)
		snap.Histograms = append(snap.Histograms, Histogram{
			Name:    name,
			Buckets: buckets,
			Counts:  counts,
			Sum:     h.sum,
			Count:   h.count,
		})
	}
	countersMu.Unlock()

	sortMetrics(snap.Counters)
	sortMetrics(snap.Gauges)
	sort.Slice(snap.Histograms, func(i, j int) bool {
		return snap.Histograms[i].Name < snap.Histograms[j].Name
	})
	return snap
}

// sortMetrics orders a metric list by name, then canonical labels, so a
// snapshot is stable across reads.
func sortMetrics(m []Metric) {
	sort.Slice(m, func(i, j int) bool {
		if m[i].Name != m[j].Name {
			return m[i].Name < m[j].Name
		}
		return canonicalLabels(m[i].Labels) < canonicalLabels(m[j].Labels)
	})
}

// parseLabels inverts canonicalLabels.
func parseLabels(s string) Labels {
	out := Labels{}
	for _, pair := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(pair, "=")
		if !ok {
			continue
		}
		out[k] = v
	}
	return out
}
