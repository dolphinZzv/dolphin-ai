package resource

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// LinuxSampler collects resource stats from /proc.
type LinuxSampler struct {
	mu         sync.Mutex
	closed     bool

	// CPU: previous sample values for delta calculation
	prevCPUIdle   uint64
	prevCPUTotal  uint64

	// Network: previous sample values
	prevNetTime time.Time
	prevNetRx   uint64
	prevNetTx   uint64
}

// NewSampler creates a new Linux sampler.
func NewSampler() *LinuxSampler {
	s := &LinuxSampler{}
	s.initCPU()
	s.initNet()
	return s
}

func (s *LinuxSampler) initCPU() {
	idle, total, err := readCPUStats()
	if err == nil {
		s.prevCPUIdle = idle
		s.prevCPUTotal = total
	}
}

func (s *LinuxSampler) initNet() {
	rx, tx, err := readNetStats()
	if err == nil {
		s.prevNetRx = rx
		s.prevNetTx = tx
		s.prevNetTime = time.Now()
	}
}

// CPU returns the CPU usage percentage since the last call.
func (s *LinuxSampler) CPU() (float64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	idle, total, err := readCPUStats()
	if err != nil {
		return 0, err
	}

	deltaIdle := idle - s.prevCPUIdle
	deltaTotal := total - s.prevCPUIdle
	s.prevCPUIdle = idle
	s.prevCPUTotal = total

	if deltaTotal == 0 {
		return 0, nil
	}

	pct := 100.0 * (1.0 - float64(deltaIdle)/float64(deltaTotal))
	return math.Round(pct*10) / 10, nil
}

// Memory returns the memory usage percentage.
func (s *LinuxSampler) Memory() (float64, error) {
	total, avail, err := readMemStats()
	if err != nil {
		return 0, err
	}
	if total == 0 {
		return 0, nil
	}
	used := total - avail
	pct := 100.0 * float64(used) / float64(total)
	return math.Round(pct*10) / 10, nil
}

// Disk returns the disk usage percentage for the given path.
func (s *LinuxSampler) Disk(path string) (float64, error) {
	if path == "" {
		path = "/"
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", path, err)
	}
	total := stat.Blocks * uint64(stat.Bsize)
	free := stat.Bavail * uint64(stat.Bsize)
	if total == 0 {
		return 0, nil
	}
	used := total - free
	pct := 100.0 * float64(used) / float64(total)
	return math.Round(pct*10) / 10, nil
}

// Network returns the receive and transmit bytes/sec rates since the last call.
func (s *LinuxSampler) Network() (rxRate, txRate float64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	rx, tx, err := readNetStats()
	if err != nil {
		return 0, 0, err
	}

	elapsed := now.Sub(s.prevNetTime).Seconds()
	if elapsed <= 0 {
		elapsed = 1
	}

	rxRate = float64(rx-s.prevNetRx) / elapsed
	txRate = float64(tx-s.prevNetTx) / elapsed

	if rxRate < 0 {
		rxRate = 0
	}
	if txRate < 0 {
		txRate = 0
	}

	s.prevNetRx = rx
	s.prevNetTx = tx
	s.prevNetTime = now

	return math.Round(rxRate*100) / 100, math.Round(txRate*100) / 100, nil
}

// Close marks the sampler as closed.
func (s *LinuxSampler) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
}

// readCPUStats parses /proc/stat and returns (idle_ticks, total_ticks).
func readCPUStats() (uint64, uint64, error) {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0, fmt.Errorf("read /proc/stat: %w", err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		// fields: cpu user nice system idle iowait irq softirq steal
		var total uint64
		for i := 1; i < len(fields); i++ {
			v, err := strconv.ParseUint(fields[i], 10, 64)
			if err != nil {
				continue
			}
			total += v
		}
		idle, _ := strconv.ParseUint(fields[4], 10, 64) // idle is field 4
		return idle, total, nil
	}
	return 0, 0, fmt.Errorf("cpu line not found in /proc/stat")
}

// readMemStats parses /proc/meminfo and returns (total_bytes, available_bytes).
func readMemStats() (uint64, uint64, error) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, fmt.Errorf("read /proc/meminfo: %w", err)
	}

	var total, available uint64
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			total = parseMemInfoValue(line)
		case strings.HasPrefix(line, "MemAvailable:"):
			available = parseMemInfoValue(line)
		}
	}
	if total == 0 {
		return 0, 0, fmt.Errorf("MemTotal not found in /proc/meminfo")
	}
	if available == 0 {
		// Fallback: MemFree + Cached + SReclaimable (rough approximation)
		available = total // worst case
	}
	return total * 1024, available * 1024, nil // values are in kB
}

// parseMemInfoValue extracts the numeric value (in kB) from a /proc/meminfo line.
func parseMemInfoValue(line string) uint64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	v, _ := strconv.ParseUint(fields[1], 10, 64)
	return v
}

// readNetStats parses /proc/net/dev and returns total (rx_bytes, tx_bytes)
// across all non-loopback interfaces.
func readNetStats() (uint64, uint64, error) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0, fmt.Errorf("read /proc/net/dev: %w", err)
	}

	var rxTotal, txTotal uint64
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		// Skip headers and loopback
		if !strings.Contains(line, ":") {
			continue
		}
		iface := strings.Split(line, ":")[0]
		if strings.TrimSpace(iface) == "lo" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		// After ":", rx_bytes is field 0, tx_bytes is field 8
		rx, _ := strconv.ParseUint(fields[1], 10, 64)
		tx, _ := strconv.ParseUint(fields[9], 10, 64)
		rxTotal += rx
		txTotal += tx
	}
	return rxTotal, txTotal, nil
}
