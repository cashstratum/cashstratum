package main

import (
	"context"
	"net/http"
	"net/http/httptest"
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

	client := newNodeRPCClient(endpointFor(t, srv))
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

	client := newNodeRPCClient(endpointFor(t, srv))
	_, ok, err := client.GetBlockHeader(context.Background(), "deadbeef")
	if ok || err == nil {
		t.Fatalf("GetBlockHeader() = (ok=%v, err=%v), want (false, non-nil) on HTTP 500", ok, err)
	}
}

func TestGetBlockHeader_Timeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * enrichRPCTimeout)
		w.Write([]byte(`{"result":{"height":1,"time":1},"error":null}`))
	}))
	defer srv.Close()

	client := newNodeRPCClient(endpointFor(t, srv))
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

	client := newNodeRPCClient(&ckconf.BtcdEndpoint{URL: addr, Auth: "u", Pass: "p"})
	_, ok, err := client.GetBlockHeader(context.Background(), "deadbeef")
	if ok || err == nil {
		t.Fatalf("GetBlockHeader() = (ok=%v, err=%v), want (false, non-nil) on connection refused", ok, err)
	}
}

func TestEnrichNetworkBlock_FailureYieldsUnenrichedButStillEmitted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := newNodeRPCClient(endpointFor(t, srv))
	evt := newNetworkBlockEvent("00000000000000000157d95f4f88ba7e98ac6234edecfe8ec91388b1d1fa1744")

	got := enrichNetworkBlock(context.Background(), client, evt)

	if got.Enriched {
		t.Fatalf("Enriched = true, want false on RPC failure")
	}
	if got.Height != nil {
		t.Fatalf("Height = %v, want nil on RPC failure", *got.Height)
	}
	if got.BlockTime != nil {
		t.Fatalf("BlockTime = %v, want nil on RPC failure", *got.BlockTime)
	}
	if got.Hash != evt.Hash {
		t.Fatalf("Hash = %q, want %q -- event must still be produced", got.Hash, evt.Hash)
	}
}

func TestEnrichNetworkBlock_SuccessFillsHeightAndBlockTime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"result":{"height":968432,"time":1756598780},"error":null}`))
	}))
	defer srv.Close()

	client := newNodeRPCClient(endpointFor(t, srv))
	evt := newNetworkBlockEvent("00000000000000000157d95f4f88ba7e98ac6234edecfe8ec91388b1d1fa1744")

	got := enrichNetworkBlock(context.Background(), client, evt)

	if !got.Enriched {
		t.Fatalf("Enriched = false, want true on RPC success")
	}
	if got.Height == nil || *got.Height != 968432 {
		t.Fatalf("Height = %v, want 968432", got.Height)
	}
	if got.BlockTime == nil || *got.BlockTime != 1756598780 {
		t.Fatalf("BlockTime = %v, want 1756598780", got.BlockTime)
	}
}
