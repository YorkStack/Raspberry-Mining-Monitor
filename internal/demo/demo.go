// Package demo generates realistic, drifting telemetry so the dashboard can be
// developed and demonstrated without physical miners or an internet connection.
//
// Everything here is synthetic. The simulators are seeded, so a given seed
// always produces the same sequence, which keeps the tests deterministic.
package demo

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/bitcoin"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/miner"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/model"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/pool"
)

// Clock returns the current time. Tests inject a controllable one.
type Clock func() time.Time

func f(v float64) *float64 { return &v }
func u(v uint64) *uint64   { return &v }

// errOffline is what a simulated dropout returns.
var errOffline = errors.New("demo: miner unreachable")

// MinerConfig describes the device to simulate.
type MinerConfig struct {
	Name         string
	Model        string
	Firmware     string
	NominalTHs   float64
	NominalW     float64
	NominalTempC float64
	Fans         int

	// DropoutChance is the per-poll probability of a simulated failure.
	DropoutChance float64
}

// Miner is a synthetic AxeOS device.
type Miner struct {
	cfg   MinerConfig
	rnd   *rand.Rand
	clock Clock

	started time.Time
	last    time.Time

	// walk is a slow random walk in [-1, 1] driving hashrate and temperature,
	// so successive samples look correlated rather than like white noise.
	walk float64

	accepted uint64
	rejected uint64
	bestDiff float64

	offlineUntil time.Time
}

// NewMiner creates a simulated miner.
func NewMiner(cfg MinerConfig, seed int64, clock Clock) *Miner {
	if cfg.Fans <= 0 {
		cfg.Fans = 1
	}
	now := clock()
	return &Miner{
		cfg:      cfg,
		rnd:      rand.New(rand.NewSource(seed)),
		clock:    clock,
		started:  now,
		last:     now,
		bestDiff: 1e8 + float64(seed%7)*1e7,
	}
}

// Name is the configured display name.
func (m *Miner) Name() string { return m.cfg.Name }

// Fetch returns the next synthetic snapshot, or an error when the simulated
// device is in a dropout.
func (m *Miner) Fetch(_ context.Context) (miner.Snapshot, error) {
	now := m.clock()
	elapsed := now.Sub(m.last)
	if elapsed < 0 {
		elapsed = 0
	}
	m.last = now

	if now.Before(m.offlineUntil) {
		return miner.Snapshot{}, errOffline
	}
	if m.cfg.DropoutChance > 0 && m.rnd.Float64() < m.cfg.DropoutChance {
		// Stay down for a few polls so the UI's stale state is reachable.
		m.offlineUntil = now.Add(time.Duration(3+m.rnd.Intn(8)) * time.Second)
		return miner.Snapshot{}, errOffline
	}

	// Ornstein-Uhlenbeck style drift: pull toward zero, nudge randomly.
	m.walk = m.walk*0.97 + (m.rnd.Float64()*2-1)*0.12
	m.walk = math.Max(-1, math.Min(1, m.walk))

	hashrate := m.cfg.NominalTHs * (1 + m.walk*0.08)
	power := m.cfg.NominalW * (1 + m.walk*0.03)
	temp := m.cfg.NominalTempC + m.walk*4
	vrTemp := temp + 8 + m.walk

	// Shares arrive in rough proportion to hashrate.
	expected := hashrate * elapsed.Seconds() * 0.04
	newShares := uint64(expected)
	if m.rnd.Float64() < expected-float64(newShares) {
		newShares++
	}
	m.accepted += newShares
	if newShares > 0 && m.rnd.Float64() < 0.002 {
		m.rejected++
	}

	// Occasionally beat the previous best share.
	if m.rnd.Float64() < 0.01 {
		m.bestDiff *= 1 + m.rnd.Float64()*0.5
	}

	fans := make([]miner.Fan, 0, m.cfg.Fans)
	for i := 0; i < m.cfg.Fans; i++ {
		rpm := 4800 + m.walk*400 + float64(i)*120
		pct := 62 + m.walk*8
		fans = append(fans, miner.Fan{RPM: f(rpm), Percent: f(pct)})
	}

	s := miner.Snapshot{
		Name:            m.cfg.Name,
		Model:           m.cfg.Model,
		Firmware:        m.cfg.Firmware,
		Variant:         miner.VariantDemo,
		UptimeSeconds:   now.Sub(m.started).Seconds(),
		HashrateTHs:     hashrate,
		ExpectedHashTHs: f(m.cfg.NominalTHs),
		PowerW:          f(power),
		VoltageV:        f(5.02 + m.walk*0.03),
		CurrentA:        f(power / 5.0),
		FreqMHz:         f(625),
		CoreVoltageV:    f(1.15),
		ASICTempC:       f(temp),
		VRMTempC:        f(vrTemp),
		Fans:            fans,
		SharesAccepted:  u(m.accepted),
		SharesRejected:  u(m.rejected),
		BestDiff:        f(m.bestDiff),
		BestSessionDiff: f(m.bestDiff * 0.6),
		PoolURL:         "stratum+tcp://public-pool.io:3333",
		PoolUser:        "bc1qdemodemodemodemodemodemodemodemodemo42",
	}
	s.Source = model.Source{}
	s.Succeed(now)
	return s, nil
}

