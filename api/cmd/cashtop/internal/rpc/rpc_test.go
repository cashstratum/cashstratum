package rpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"cashstratumapi/internal/ckconf"
	"cashstratumapi/internal/noderpc"
)

func server(t *testing.T, results map[string]string) (*Client, *int32) {
	t.Helper()
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		b, _ := io.ReadAll(r.Body)
		var req struct{ Method string }
		_ = json.Unmarshal(b, &req)
		res, ok := results[req.Method]
		if !ok {
			fmt.Fprint(w, `{"result":null,"error":{"code":-32601,"message":"nope"}}`)
			return
		}
		fmt.Fprintf(w, `{"result":%s,"error":null}`, res)
	}))
	t.Cleanup(srv.Close)
	return New(&ckconf.BtcdEndpoint{URL: srv.URL, Auth: "rpcuser-xyz", Pass: "s3cretpass"}), &n
}

func TestDisallowedRefusedWithoutIO(t *testing.T) {
	c, n := server(t, nil)
	for _, m := range []string{"getblocktemplate", "sendtoaddress", "walletpassphrase", "stop", "submitblock", "getwalletinfo", "getblockcount"} {
		if _, err := c.Call(context.Background(), m); err == nil {
			t.Fatalf("%s: want error", m)
		}
	}
	if *n != 0 {
		t.Fatalf("server saw %d requests, want 0", *n)
	}
}

func TestHelpers(t *testing.T) {
	const h = "000000000000000001aa"
	c, _ := server(t, map[string]string{
		"getblockchaininfo": `{"chain":"main","blocks":900,"headers":901,"bestblockhash":"` + h + `","difficulty":1.5,"verificationprogress":0.99,"initialblockdownload":false}`,
		"getmininginfo":     `{"blocks":900,"difficulty":2.5,"networkhashps":3e18,"pooledtx":7,"chain":"main"}`,
		"getmempoolinfo":    `{"size":12,"bytes":3400,"usage":9000}`,
		"getnetworkhashps":  `4.2e18`,
		"getbestblockhash":  `"` + h + `"`,
		"getblockhash":      `"` + h + `"`,
		"getblockheader":    `{"hash":"` + h + `","confirmations":3,"height":900,"time":1700000000}`,
		"getblock":          `{"tx":["aa01","bb02"]}`,
		"getrawtransaction": `{"vout":[{"value":3.12500000},{"value":0.00012345}]}`,
	})
	ctx := context.Background()
	if ci, err := c.ChainInfo(ctx); err != nil || ci.Blocks != 900 || ci.Chain != "main" || ci.BestBlockHash != h {
		t.Fatalf("chain %+v %v", ci, err)
	}
	if mi, err := c.MiningInfo(ctx); err != nil || mi.NetworkHashPS != 3e18 || mi.PooledTx != 7 {
		t.Fatalf("mining %+v %v", mi, err)
	}
	if mp, err := c.MempoolInfo(ctx); err != nil || mp.Size != 12 || mp.Bytes != 3400 {
		t.Fatalf("mempool %+v %v", mp, err)
	}
	if v, err := c.NetworkHashPS(ctx, 144); err != nil || v != 4.2e18 {
		t.Fatalf("nhps %v %v", v, err)
	}
	if v, err := c.BestBlockHash(ctx); err != nil || v != h {
		t.Fatalf("best %v %v", v, err)
	}
	if v, err := c.BlockHash(ctx, 900); err != nil || v != h {
		t.Fatalf("hash %v %v", v, err)
	}
	if hd, err := c.BlockHeader(ctx, h); err != nil || hd.Confirmations != 3 || hd.Height != 900 || hd.Time != 1700000000 {
		t.Fatalf("header %+v %v", hd, err)
	}
	if r, err := c.BlockReward(ctx, h); err != nil || r.Satoshis != 312512345 || r.BCH != 3.12512345 {
		t.Fatalf("reward %+v %v", r, err)
	}
	if o, err := c.IsOrphaned(ctx, 900, h); err != nil || o {
		t.Fatalf("orphaned %v %v", o, err)
	}
	if d, err := c.Latency(ctx); err != nil || d <= 0 {
		t.Fatalf("latency %v %v", d, err)
	}
}

func TestIsOrphaned(t *testing.T) {
	c, _ := server(t, map[string]string{
		"getblockheader": `{"confirmations":-1,"height":5,"time":1}`,
	})
	if o, err := c.IsOrphaned(context.Background(), 5, "aa"); err != nil || !o {
		t.Fatalf("want orphaned, got %v %v", o, err)
	}
	c2, _ := server(t, map[string]string{
		"getblockheader": `{"confirmations":2,"height":5,"time":1}`,
		"getblockhash":   `"bb"`,
	})
	if o, err := c2.IsOrphaned(context.Background(), 5, "aa"); err != nil || !o {
		t.Fatalf("want orphaned on hash mismatch, got %v %v", o, err)
	}
}

func TestCredentialsNeverLeak(t *testing.T) {
	c, _ := server(t, nil)
	_, err := c.BestBlockHash(context.Background())
	if err == nil {
		t.Fatal("want error")
	}
	for _, s := range []string{c.String(), fmt.Sprintf("%v %+v %#v", c, c, c), err.Error()} {
		if strings.Contains(s, "rpcuser-xyz") || strings.Contains(s, "s3cretpass") {
			t.Fatalf("credential leaked: %s", s)
		}
	}
	if got := c.redact(fmt.Errorf("dial s3cretpass failed")); strings.Contains(got.Error(), "s3cretpass") {
		t.Fatal("redact failed")
	}
}

func TestBlockRewardUsesTxidsAndCoinbaseOnly(t *testing.T) {
	const h = "00000000000000000abc"
	type call struct {
		Method string
		Params []any
	}
	var calls []call
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var req call
		_ = json.Unmarshal(b, &req)
		calls = append(calls, req)
		switch req.Method {
		case "getblock":
			fmt.Fprint(w, `{"result":{"tx":["cb01","tx02"]},"error":null}`)
		case "getrawtransaction":
			fmt.Fprint(w, `{"result":{"vout":[{"value":3.125},{"value":0.00012345}]},"error":null}`)
		}
	}))
	defer srv.Close()
	c := New(&ckconf.BtcdEndpoint{URL: srv.URL})
	r, err := c.BlockReward(context.Background(), h)
	if err != nil || r.Satoshis != 312512345 {
		t.Fatalf("reward %+v %v", r, err)
	}
	if len(calls) != 2 || calls[0].Method != "getblock" || calls[1].Method != "getrawtransaction" {
		t.Fatalf("calls %+v", calls)
	}
	if calls[0].Params[0] != h || calls[0].Params[1] != float64(1) {
		t.Fatalf("getblock params %v", calls[0].Params)
	}
	if p := calls[1].Params; p[0] != "cb01" || p[1] != true || p[2] != h {
		t.Fatalf("getrawtransaction params %v", p)
	}
}

func TestBlockRewardTruncatedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"result":{"tx":["`+strings.Repeat("ab", 1<<20)+`"]},"error":null}`)
	}))
	defer srv.Close()
	c := New(&ckconf.BtcdEndpoint{URL: srv.URL})
	_, err := c.BlockReward(context.Background(), "h")
	if !errors.Is(err, noderpc.ErrResponseTooLarge) {
		t.Fatalf("err = %v", err)
	}
}
