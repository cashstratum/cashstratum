package solvecheck

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"cashstratumapi/internal/noderpc"
)

func writeConf(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "cashstratum.conf")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const (
	feeNow = "bitcoincash:qptme3v2yuyecr0rjhj6jqe3jx97jq3nngtkg9refg"
	feeOld = "bitcoincash:qr5pp33g5ul4l8chfg56au2kpvanfmqryqfgv00utu"
)

func coinbase(sig, vout1 string) noderpc.BlockCoinbase {
	return noderpc.BlockCoinbase{
		ScriptSig: hex.EncodeToString([]byte("\x03\x97\xe6\x0e" + sig)),
		Outputs:   []noderpc.BlockOutput{{Address: "bitcoincash:qrfc9tvc5dx4ywgmnxj5yy95xdrg7ra9eupjeshhfq"}, {Address: vout1}},
	}
}

// A solo conf sets pooladdress as its fee address. A block found while the
// operator used another fee address is still the pool's by its btcsig.
func TestLoadOwnerSoloUsesBtcsig(t *testing.T) {
	o := LoadOwner(writeConf(t, `{"btcsig": "/pool/ ", "pooladdress": "`+feeNow+`", "poolfee": 2.0}`))
	if o.PoolAddress != "" || o.Sig != "/pool/" {
		t.Fatalf("owner = %+v, want btcsig only", o)
	}
	if !o.Owns(coinbase("/pool/", feeOld)) {
		t.Fatal("solo block paying an earlier fee address was not ours")
	}
	if o.Owns(coinbase("/other/", feeNow)) {
		t.Fatal("a competitor's block paying our fee address was ours")
	}
}

func TestLoadOwnerPropUsesPoolAddress(t *testing.T) {
	o := LoadOwner(writeConf(t, `{"btcsig": "/pool/", "pooladdress": "`+feeNow+`", "poolfee": 50}`))
	if o.PoolAddress == "" {
		t.Fatalf("owner = %+v, want the pooladdress", o)
	}
	if !o.Owns(coinbase("/other/", feeNow)) || o.Owns(coinbase("/pool/", feeOld)) {
		t.Fatal("PROP ownership must follow the payroll output, not the tag")
	}
}

func TestLoadOwnerMissingConf(t *testing.T) {
	if LoadOwner(filepath.Join(t.TempDir(), "none.conf")).Known() || LoadOwner("").Known() {
		t.Fatal("a missing conf must decide nothing")
	}
}
