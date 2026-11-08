// Package pooltags names the mining pool behind a block from the text a
// miner leaves in the coinbase input. It has no dependencies.
package pooltags

import "strings"

// pool is one known pool: its display name and the lowercase text fragments
// that identify it in a coinbase.
type pool struct {
	name     string
	patterns []string
}

// pools lists the known pools, most specific first. Matching is a
// case-insensitive substring search, so the first pool with any matching
// fragment wins.
var pools = []pool{
	{"AntPool", []string{"antpool"}},
	{"F2Pool", []string{"f2pool"}},
	{"ViaBTC", []string{"viabtc", "viaminingaddr"}},
	{"BTC.com", []string{"btc.com", "btccom"}},
	{"Binance Pool", []string{"binance"}},
	{"Foundry USA", []string{"foundry"}},
	{"SlushPool", []string{"slush"}},
	{"Mining-Dutch", []string{"mining-dutch"}},
	{"SBI Crypto", []string{"sbicrypto", "sbi crypto"}},
	{"OKMiner", []string{"okminer"}},
	{"Poolin", []string{"poolin"}},
	{"EMCD", []string{"emcd"}},
	{"NiceHash", []string{"nicehash"}},
	{"SpiderPool", []string{"spiderpool"}},
	{"Rawpool", []string{"rawpool"}},
	{"SigmaPool", []string{"sigmapool"}},
	{"mmpool", []string{"mmpool"}},
	{"PTE Ltd", []string{"pte ltd"}},
	{"DPBCH", []string{"dpbch"}},
	{"MolePool", []string{"molepool"}},
	{"JCBCH", []string{"/jcbch/"}},
	{"LuckyFastBlocks", []string{"luckyfastblocks"}},
	{"Toomim Bros", []string{"toomim bros", "toom.im"}},
	{"TrustPool", []string{"trustpool"}},
	{"NTMinerPool", []string{"ntminerpool"}},
	{"ZSolo", []string{"zsolo"}},
	{"DogPool", []string{"dogpool"}},
	{"Kryptex", []string{"kryptex"}},
	{"CloverPool", []string{"cloverpool"}},
	{"ProHashing", []string{"prohashing"}},
	{"ZergPool", []string{"zergpool"}},
	{"SoloHash", []string{"solohash"}},
	{"SoloMining", []string{"solomining"}},
	{"ZPool", []string{"zpool"}},
	{"ZuluPool", []string{"zulupool"}},
	{"Minerium", []string{"minerium"}},
	{"Migodi", []string{"migodi"}},
	{"CyberPool", []string{"cyberpool"}},
	{"CKPool", []string{"ckpool"}},
}

// Match returns the pool whose pattern occurs in tag.
func Match(tag string) (string, bool) {
	low := strings.ToLower(tag)
	if low == "" {
		return "", false
	}
	for _, p := range pools {
		for _, pat := range p.patterns {
			if strings.Contains(low, pat) {
				return p.name, true
			}
		}
	}
	return "", false
}

// minRun is the shortest printable run kept by Decode.
const minRun = 4

// maxTag caps the decoded tag length.
const maxTag = 48

// Decode extracts the readable text from a coinbase input script given as
// hex: printable ASCII runs of at least four characters, joined by a space.
// Invalid hex yields an empty string.
func Decode(hexScript string) string {
	b := make([]byte, 0, len(hexScript)/2)
	for i := 0; i+1 < len(hexScript); i += 2 {
		hi, ok1 := nib(hexScript[i])
		lo, ok2 := nib(hexScript[i+1])
		if !ok1 || !ok2 {
			return ""
		}
		b = append(b, hi<<4|lo)
	}
	var runs []string
	start := -1
	flush := func(end int) {
		if start >= 0 && end-start >= minRun {
			runs = append(runs, strings.TrimSpace(string(b[start:end])))
		}
		start = -1
	}
	for i, c := range b {
		if c >= 0x20 && c < 0x7f {
			if start < 0 {
				start = i
			}
			continue
		}
		flush(i)
	}
	flush(len(b))
	out := strings.Join(runs, " ")
	if len(out) > maxTag {
		out = out[:maxTag]
	}
	return out
}

func nib(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}
