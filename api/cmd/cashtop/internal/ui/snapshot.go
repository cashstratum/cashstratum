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
	Hashrate1m float64
	Hashrate5m float64
	Hashrate1h float64
	Hashrate1d float64
	Diff       float64 // current share difficulty
	BestShare  float64
	Shares     int64
	LastShare  time.Time `json:",omitzero"`
	Connected  time.Time `json:",omitzero"` // zero when unknown
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
	Luck          float64   // effort at the find, percent of network difficulty
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
	Chain        string
	Tip          int
	TipHash      string
	TipTime      time.Time `json:",omitzero"`
	Difficulty   float64   `json:",omitempty"`
	NetHashrate  float64   // H/s
	MempoolTx    int
	MempoolBytes int64
	RPCLatency   time.Duration
	RPCOK        bool
	ZMQLast      time.Time `json:",omitzero"` // last hashblock notification
	TemplateTime time.Time `json:",omitzero"` // when the current template was built
	TemplateTxs  int

	// LastNetBlock is when the newest network block was seen, from the
	// latest of the tip header time, the hashblock notification and the pool
	// log; zero when unknown. PrevDifficulty is the difficulty of the block
	// before the tip (zero when unknown) and PrevTipTime that block's time.
	LastNetBlock   time.Time `json:",omitzero"`
	PrevDifficulty float64   `json:",omitempty"`
	PrevTipTime    time.Time `json:",omitzero"`
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
