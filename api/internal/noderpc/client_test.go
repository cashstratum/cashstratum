package noderpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"cashstratumapi/internal/ckconf"
)

func endpointFor(t *testing.T, srv *httptest.Server) *ckconf.BtcdEndpoint {
	t.Helper()
	return &ckconf.BtcdEndpoint{URL: srv.URL, Auth: "user", Pass: "pass"}
}

func TestParseGetBlockHeaderResponse_Success(t *testing.T) {
	raw := []byte(`{"result":{"height":968432,"time":1756598780},"error":null,"id":"blocksniper-notifier"}`)
	got, err := parseGetBlockHeaderResponse(raw)
	if err != nil {
		t.Fatalf("parseGetBlockHeaderResponse() error = %v, want nil", err)
	}
	if got.Height != 968432 || got.Time != 1756598780 {
		t.Fatalf("parseGetBlockHeaderResponse() = %+v, want height=968432 time=1756598780", got)
	}
}

func TestParseGetBlockHeaderResponse_RPCError(t *testing.T) {
	raw := []byte(`{"result":null,"error":{"code":-5,"message":"Block not found"},"id":"blocksniper-notifier"}`)
	if _, err := parseGetBlockHeaderResponse(raw); err == nil {
		t.Fatalf("parseGetBlockHeaderResponse() error = nil, want non-nil for an RPC-level error")
	}
}

func TestGetBlockHeader_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"result":{"height":968432,"time":1756598780},"error":null,"id":"blocksniper-notifier"}`))
	}))
	defer srv.Close()

	client := New(endpointFor(t, srv))
	result, ok, err := client.GetBlockHeader(context.Background(), "00000000000000000157d95f4f88ba7e98ac6234edecfe8ec91388b1d1fa1744")
	if err != nil || !ok {
		t.Fatalf("GetBlockHeader() = (ok=%v, err=%v), want (true, nil)", ok, err)
	}
	if result.Height != 968432 || result.Time != 1756598780 {
		t.Fatalf("GetBlockHeader() result = %+v, want height=968432 time=1756598780", result)
	}
}

func TestGetBlockHeader_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := New(endpointFor(t, srv))
	_, ok, err := client.GetBlockHeader(context.Background(), "deadbeef")
	if ok || err == nil {
		t.Fatalf("GetBlockHeader() = (ok=%v, err=%v), want (false, non-nil) on HTTP 500", ok, err)
	}
}

func TestGetBlockHeader_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * DefaultTimeout)
		w.Write([]byte(`{"result":{"height":1,"time":1},"error":null}`))
	}))
	defer srv.Close()

	client := New(endpointFor(t, srv))
	_, ok, err := client.GetBlockHeader(context.Background(), "deadbeef")
	if ok || err == nil {
		t.Fatalf("GetBlockHeader() = (ok=%v, err=%v), want (false, non-nil) on timeout", ok, err)
	}
}

func TestGetBlockHeader_ConnectionRefused(t *testing.T) {
	// A closed server: connect refused, no listener on this address.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	addr := srv.URL
	srv.Close()

	client := New(&ckconf.BtcdEndpoint{URL: addr, Auth: "u", Pass: "p"})
	_, ok, err := client.GetBlockHeader(context.Background(), "deadbeef")
	if ok || err == nil {
		t.Fatalf("GetBlockHeader() = (ok=%v, err=%v), want (false, non-nil) on connection refused", ok, err)
	}
}

// TestCall_SendsMethodParamsAndAuth pins the request shape: JSON-RPC 1.0
// envelope, the default request id, basic auth from the endpoint, and the
// raw result handed back unchanged.
func TestCall_SendsMethodParamsAndAuth(t *testing.T) {
	var gotBody []byte
	var gotUser, gotPass string
	var gotAuthOK bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotUser, gotPass, gotAuthOK = r.BasicAuth()
		w.Write([]byte(`{"result":{"blocks":968432},"error":null,"id":"blocksniper-notifier"}`))
	}))
	defer srv.Close()

	client := New(endpointFor(t, srv))
	raw, err := client.Call(context.Background(), "getblockchaininfo", nil)
	if err != nil {
		t.Fatalf("Call() error = %v, want nil", err)
	}
	if string(raw) != `{"blocks":968432}` {
		t.Fatalf("Call() result = %s, want the raw result object", raw)
	}
	wantBody := `{"id":"blocksniper-notifier","jsonrpc":"1.0","method":"getblockchaininfo","params":[]}`
	if string(gotBody) != wantBody {
		t.Fatalf("request body = %s, want %s", gotBody, wantBody)
	}
	if !gotAuthOK || gotUser != "user" || gotPass != "pass" {
		t.Fatalf("basic auth = (%q, %q, %v), want (user, pass, true)", gotUser, gotPass, gotAuthOK)
	}
}

func TestGetBlockHeader_RequestBodyUnchanged(t *testing.T) {
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.Write([]byte(`{"result":{"height":1,"time":2},"error":null}`))
	}))
	defer srv.Close()

	if _, ok, err := New(endpointFor(t, srv)).GetBlockHeader(context.Background(), "deadbeef"); !ok || err != nil {
		t.Fatalf("GetBlockHeader() = (ok=%v, err=%v), want (true, nil)", ok, err)
	}
	wantBody := `{"id":"blocksniper-notifier","jsonrpc":"1.0","method":"getblockheader","params":["deadbeef"]}`
	if string(gotBody) != wantBody {
		t.Fatalf("request body = %s, want %s", gotBody, wantBody)
	}
}

func TestCall_NoEndpointConfigured(t *testing.T) {
	if _, err := New(nil).Call(context.Background(), "getblockcount", nil); err == nil {
		t.Fatalf("Call() error = nil, want non-nil with no endpoint configured")
	}
}

func TestParseResponse_NullResultIsAnError(t *testing.T) {
	if _, err := parseResponse([]byte(`{"result":null,"error":null}`)); err == nil {
		t.Fatalf("parseResponse() error = nil, want non-nil for a null result")
	}
}

func TestCallRejectsOversizedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"result":"`+strings.Repeat("a", maxResponseBytes)+`","error":null}`)
	}))
	defer srv.Close()
	c := New(&ckconf.BtcdEndpoint{URL: srv.URL})
	if _, err := c.Call(context.Background(), "getblock", nil); !errors.Is(err, ErrResponseTooLarge) {
		t.Fatalf("err = %v, want ErrResponseTooLarge", err)
	}
}

