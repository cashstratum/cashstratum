package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"cashstratumapi/internal/ckconf"
	"cashstratumapi/internal/noderpc"
)

func endpointFor(t *testing.T, srv *httptest.Server) *ckconf.BtcdEndpoint {
	t.Helper()
	return &ckconf.BtcdEndpoint{URL: srv.URL, Auth: "user", Pass: "pass"}
}

func TestEnrichNetworkBlock_FailureYieldsUnenrichedButStillEmitted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := noderpc.New(endpointFor(t, srv))
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

	client := noderpc.New(endpointFor(t, srv))
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
