package apitypes

import "encoding/json"

type TailResponse struct {
	Lines     []string `json:"lines"`
	Timestamp int64    `json:"timestamp"`
	Error     string   `json:"error,omitempty"`
}

type GrepResponse struct {
	Lines     []string `json:"lines"`
	Pattern   string   `json:"pattern"`
	Timestamp int64    `json:"timestamp"`
}

type FindBlockResponse struct {
	Found     bool     `json:"found"`
	Lines     []string `json:"lines"`
	Height    string   `json:"height"`
	Timestamp int64    `json:"timestamp"`
	Error     string   `json:"error,omitempty"`
}

type UserFileResponse struct {
	Username string      `json:"username"`
	Content  interface{} `json:"content"`
	Raw      bool        `json:"raw,omitempty"`
	Error    string      `json:"error,omitempty"`
}

type StatsResponse struct {
	Hashrate     *string `json:"hashrate"`
	Workers      int     `json:"workers"`
	Users        int     `json:"users"`
	Transactions int     `json:"transactions"`
	BlockHeight  *int    `json:"block_height"`
	// Node is the BCH node's own view of the chain, nil when the node could
	// not be reached (or no RPC endpoint was configured) -- in which case
	// BlockHeight falls back to the log-scrape below and may be stale.
	Node      *NodeInfo `json:"node,omitempty"`
	Timestamp int64     `json:"timestamp"`
	Error     string    `json:"error,omitempty"`
}

// NodeInfo mirrors the fields of the node's getblockchaininfo that a pool
// dashboard actually needs -- the same set the host's `bch-status` helper
// prints, so the two can be reconciled by eye during an incident.
type NodeInfo struct {
	Chain       string  `json:"chain"`
	Blocks      int     `json:"blocks"`
	Headers     int     `json:"headers"`
	ProgressPct float64 `json:"progress_pct"`
	Difficulty  float64 `json:"difficulty"`
	IBD         bool    `json:"ibd"`
	// Synced is the single boolean a caller should gate on: the node has
	// caught up to every header it knows about and is out of initial block
	// download. A node one block behind its own headers is still syncing,
	// however healthy `blocks` looks on its own.
	Synced bool `json:"synced"`
}

type UserLogResponse struct {
	Lines     []string `json:"lines"`
	Exists    bool     `json:"exists"`
	Username  string   `json:"username,omitempty"`
	Timestamp int64    `json:"timestamp"`
}

type HealthResponse struct {
	Status    string `json:"status"`
	Timestamp int64  `json:"timestamp"`
	LogExists bool   `json:"log_exists"`
	LogSize   int64  `json:"log_size"`
	Uptime    int64  `json:"uptime"`
	Version   string `json:"version,omitempty"`
}

type MetricsResponse struct {
	Uptime           int64 `json:"uptime"`
	RequestsTotal    int64 `json:"requests_total"`
	ErrorsTotal      int64 `json:"errors_total"`
	RateLimitEntries int   `json:"rate_limit_entries"`
	CacheEntries     int   `json:"cache_entries"`
	Timestamp        int64 `json:"timestamp"`
}

type CoinbaseOutput struct {
	Value    int64  `json:"value"`     // satoshis
	ValueBCH string `json:"value_bch"` // formatted BCH
	Address  string `json:"address"`
	Type     string `json:"type"` // "miner", "pool_fee" or "op_return"
}

type CoinbaseResponse struct {
	Username        string           `json:"username"`
	CoinbaseHex     string           `json:"coinbase_hex"`
	CoinbaseMessage string           `json:"coinbase_message"`
	Outputs         []CoinbaseOutput `json:"outputs"`
	TotalValue      int64            `json:"total_value"`     // satoshis
	TotalValueBCH   string           `json:"total_value_bch"` // formatted BCH
	BlockHeight     *int             `json:"block_height"`
	NetworkBits     string           `json:"network_bits"`
	Timestamp       int64            `json:"timestamp"`
	Error           string           `json:"error,omitempty"`
}

// ShareRecord mirrors one line of a ckpool `.sharelog` file. Field names are
// ckpool's own (src/stratifier.c share_json()), kept verbatim rather than
// Go-cased so callers can match the on-disk log directly. RejectReason and
// Error are nullable in the log (present as JSON null, or simply absent on
// an accepted share), hence the pointer / RawMessage types.
type ShareRecord struct {
	WorkinfoID   int64           `json:"workinfoid"`
	ClientID     int64           `json:"clientid"`
	Enonce1      string          `json:"enonce1"`
	Nonce2       string          `json:"nonce2"`
	Nonce        string          `json:"nonce"`
	Ntime        string          `json:"ntime"`
	Diff         float64         `json:"diff"`
	SDiff        float64         `json:"sdiff"`
	Hash         string          `json:"hash"`
	Result       bool            `json:"result"`
	RejectReason *string         `json:"reject-reason"`
	Error        json.RawMessage `json:"error"`
	Errn         int             `json:"errn"`
	CreateDate   string          `json:"createdate"`
	CreateBy     string          `json:"createby"`
	CreateCode   string          `json:"createcode"`
	CreateInet   string          `json:"createinet"`
	Workername   string          `json:"workername"`
	// Worker is the worker segment of Workername, derived at read time --
	// the sharelog itself carries no such field, so it is always recomputed
	// and never trusted from the line. See shortWorkerName.
	Worker   string `json:"worker"`
	Username string `json:"username"`
	Address  string `json:"address"`
	Agent    string `json:"agent"`
	// VersionMask is the BIP310 version-rolling mask the miner submitted
	// (params[5] of mining.submit), rendered by ckpool as 8 hex digits and
	// "00000000" when the client sent none. Appended to the sharelog record
	// last, so it is absent from every line written before that change --
	// it decodes as "" on those, which is why it is not a pointer.
	VersionMask string `json:"version_mask"`
}

// SharesResponse is /shares' payload.
type SharesResponse struct {
	Shares []ShareRecord `json:"shares"`
	// Height is the current highest known sharelog height dir (decimal,
	// decoded from its %08x name), nil when no height dir exists yet (pool
	// has not found work since it started). A cursor'd forward read can span
	// a block rollover, so this is NOT a guarantee every returned share
	// belongs to this height -- it is "where the pool is now", matching
	// StatsResponse.BlockHeight's convention.
	Height *int `json:"height"`
	// NextCursor resumes a forward read exactly where this response left
	// off: same position (no data loss) on an empty poll, one position past
	// the last line consumed otherwise. Opaque to callers -- see
	// encodeCursor/decodeCursor.
	NextCursor string `json:"next_cursor,omitempty"`
	Timestamp  int64  `json:"timestamp"`
	Error      string `json:"error,omitempty"`
}
