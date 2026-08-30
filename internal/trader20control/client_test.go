package trader20control

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const testAccount = "0x1111111111111111111111111111111111111111"

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) Do(r *http.Request) (*http.Response, error) { return f(r) }

func testClient(t *testing.T, responder func(string) string) (*Client, *[]string) {
	t.Helper()
	var seen []string
	doer := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(req.Body)
		raw := string(body)
		seen = append(seen, raw)
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(responder(raw))), Header: make(http.Header)}, nil
	})
	client, err := NewClient(Config{Account: testAccount, CandidateSHA: strings.Repeat("a", 40), PolicyHash: strings.Repeat("b", 64), HTTPClient: doer})
	if err != nil {
		t.Fatal(err)
	}
	client.now = func() time.Time { return time.UnixMilli(2_000).UTC() }
	return client, &seen
}

func TestClientExposesOnlyReadOnlyInfoOperationsAndRedactsAccount(t *testing.T) {
	client, seen := testClient(t, func(request string) string {
		switch {
		case strings.Contains(request, `"type":"clearinghouseState"`):
			return `{"marginSummary":{"accountValue":"42"},"withdrawable":"40","assetPositions":[{"position":{"coin":"BTC"}}]}`
		case strings.Contains(request, `"type":"frontendOpenOrders"`):
			return `[]`
		case strings.Contains(request, `"type":"userFillsByTime"`):
			return `[{"coin":"BTC","time":1500}]`
		case strings.Contains(request, `"type":"allMids"`):
			return `{"BTC":"100000"}`
		default:
			return `{}`
		}
	})
	ctx := context.Background()
	envelopes := []Envelope{client.Capabilities()}
	for _, call := range []func() (Envelope, error){
		func() (Envelope, error) { return client.Status(ctx) },
		func() (Envelope, error) { return client.Positions(ctx) },
		func() (Envelope, error) { return client.Orders(ctx) },
		func() (Envelope, error) { return client.History(ctx, time.UnixMilli(1000), time.UnixMilli(2000)) },
		func() (Envelope, error) { return client.RuntimeHealth(ctx) },
		func() (Envelope, error) { return client.ExplainBlocker(ctx) },
	} {
		env, err := call()
		if err != nil {
			t.Fatal(err)
		}
		envelopes = append(envelopes, env)
	}
	for _, env := range envelopes {
		raw := string(env.Data)
		if strings.Contains(strings.ToLower(raw), strings.ToLower(testAccount)) {
			t.Fatalf("operation %s leaked account", env.Operation)
		}
		if env.Protocol != ProtocolVersion {
			t.Fatalf("protocol = %q", env.Protocol)
		}
	}
	for _, request := range *seen {
		for _, forbidden := range []string{"exchange", "order", "cancel", "close", "transfer", "withdraw", "signature", "builder"} {
			if forbidden == "order" && strings.Contains(request, "frontendOpenOrders") {
				continue
			}
			if strings.Contains(strings.ToLower(request), forbidden) {
				t.Fatalf("request contains forbidden capability %q: %s", forbidden, request)
			}
		}
	}
}

func TestClientFailsClosedOnEndpointAccountAndHistoryRange(t *testing.T) {
	for _, endpoint := range []string{"https://api.hyperliquid.xyz/exchange", "https://evil.example/info", "http://api.hyperliquid.xyz/info", "https://api.hyperliquid.xyz/info?x=1"} {
		if _, err := NewClient(Config{InfoURL: endpoint, Account: testAccount}); err == nil {
			t.Fatalf("endpoint %q accepted", endpoint)
		}
	}
	if _, err := NewClient(Config{Account: "0xabc"}); err == nil {
		t.Fatal("malformed account accepted")
	}
	if _, err := NewClient(Config{Account: testAccount, CandidateSHA: "main"}); err == nil {
		t.Fatal("non-immutable candidate accepted")
	}
	if _, err := NewClient(Config{Account: testAccount, PolicyHash: "policy"}); err == nil {
		t.Fatal("non-hash policy identity accepted")
	}
	unbound, err := NewClient(Config{Account: testAccount})
	if err != nil {
		t.Fatal(err)
	}
	if env := unbound.Capabilities(); !env.Degraded || !strings.Contains(env.Reason, "identity unavailable") {
		t.Fatalf("unbound capabilities did not fail closed: %#v", env)
	}
	client, _ := testClient(t, func(string) string { return `[]` })
	if env, err := client.History(context.Background(), time.Now().Add(-8*24*time.Hour), time.Now()); err == nil || !env.Degraded {
		t.Fatal("oversized history range did not fail closed")
	}
}

func TestStaleProviderTimestampIsExplicitlyDegraded(t *testing.T) {
	client, _ := testClient(t, func(string) string { return `[{"time":1000}]` })
	client.cfg.MaxStaleness = 100 * time.Millisecond
	source := time.UnixMilli(1000).UTC()
	env := client.envelope("orders", json.RawMessage(`[]`), &source, false, "")
	if !env.Stale || !env.Degraded || env.Reason == "" {
		t.Fatalf("stale envelope = %#v", env)
	}
}