func TestParseBlockCoinbase(t *testing.T) {
	raw := []byte(`{"tx":[{"vout":[
		{"value":1.56495,"n":0,"scriptPubKey":{"addresses":["bitcoincash:qaaa"]}},
		{"value":1.56495001,"n":1,"scriptPubKey":{"address":"bitcoincash:qbbb"}},
		{"value":0,"n":2,"scriptPubKey":{}}]}]}`)
	cb, err := parseBlockCoinbase(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(cb.Outputs) != 3 || cb.Outputs[0].Sats != 156495000 || cb.Outputs[0].Address != "bitcoincash:qaaa" ||
		cb.Outputs[1].Sats != 156495001 || cb.Outputs[1].Address != "bitcoincash:qbbb" || cb.Outputs[2].Address != "" {
		t.Fatalf("outputs %+v", cb.Outputs)
	}
	if _, err := parseBlockCoinbase([]byte(`{"tx":[]}`)); err == nil {
		t.Fatal("empty block accepted")
	}
}

func TestCoinToSats(t *testing.T) {
	for in, want := range map[string]int64{"0": 0, "1e-05": 1000, "3.1299": 312990000, "0.00000001": 1} {
		if got, err := coinToSats(in); err != nil || got != want {
			t.Errorf("%s = %d, %v", in, got, err)
		}
	}
	if _, err := coinToSats("0.000000001"); err == nil {
		t.Error("sub-satoshi accepted")
	}
}
