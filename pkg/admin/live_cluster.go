package admin

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultLiveClusterChannel       = "goframe:admin:live:v1"
	defaultLiveClusterPublishBuffer = 256
)

type liveClusterRelayConfig struct {
	RedisURL string
	Channel  string
	NodeID   string
	Token    string
	Logger   *slog.Logger
	OnEvent  func(sourceNode string, event liveEventEnvelope)
}

type liveClusterWireEvent struct {
	Version     string            `json:"version"`
	NodeID      string            `json:"node_id"`
	Token       string            `json:"token,omitempty"`
	PublishedAt string            `json:"published_at"`
	Event       liveEventEnvelope `json:"event"`
}

type liveClusterRelay struct {
	nodeID  string
	channel string
	token   string
	logger  *slog.Logger

	client *redis.Client
	pubsub *redis.PubSub

	sendCh    chan liveEventEnvelope
	cancel    context.CancelFunc
	done      chan struct{}
	closeOnce sync.Once
	wg        sync.WaitGroup

	connected atomic.Bool
	published atomic.Uint64
	dropped   atomic.Uint64
	received  atomic.Uint64
	ignored   atomic.Uint64
}

func (p *Panel) publishLiveClusterEvent(event liveEventEnvelope) {
	if p == nil {
		return
	}
	p.liveClusterMu.RLock()
	relay := p.liveCluster
	p.liveClusterMu.RUnlock()
	if relay == nil {
		return
	}
	if strings.TrimSpace(event.NodeID) == "" {
		event.NodeID = p.liveNodeID()
	}
	p.live.nodes.touch(event.NodeID, time.Now().UTC())
	relay.publish(event)
}

func (p *Panel) liveClusterSnapshot() liveClusterSnapshot {
	if p == nil {
		return liveClusterSnapshot{Enabled: false, Reason: "admin panel is not initialized"}
	}
	if !p.config.LiveClusterEnabled {
		return liveClusterSnapshot{Enabled: false, Reason: "admin live cluster relay is disabled"}
	}
	p.liveClusterMu.RLock()
	relay := p.liveCluster
	p.liveClusterMu.RUnlock()
	if relay == nil {
		redisURL := strings.TrimSpace(p.config.LiveClusterRedisURL)
		if redisURL == "" {
			redisURL = strings.TrimSpace(p.config.RedisURL)
		}
		reason := "relay is not initialized"
		if redisURL == "" {
			reason = "redis url is not configured"
		}
		return liveClusterSnapshot{
			Enabled:   true,
			Connected: false,
			NodeID:    p.liveNodeID(),
			Reason:    reason,
		}
	}
	return relay.snapshot()
}

func (p *Panel) ingestClusterLiveEvent(sourceNode string, event liveEventEnvelope) {
	if p == nil || p.live == nil {
		return
	}
	nodeID := strings.TrimSpace(sourceNode)
	if nodeID == "" {
		nodeID = strings.TrimSpace(event.NodeID)
	}
	if nodeID == "" {
		return
	}

	if p.logger != nil {
		p.logger.Info("cluster event ingested", "node", nodeID, "type", event.Type)
	}

	ts := parseRFC3339(event.Timestamp)
	if ts.IsZero() {
		ts = time.Now().UTC()
	}
	p.live.nodes.touch(nodeID, ts)

	switch strings.TrimSpace(event.Type) {
	case "node.heartbeat":
		return
	case "http.request":
		if event.Request == nil {
			return
		}
		req := *event.Request
		if strings.TrimSpace(req.NodeID) == "" {
			req.NodeID = nodeID
		}
		if shouldExcludeLivePath(req.Path, p.liveExcludePatterns()) {
			return
		}
		p.live.requests.push(req)
		p.live.bus.publish(liveEventEnvelope{
			NodeID:    req.NodeID,
			Type:      "http.request",
			Timestamp: req.Timestamp,
			Request:   &req,
		})
	case "db.query":
		if event.SQL == nil {
			return
		}
		sql := *event.SQL
		if strings.TrimSpace(sql.NodeID) == "" {
			sql.NodeID = nodeID
		}
		p.live.sql.push(sql)
		p.live.bus.publish(liveEventEnvelope{
			NodeID:    sql.NodeID,
			Type:      "db.query",
			Timestamp: sql.Timestamp,
			SQL:       &sql,
		})
	case "session.activity":
		if event.Session == nil {
			return
		}
		session := *event.Session
		if strings.TrimSpace(session.NodeID) == "" {
			session.NodeID = nodeID
		}
		key := clusterSessionStoreKey(session)
		if key != "" {
			p.live.sessions.upsert(key, session)
		}
		p.live.bus.publish(liveEventEnvelope{
			NodeID:    session.NodeID,
			Type:      "session.activity",
			Timestamp: session.LastSeenAt,
			Session:   &session,
		})
	default:
		// Unknown event type: ignore to keep forward compatibility.
	}
}

func clusterSessionStoreKey(row liveSessionActivity) string {
	if token := strings.TrimSpace(row.SessionToken); token != "" {
		return "session:" + token
	}
	short := strings.TrimSpace(row.TokenShort)
	node := strings.TrimSpace(row.NodeID)
	if short != "" {
		if node == "" {
			return "session-short:" + short
		}
		return "session-short:" + short + ":" + node
	}
	userID := strings.TrimSpace(row.UserID)
	if userID != "" {
		if node == "" {
			return "user:" + userID
		}
		return "user:" + userID + ":" + node
	}
	return ""
}

