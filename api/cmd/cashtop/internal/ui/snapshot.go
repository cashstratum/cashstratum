package ui

import "time"

// Snapshot is everything the views render. It is plain data: a Provider
// builds it from pool files and node RPC, the demo builds it from fixtures.
// Zero values mean "unknown" and render as a dash.
type Snapshot struct {
	Taken      time.Time
	Pool       PoolCard
	Workers    []WorkerRow
	Blocks     []BlockRow // newest first
	Feed       []FeedRow  // oldest first
	Network    NetworkInfo
	BestShares []BestShare // highest first, best share per worker since the last block
	Hashrates  []float64   // trailing pool hashrate series in H/s, oldest first
	Shares     []ShareDrop // recent submits from the current height's sharelogs, newest last

	// Round is the current round's per-miner breakdown.
	Round RoundInfo

	// Prop describes a pool that pays its miners pro rata from a shared
	// payroll output; its zero value is a solo pool.
	Prop PropInfo
}

// PropInfo is the pro-rata payout view of the round in progress. Every
// amount is in satoshis and an estimate until the block confirms; zero means
// the template's coinbase value is not known yet.
type PropInfo struct {
	On           bool
	Reward       int64 // coinbase value of the current template
	MinersPot    int64 // part of the reward shared pro rata
	MinersPotBP  int64 // the miners' pot in basis points of the coinbase; zero means unknown
	FinderBonus  int64 // finder output, which the estimates exclude
	MinersShown  int   // addresses with an estimate
	BelowMinimum int   // addresses under the eligibility threshold
}

// RoundInfo is the share accounting of the round in progress.
type RoundInfo struct {
	Since  time.Time    `json:",omitzero"` // when the round started; zero when unknown
	Height int          // height of the block template being worked on
	Total  float64      // sum of accepted share difficulty in the round
	Miners []RoundMiner // sorted by Diff, highest first
}

// RoundMiner is one miner's slice of the current round.
type RoundMiner struct {
	Address   string  // payout address of the miner
	Diff      float64 // accepted share difficulty this round
	Pct       float64 // share of RoundInfo.Total, percent
	OwnLuck   float64 // the miner's own effort this round, percent of network difficulty; valid when OwnLuckOK
	OwnLuckOK bool    // OwnLuck is known
	Best      float64 // best share difficulty this round
	BestOfNet float64 // Best as a percent of network difficulty

	// EstSats is the estimated pro-rata reward in satoshis and Eligible
	// whether the address clears the eligibility threshold; both are set
	// only on a pro-rata pool with a known reward.
	EstSats  int64
	Eligible bool
}

// MaxShareDrops caps Snapshot.Shares.
const MaxShareDrops = 500

// NearFraction is the share of the network difficulty from which a submit
// counts as a near miss.
const NearFraction = 0.01

// ShareDrop is one submitted share. Near marks sdiff >= NearFraction of the
// network difficulty; block-solving shares are always Near.
type ShareDrop struct {
	Time     time.Time
	Worker   string
	Diff     float64
	SDiff    float64
	Accepted bool
	Near     bool
}

// IsNear reports whether sdiff counts as a near miss at networkDiff.
func IsNear(sdiff, networkDiff float64) bool {
	return networkDiff > 0 && sdiff >= NearFraction*networkDiff
}

// PoolCard carries the headline pool statistics from the Pool status lines.
type PoolCard struct {
	Hashrate1m, Hashrate5m, Hashrate15m, Hashrate1h float64 // H/s
	Hashrate6h, Hashrate1d, Hashrate7d              float64 // H/s

	// Workers counts connected clients; Idle those with no share for over
	// 60 s; Disconnected the resumable sessions: dropped miners the daemon
	// keeps for 600 s so they can reconnect.
	Users, Workers, Idle, Disconnected int
	SPS1m, SPS5m                       float64

	// Accepted is the share difficulty accounted in the current round; with
	// NetworkDiff it gives the round effort. RoundPercent is the value the
	// pool itself reports for the round (zero when absent).
	Accepted     float64
	Rejected     float64
	NetworkDiff  float64
	RoundPercent float64
	BestShare    float64 // best share difficulty since the last block

	Height    int           // height of the block template being worked on
	LastBlock time.Time     `json:",omitzero"` // when the pool last found a block; zero if never
	Runtime   time.Duration // pool uptime as of Snapshot.Taken

	AllAliveSince      time.Time     `json:",omitzero"` // every worker has been live since then; zero when unknown
	LongestSession     time.Duration // longest current worker connection
	LongestSessionName string

	// Stale marks a pool that is not running: the figures above are the last
	// values it wrote, kept for history, and views must not present them as
	// live. LastActivity is the last sign of activity (zero when unknown); it
	// is filled whether or not the pool is stale. StaleReason says which
	// signals failed, joined by "; ".
	Stale        bool      `json:",omitempty"`
	LastActivity time.Time `json:",omitzero"`
	StaleReason  string    `json:",omitempty"`
}

// WorkerRow is one worker of the Workers table.
type WorkerRow struct {
	Name       string // "<user>.<rig>" as authorised
	Address    string
	UserAgent  string
	AgentStale bool `json:",omitempty"` // UserAgent is the last one known, not from the current window
	Hashrate1m float64
	Hashrate5m float64
	Hashrate1h float64
	Hashrate1d float64
	Diff       float64 // current share difficulty
	BestShare  float64
	Shares     int64
	LastShare  time.Time `json:",omitzero"`
	Connected  time.Time `json:",omitzero"` // zero when unknown

	// Disconnected is when the worker's last session dropped; zero while
	// connected or unknown. DropReason says why, when the pool logged it.
	Disconnected time.Time `json:",omitzero"`
	DropReason   string    `json:",omitempty"`
}