// Bitcoin is a synthetic network-data provider.
type Bitcoin struct {
	rnd   *rand.Rand
	clock Clock

	started   time.Time
	baseHeigh uint32

	lastBlock  time.Time
	height     uint32
	difficulty float64
}

// NewBitcoin creates a simulated Bitcoin data provider seeded near the real
// chain state observed while the design was written.
func NewBitcoin(seed int64, clock Clock) *Bitcoin {
	now := clock()
	return &Bitcoin{
		rnd:        rand.New(rand.NewSource(seed)),
		clock:      clock,
		started:    now,
		baseHeigh:  963_692,
		height:     963_692,
		lastBlock:  now.Add(-222 * time.Second),
		difficulty: 125_807_076_547_197.5,
	}
}

// SourceKind reports that this is synthetic data, which drives the dashboard
// badge.
func (b *Bitcoin) SourceKind() bitcoin.SourceKind { return bitcoin.SourceDemo }

// Network returns the simulated chain state.
func (b *Bitcoin) Network(_ context.Context) (bitcoin.Snapshot, error) {
	now := b.clock()

	// Blocks arrive as a Poisson process with a ten-minute mean.
	for {
		gap := time.Duration(-math.Log(1-b.rnd.Float64()) * float64(10*time.Minute))
		next := b.lastBlock.Add(gap)
		if next.After(now) {
			break
		}
		b.lastBlock = next
		b.height++
	}

	s := bitcoin.Snapshot{
		Kind:                   bitcoin.SourceDemo,
		Height:                 b.height,
		Difficulty:             b.difficulty,
		NetworkHashrateHs:      907_782_986_431_433_900_000,
		PriceEUR:               65_515,
		LastBlockTime:          b.lastBlock,
		NextRetargetHeight:     965_664,
		NextRetargetChangePct:  -2.45,
		NextRetargetETASeconds: 1_215_308,
	}
	s.Succeed(now)
	return s, nil
}

// Pool is a synthetic solo-pool adapter modelled on Public Pool, including its
// missing metrics.
type Pool struct {
	rnd   *rand.Rand
	clock Clock

	workers  []string
	bestDiff float64
	best     map[string]float64
}

// NewPool creates a simulated pool adapter with one worker per miner name.
func NewPool(workers []string, seed int64, clock Clock) *Pool {
	p := &Pool{
		rnd:      rand.New(rand.NewSource(seed)),
		clock:    clock,
		workers:  workers,
		bestDiff: 1.2e10,
		best:     make(map[string]float64, len(workers)),
	}
	for i, w := range workers {
		p.best[w] = 4e9 + float64(i)*1.5e9
	}
	return p
}

// Name identifies the adapter.
func (p *Pool) Name() string { return "publicpool-demo" }

// Capabilities mirrors what Public Pool can actually supply, so the demo does
// not promise metrics the real adapter will not have.
func (p *Pool) Capabilities() pool.Capabilities {
	return pool.Caps(
		pool.FieldHashrate,
		pool.FieldBestShare,
		pool.FieldBestEver,
		pool.FieldLastShare,
		pool.FieldActiveWorkers,
		pool.FieldBlocksFound,
	)
}

// Fetch returns the simulated pool-side view. It implements pool.Fetcher; the
// telemetry is unused because the demo generates its own numbers.
func (p *Pool) Fetch(_ context.Context, _ map[string]miner.Snapshot) (pool.Snapshot, error) {
	now := p.clock()

	if p.rnd.Float64() < 0.02 {
		p.bestDiff *= 1 + p.rnd.Float64()*0.3
	}

	workers := make([]pool.Worker, 0, len(p.workers))
	for _, name := range p.workers {
		if p.rnd.Float64() < 0.02 {
			p.best[name] *= 1 + p.rnd.Float64()*0.4
		}
		b := p.best[name]
		workers = append(workers, pool.Worker{
			Name:           name,
			MinerName:      name,
			BestDifficulty: f(b),
			LastSeen:       now.Add(-time.Duration(p.rnd.Intn(45)) * time.Second),
		})
	}

	last := now.Add(-time.Duration(p.rnd.Intn(30)) * time.Second)
	active := len(workers)
	s := pool.Snapshot{
		Provider:        "publicpool",
		Caps:            p.Capabilities(),
		Workers:         workers,
		WorkersCount:    len(workers),
		ActiveWorkers:   &active,
		BestDifficulty:  f(p.bestDiff),
		BestEver:        f(p.bestDiff),
		PoolHashrateTHs: f(118_000 + p.rnd.Float64()*4000),
		PoolMiners:      intPtr(2400 + p.rnd.Intn(200)),
		BlocksFound:     intPtr(3),
		LastShare:       &last,
	}
	s.Succeed(now)
	return s, nil
}

func intPtr(v int) *int { return &v }
