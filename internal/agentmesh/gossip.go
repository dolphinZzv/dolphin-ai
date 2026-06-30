package agentmesh

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"time"

	"go.uber.org/zap"
)

// GossipType is the kind of a GossipMessage.
type GossipType string

const (
	GossipAnnounce GossipType = "announce" // I'm online
	GossipPing     GossipType = "ping"     // who's online?
	GossipAck      GossipType = "ack"      // I'm online + peers I know
	GossipBye     GossipType = "bye"       // graceful exit
)

// GossipMessage is the UDP gossip envelope.
type GossipMessage struct {
	Type      GossipType `json:"type"`
	Agent     AgentCard  `json:"agent"`
	Peers     []string   `json:"peers"`     // known peer addrs
	TTL       int        `json:"ttl"`       // remaining hops, starts at 3
	Timestamp time.Time  `json:"ts"`
}

// GossipConfig configures UDP gossip discovery.
type GossipConfig struct {
	Port            int           // UDP port, default 8101
	AnnounceInterval time.Duration // default 30s
	PeerTimeout     time.Duration // default 3x announce
	MaxHops         int           // default 3
	BindAddr        string        // default 0.0.0.0
}

// DefaultGossipConfig returns production defaults.
func DefaultGossipConfig() GossipConfig {
	return GossipConfig{
		Port:             8101,
		AnnounceInterval: 30 * time.Second,
		PeerTimeout:      90 * time.Second,
		MaxHops:          3,
	}
}

// Gossip drives UDP-based LAN agent discovery. It announces this agent's
// card, learns peers from incoming announces/pings, and rebroadcasts with a
// TTL to limit propagation. Conflict resolution on name clashes is delegated
// to the Registry's tie-breaker.
type Gossip struct {
	cfg     GossipConfig
	card    AgentCard
	registry *Registry
	logger   *zap.Logger

	conn      *net.UDPConn
	broadcast *net.UDPAddr // multicast/broadcast target

	mu       sync.Mutex
	seen     map[string]time.Time // peer addr → last seen
	knownPeers []string
	cancel   context.CancelFunc

	// dedup: cache recently-seen message ids (from+ts) to avoid loops.
	dedupMu sync.Mutex
	dedup   map[string]time.Time
}

// NewGossip builds a Gossip driver. The card's Addr is announced to peers.
func NewGossip(cfg GossipConfig, card AgentCard, reg *Registry, logger *zap.Logger) *Gossip {
	if logger == nil {
		logger = zap.NewNop()
	}
	if cfg.MaxHops <= 0 {
		cfg = DefaultGossipConfig()
	}
	return &Gossip{
		cfg:      cfg,
		card:     card,
		registry: reg,
		logger:   logger,
		seen:     map[string]time.Time{},
		dedup:    map[string]time.Time{},
	}
}

// Start binds the UDP socket and begins announcing + listening.
func (g *Gossip) Start(ctx context.Context) error {
	addr := g.cfg.BindAddr
	if addr == "" {
		addr = "0.0.0.0"
	}
	udpAddr, err := net.ResolveUDPAddr("udp4", addr+":"+itoa(g.cfg.Port))
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp4", udpAddr)
	if err != nil {
		return err
	}
	conn.SetReadBuffer(1 << 20)
	g.conn = conn

	// Broadcast target: the subnet broadcast on the same port. We use
	// 255.255.255.255 as a simple default; production deployments would scope
	// this to the subnet.
	bcAddr, _ := net.ResolveUDPAddr("udp4", "255.255.255.255:"+itoa(g.cfg.Port))
	g.broadcast = bcAddr

	cctx, cancel := context.WithCancel(ctx)
	g.cancel = cancel

	go g.readLoop(cctx)
	go g.announceLoop(cctx)
	go g.gcLoop(cctx)
	return nil
}