// BlockStatus is the chain state of a found block.
type BlockStatus string

const (
	BlockPending   BlockStatus = "pending"
	BlockConfirmed BlockStatus = "confirmed"
	BlockOrphaned  BlockStatus = "orphaned"
	BlockRejected  BlockStatus = "rejected"
)

// BlockRow is one entry of the block history.
type BlockRow struct {
	Height        int
	Hash          string
	Finder        string    // worker that found it
	Proto         string    // "v1" / "sv2"
	Time          time.Time `json:",omitzero"`
	Luck          float64   // pool effort at the find, percent of network difficulty
	LuckOK        bool      // Luck is known (a zero effort is not "unknown")
	FinderLuck    float64   // the finder's own effort at the find, percent of network difficulty; valid when FinderLuckOK
	FinderLuckOK  bool      // FinderLuck is known
	Status        BlockStatus
	Confirmations int
	Reward        int64 // satoshis; zero when unknown
}

// FeedRow is one rendered line of the event feed. Style is a short tag
// ("BLOCK", "POOL", "AUTH", ...), Colour an index into the theme palette.
type FeedRow struct {
	Time   time.Time
	Style  string
	Text   string
	Colour int
}

// NetworkInfo is the node and chain view.
type NetworkInfo struct {
	Chain         string
	Tip           int
	TipHash       string
	TipTime       time.Time `json:",omitzero"`
	Difficulty    float64   `json:",omitempty"`
	NetHashrate   float64   // H/s
	MempoolTx     int
	MempoolBytes  int64
	RPCLatency    time.Duration
	RPCOK         bool
	ZMQLast       time.Time `json:",omitzero"` // last hashblock notification
	ZMQConfigured bool      `json:",omitzero"` // a ZMQ endpoint is configured
	ZMQConnected  bool      `json:",omitzero"` // the subscriber is currently connected
	TemplateTime  time.Time `json:",omitzero"` // when the current template was built
	TemplateTxs   int

	// LastNetBlock is when the newest network block was seen, from the
	// latest of the tip header time, the hashblock notification and the pool
	// log; zero when unknown. PrevDifficulty is the difficulty of the block
	// before the tip (zero when unknown) and PrevTipTime that block's time.
	LastNetBlock   time.Time `json:",omitzero"`
	PrevDifficulty float64   `json:",omitempty"`
	PrevTipTime    time.Time `json:",omitzero"`

	// Recent lists the latest network blocks, newest first. DiffSeries is the
	// trailing network difficulty series, oldest first.
	Recent     []NetBlockRow
	DiffSeries []float64
}

// NetBlockRow is one recent network block.
type NetBlockRow struct {
	Height   int
	Hash     string
	Time     time.Time     `json:",omitzero"`
	Interval time.Duration // time since the previous block; zero when unknown
	Pool     string        // mining pool that found it; empty when unknown
	Tag      string        // coinbase tag; empty when unknown
	Ours     bool          // found by this pool
}

// BestShare is one row of the best-share leaderboard.
type BestShare struct {
	Worker string
	Diff   float64
	Time   time.Time
}

// DrillDown is the sharelog view of one height.
type DrillDown struct {
	Height      int
	Dir         string // sharelog directory name, %08x of the height
	NetworkDiff float64
	Jobs        []DrillJob
}

// DrillJob groups the shares submitted against one job id.
type DrillJob struct {
	ID     string
	Shares []DrillShare
}

// DrillShare is one sharelog line. Winning marks sdiff >= network difficulty.
type DrillShare struct {
	Time    time.Time
	Worker  string
	Agent   string
	Diff    float64
	SDiff   float64
	Result  bool
	Error   string
	Winning bool
}

// Provider supplies data to the UI. Snapshot is called from a command, off
// the render path; it should return the last good state rather than block.
type Provider interface {
	Snapshot() Snapshot
	DrillDown(height int) (DrillDown, error)
}

// SnapshotMsg replaces the current snapshot.
type SnapshotMsg struct{ Snapshot Snapshot }

// FeedRowsMsg appends rows to the feed.
type FeedRowsMsg struct{ Rows []FeedRow }

// BlockFoundMsg announces a block found by the pool. Overlays (the
// celebration) react to it; the model itself only prepends the block.
type BlockFoundMsg struct {
	Block  BlockRow
	Replay bool
}

// BlockUpdateMsg fills in fields (effort/luck, hash) that arrive a few log
// lines after BlockFoundMsg.
type BlockUpdateMsg struct {
	Height int
	Hash   string
	Luck   float64
}

// NetworkBlockMsg announces a new chain tip, whoever found it. Height, the
// difficulties and Gap are zero when unknown.
type NetworkBlockMsg struct {
	Height   int
	Time     time.Time
	Diff     float64
	PrevDiff float64
	Gap      time.Duration // time since the previous block
}

// TickMsg is the single animation tick (every 250 ms).
type TickMsg struct {
	Time  time.Time
	Frame int
}

// DrillDownMsg carries a finished drill-down lookup.
type DrillDownMsg struct {
	Height int
	Data   DrillDown
	Err    error
}
