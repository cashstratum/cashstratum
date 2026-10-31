package events

import (
	"context"

	"cashstratumapi/cmd/cashtop/internal/rpc"
)

// Node is the read-only RPC surface used to enrich and re-check a find.
type Node interface {
	BlockHash(ctx context.Context, height int64) (string, error)
	BlockReward(ctx context.Context, hash string) (rpc.Reward, error)
	IsOrphaned(ctx context.Context, height int64, hash string) (bool, error)
}

// Enrich confirms the block hash with getblockhash and fills the reward. The
// node's hash wins when it disagrees with the log. Errors leave the block
// unchanged apart from fields already filled.
func Enrich(ctx context.Context, n Node, b BlockFound) (BlockFound, error) {
	h, err := n.BlockHash(ctx, b.Height)
	if err != nil {
		return b, err
	}
	b.Hash = h
	r, err := n.BlockReward(ctx, h)
	if err != nil {
		return b, err
	}
	b.Reward, b.HasReward = r.BCH, true
	return b, nil
}

// CheckOrphan reports whether a previously found block left the best chain.
func CheckOrphan(ctx context.Context, n Node, b BlockFound) (bool, error) {
	if b.Hash == "" {
		return false, nil
	}
	return n.IsOrphaned(ctx, b.Height, b.Hash)
}