func newLiveClusterRelay(cfg liveClusterRelayConfig) (*liveClusterRelay, error) {
	redisURL := strings.TrimSpace(cfg.RedisURL)
	if redisURL == "" {
		return nil, fmt.Errorf("admin live cluster relay requires redis url")
	}
	channel := strings.TrimSpace(cfg.Channel)
	if channel == "" {
		channel = defaultLiveClusterChannel
	}
	nodeID := strings.TrimSpace(cfg.NodeID)
	if nodeID == "" {
		return nil, fmt.Errorf("admin live cluster relay requires node id")
	}

	options, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	client := redis.NewClient(options)

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer pingCancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("ping redis: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	pubsub := client.Subscribe(ctx, channel)
	receiveCtx, receiveCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_, err = pubsub.Receive(receiveCtx)
	receiveCancel()
	if err != nil {
		cancel()
		_ = pubsub.Close()
		_ = client.Close()
		return nil, fmt.Errorf("subscribe redis channel %q: %w", channel, err)
	}

	relay := &liveClusterRelay{
		nodeID:  nodeID,
		channel: channel,
		token:   strings.TrimSpace(cfg.Token),
		logger:  cfg.Logger,
		client:  client,
		pubsub:  pubsub,
		sendCh:  make(chan liveEventEnvelope, defaultLiveClusterPublishBuffer),
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	relay.connected.Store(true)

	relay.wg.Add(2)
	go relay.runPublisher(ctx)
	go relay.runSubscriber(ctx, cfg.OnEvent)
	go relay.awaitShutdown()

	return relay, nil
}

func (r *liveClusterRelay) awaitShutdown() {
	r.wg.Wait()
	r.connected.Store(false)
	if r.pubsub != nil {
		_ = r.pubsub.Close()
	}
	if r.client != nil {
		_ = r.client.Close()
	}
	close(r.done)
}

func (r *liveClusterRelay) runPublisher(ctx context.Context) {
	defer r.wg.Done()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Periodic heartbeat to keep node visible in topology even if idle
			event := liveEventEnvelope{
				Type:      "node.heartbeat",
				Timestamp: time.Now().UTC().Format(time.RFC3339),
			}
			if r.logger != nil {
				r.logger.Info("publishing node heartbeat", "node", r.nodeID)
			}
			r.publish(event)

		case event := <-r.sendCh:
			wire := liveClusterWireEvent{
				Version:     "1",
				NodeID:      r.nodeID,
				Token:       r.token,
				PublishedAt: time.Now().UTC().Format(time.RFC3339),
				Event:       event,
			}
			data, err := json.Marshal(wire)
			if err != nil {
				r.dropped.Add(1)
				continue
			}

			pubCtx, pubCancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
			err = r.client.Publish(pubCtx, r.channel, data).Err()
			pubCancel()
			if err != nil {
				r.dropped.Add(1)
				if r.logger != nil {
					r.logger.Debug("admin live cluster publish failed", "channel", r.channel, "error", err)
				}
				continue
			}
			r.published.Add(1)
		}
	}
}

func (r *liveClusterRelay) runSubscriber(ctx context.Context, onEvent func(sourceNode string, event liveEventEnvelope)) {
	defer r.wg.Done()
	ch := r.pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if msg == nil {
				r.ignored.Add(1)
				continue
			}
			var payload liveClusterWireEvent
			if err := json.Unmarshal([]byte(msg.Payload), &payload); err != nil {
				r.ignored.Add(1)
				continue
			}
			sourceNode := strings.TrimSpace(payload.NodeID)
			if sourceNode == "" {
				r.ignored.Add(1)
				continue
			}
			if sourceNode == r.nodeID {
				r.ignored.Add(1)
				continue
			}
			if r.token != "" {
				token := strings.TrimSpace(payload.Token)
				if subtle.ConstantTimeCompare([]byte(token), []byte(r.token)) != 1 {
					r.ignored.Add(1)
					continue
				}
			}
			r.received.Add(1)
			if onEvent != nil {
				onEvent(sourceNode, payload.Event)
			}
		}
	}
}

func (r *liveClusterRelay) publish(event liveEventEnvelope) {
	if r == nil {
		return
	}
	select {
	case <-r.done:
		r.dropped.Add(1)
		return
	default:
	}

	select {
	case r.sendCh <- event:
	default:
		r.dropped.Add(1)
	}
}

func (r *liveClusterRelay) close(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.closeOnce.Do(func() {
		r.cancel()
	})
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *liveClusterRelay) snapshot() liveClusterSnapshot {
	if r == nil {
		return liveClusterSnapshot{}
	}
	return liveClusterSnapshot{
		Enabled:   true,
		Connected: r.connected.Load(),
		NodeID:    r.nodeID,
		Channel:   r.channel,
		Published: r.published.Load(),
		Dropped:   r.dropped.Load(),
		Received:  r.received.Load(),
		Ignored:   r.ignored.Load(),
	}
}
