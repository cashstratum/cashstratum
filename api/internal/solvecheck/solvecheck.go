// Package solvecheck decides which block of the node's chain is this pool's
// own and settles a recorded solve against it. The API and cashtop share it,
// so both orphan, accept and correct a solve by the same rule: the coinbase of
// the block at the solve's height, never a bare comparison of hashes.
package solvecheck

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"

	"cashstratumapi/internal/ckconf"
	"cashstratumapi/internal/noderpc"
	"cashstratumapi/internal/poolfmt"
	"cashstratumapi/internal/poolindex"
)

// Owner says which coinbases are this pool's: the PROP pool pays its
// pooladdress at vout 1; with no pooladdress, the coinbase script carries the
// pool's btcsig. The zero value can decide nothing.
type Owner struct {
	PoolAddress string // canonical
	Sig         string
}

// Known reports whether the owner has anything to match a coinbase against.
func (o Owner) Known() bool { return o.PoolAddress != "" || o.Sig != "" }

// Owns reports whether cb is a block of this pool.
func (o Owner) Owns(cb noderpc.BlockCoinbase) bool {
	if o.PoolAddress != "" {
		return len(cb.Outputs) > 1 && poolfmt.CanonicalAddress(cb.Outputs[1].Address) == o.PoolAddress
	}
	raw, err := hex.DecodeString(cb.ScriptSig)
	return err == nil && o.Sig != "" && bytes.Contains(raw, []byte(o.Sig))
}

// LoadOwner reads pooladdress and btcsig from the pool conf.
func LoadOwner(confPath string) Owner {
	if confPath == "" {
		return Owner{}
	}
	o := Owner{PoolAddress: poolfmt.CanonicalAddress(ckconf.LoadPoolAddress(confPath))}
	if data, err := os.ReadFile(confPath); err == nil {
		var c struct {
			BtcSig string `json:"btcsig"`
		}
		if json.Unmarshal(data, &c) == nil {
			o.Sig = strings.TrimSpace(c.BtcSig)
		}
	}
	return o
}

// Node is the node access the rule needs; *noderpc.Client satisfies it.
type Node interface {
	GetBlockHash(ctx context.Context, height int64) (string, error)
	GetBlockCoinbase(ctx context.Context, hash string) (noderpc.BlockCoinbase, error)
}

// Verdict is the outcome of asking whose block sits at a height.
type Verdict int

const (
	// Unknown: the owner cannot be told, or the node did not answer.
	Unknown Verdict = iota
	// Ours: the chain block at the height is this pool's.
	Ours
	// NotOurs: the chain block at the height belongs to someone else.
	NotOurs
)

// Classify fetches the node's block at height and decides whose it is. It
// returns the block's hash and coinbase with the verdict.
func Classify(ctx context.Context, node Node, owner Owner, height int64) (Verdict, string, noderpc.BlockCoinbase) {
	if !owner.Known() {
		return Unknown, "", noderpc.BlockCoinbase{}
	}
	hash, err := node.GetBlockHash(ctx, height)
	if err != nil {
		return Unknown, "", noderpc.BlockCoinbase{}
	}
	cb, err := node.GetBlockCoinbase(ctx, hash)
	if err != nil {
		return Unknown, "", noderpc.BlockCoinbase{}
	}
	if owner.Owns(cb) {
		return Ours, hash, cb
	}
	return NotOurs, hash, cb
}

// Store is the part of the pool index a settlement writes to.
type Store interface {
	SetSolveStatus(height int64, status string) error
	VerifySolveHash(height int64, hash string) (bool, error)
}

// Settle applies the rule to one solve. Where the chain block at height is
// ours its hash is recorded and the solve is accepted; where it is someone
// else's and the chain tip has moved past height, the solve is orphaned and
// its recorded hash stays as it was. A node error or an unknown owner changes
// nothing. status is the solve's current status. The verdict, chain hash and
// coinbase are returned so a caller can fill what else it keeps.
func Settle(ctx context.Context, st Store, node Node, owner Owner, height, tip int64, status string) (Verdict, string, noderpc.BlockCoinbase, error) {
	v, hash, cb := Classify(ctx, node, owner, height)
	var err error
	switch {
	case v == Ours:
		if _, e := st.VerifySolveHash(height, hash); e != nil {
			err = e
		}
		if status != poolindex.StatusAccepted {
			if e := st.SetSolveStatus(height, poolindex.StatusAccepted); e != nil {
				err = e
			}
		}
	case v == NotOurs && tip > height && status != poolindex.StatusOrphaned:
		err = st.SetSolveStatus(height, poolindex.StatusOrphaned)
	}
	return v, hash, cb, err
}