// Stop closes the socket and stops the loops.
func (g *Gossip) Stop() {
	if g.cancel != nil {
		g.cancel()
	}
	if g.conn != nil {
		// best-effort bye
		g.send(GossipMessage{Type: GossipBye, Agent: g.card, TTL: 1, Timestamp: time.Now()})
		_ = g.conn.Close()
	}
}

func (g *Gossip) announceLoop(ctx context.Context) {
	// Announce immediately on start, then on interval.
	g.announce()
	t := time.NewTicker(g.cfg.AnnounceInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			g.announce()
		}
	}
}

func (g *Gossip) announce() {
	msg := GossipMessage{
		Type:      GossipAnnounce,
		Agent:     g.card,
		Peers:     g.knownPeerAddrs(),
		TTL:       g.cfg.MaxHops,
		Timestamp: time.Now(),
	}
	g.send(msg)
}

func (g *Gossip) send(msg GossipMessage) {
	if g.conn == nil || g.broadcast == nil {
		return
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	_, _ = g.conn.WriteToUDP(data, g.broadcast)
}

func (g *Gossip) readLoop(ctx context.Context) {
	buf := make([]byte, 64*1024)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, src, err := g.conn.ReadFromUDP(buf)
		if err != nil {
			return
		}
		var msg GossipMessage
		if err := json.Unmarshal(buf[:n], &msg); err != nil {
			continue
		}
		if msg.Agent.Addr == g.card.Addr {
			continue // our own broadcast echoed back
		}
		g.handle(msg, src)
	}
}

func (g *Gossip) handle(msg GossipMessage, src *net.UDPAddr) {
	// dedup by (from addr, ts)
	dedupKey := msg.Agent.Addr + "|" + msg.Timestamp.Format(time.RFC3339Nano)
	g.dedupMu.Lock()
	if _, seen := g.dedup[dedupKey]; seen {
		g.dedupMu.Unlock()
		return
	}
	g.dedup[dedupKey] = time.Now()
	// trim dedup map
	if len(g.dedup) > 200 {
		for k, t := range g.dedup {
			if time.Since(t) > 5*time.Minute {
				delete(g.dedup, k)
			}
		}
	}
	g.dedupMu.Unlock()

	// Record peer as seen + register into the registry (tie-breaker handles
	// name conflicts).
	g.mu.Lock()
	g.seen[msg.Agent.Addr] = time.Now()
	g.mu.Unlock()

	switch msg.Type {
	case GossipAnnounce, GossipAck:
		card := msg.Agent
		if card.Status == "" {
			card.Status = AgentRunning
		}
		g.registry.Upsert(card)
		// learn peers from the message
		for _, p := range msg.Peers {
			g.rememberPeer(p)
		}
		// forward with TTL-1 (exclude source by not echoing to src)
		if msg.TTL > 1 {
			fwd := msg
			fwd.TTL = msg.TTL - 1
			g.send(fwd)
		}
	case GossipBye:
		g.registry.Deregister(msg.Agent.Name)
	}
}

// rememberPeer adds a known peer addr (for the Peers field we broadcast).
func (g *Gossip) rememberPeer(addr string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, p := range g.knownPeers {
		if p == addr {
			return
		}
	}
	g.knownPeers = append(g.knownPeers, addr)
}

func (g *Gossip) knownPeerAddrs() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]string, len(g.knownPeers))
	copy(out, g.knownPeers)
	return out
}

// gcLoop expires peers we haven't heard from in PeerTimeout.
func (g *Gossip) gcLoop(ctx context.Context) {
	t := time.NewTicker(g.cfg.AnnounceInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			g.gc()
		}
	}
}

func (g *Gossip) gc() {
	cutoff := time.Now().Add(-g.cfg.PeerTimeout)
	g.mu.Lock()
	expired := []string{}
	for addr, last := range g.seen {
		if last.Before(cutoff) {
			expired = append(expired, addr)
			delete(g.seen, addr)
		}
	}
	g.mu.Unlock()
	for _, addr := range expired {
		if c, ok := g.registry.GetByAddr(addr); ok {
			g.registry.Deregister(c.Name)
		}
	}
}
