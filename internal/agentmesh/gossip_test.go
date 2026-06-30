package agentmesh

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"
)

// 两个 agent 在局域网互相发现：A 广播 announce，B 收到后把 A 注册进 registry。
func TestGossip_TwoAgentsDiscover(t *testing.T) {
	regA := NewRegistry(nil, nil, nil)
	regB := NewRegistry(nil, nil, nil)

	// 各自绑一个独立 UDP 端口（用 :0 随机端口 + 手动取端口）
	addrA := freeUDPAddr(t)
	addrB := freeUDPAddr(t)

	cardA := AgentCard{Name: "seer", Addr: "127.0.0.1:8201", Capabilities: []string{"divine"}, Status: AgentRunning, ProtoVersion: 4}
	cardB := AgentCard{Name: "witch", Addr: "127.0.0.1:8202", Capabilities: []string{"save"}, Status: AgentRunning, ProtoVersion: 4}

	gA := newGossipOn(addrA, cardA, regA, t)
	gB := newGossipOn(addrB, cardB, regB, t)
	defer gA.Stop()
	defer gB.Stop()

	// 手动让 A 把 announce 直接发给 B 的端口（绕过广播，确保测试稳定）
	gA.sendTo(GossipMessage{
		Type: GossipAnnounce, Agent: cardA, TTL: 3, Timestamp: time.Now(),
	}, addrB)
	gB.sendTo(GossipMessage{
		Type: GossipAnnounce, Agent: cardB, TTL: 3, Timestamp: time.Now(),
	}, addrA)

	// 等待处理
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := regB.Get("seer"); ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, ok := regB.Get("seer"); !ok {
		t.Fatal("B 应发现 A (seer)")
	}
	if _, ok := regA.Get("witch"); !ok {
		t.Fatal("A 应发现 B (witch)")
	}
}

func TestGossip_ByeDeregisters(t *testing.T) {
	regA := NewRegistry(nil, nil, nil)
	regB := NewRegistry(nil, nil, nil)
	addrA := freeUDPAddr(t)
	addrB := freeUDPAddr(t)

	cardA := AgentCard{Name: "hunter", Addr: "127.0.0.1:8211", Status: AgentRunning, ProtoVersion: 4}
	cardB := AgentCard{Name: "mod", Addr: "127.0.0.1:8212", Status: AgentRunning, ProtoVersion: 4}

	gA := newGossipOn(addrA, cardA, regA, t)
	gB := newGossipOn(addrB, cardB, regB, t)
	defer gA.Stop()
	defer gB.Stop()

	// A 先 announce 给 B
	gA.sendTo(GossipMessage{Type: GossipAnnounce, Agent: cardA, TTL: 3, Timestamp: time.Now()}, addrB)
	waitFor(t, func() bool { _, ok := regB.Get("hunter"); return ok })
	// 然后 A bye 给 B → B 应移除 hunter
	gA.sendTo(GossipMessage{Type: GossipBye, Agent: cardA, TTL: 1, Timestamp: time.Now().Add(time.Second)}, addrB)
	waitFor(t, func() bool { _, ok := regB.Get("hunter"); return !ok })
}

// ── helpers ──

func freeUDPAddr(t *testing.T) *net.UDPAddr {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 0})
	if err != nil {
		t.Skipf("无法绑定 UDP 端口: %v", err)
	}
	addr := conn.LocalAddr().(*net.UDPAddr)
	_ = conn.Close()
	return addr
}

func newGossipOn(addr *net.UDPAddr, card AgentCard, reg *Registry, t *testing.T) *Gossip {
	t.Helper()
	conn, err := net.ListenUDP("udp4", addr)
	if err != nil {
		t.Skipf("监听 UDP 失败: %v", err)
	}
	g := &Gossip{
		cfg:      GossipConfig{Port: addr.Port, AnnounceInterval: 30 * time.Second, PeerTimeout: 90 * time.Second, MaxHops: 3},
		card:     card,
		registry: reg,
		logger:   nil,
		seen:     map[string]time.Time{},
		dedup:    map[string]time.Time{},
		conn:     conn,
	}
	g.cancel = func() {}
	go g.readLoop(context.Background())
	return g
}

// sendTo sends a gossip message to a specific UDP addr (test-only unicast).
func (g *Gossip) sendTo(msg GossipMessage, dst *net.UDPAddr) {
	data, _ := json.Marshal(msg)
	_, _ = g.conn.WriteToUDP(data, dst)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("条件超时未满足")
}
