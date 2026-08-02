package service

import (
	"crypto/tls"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/QuantumNous/new-api/dto"
)

type shardedRoundTripper struct {
	shards   []http.RoundTripper
	n        uint32
	counters sync.Map
}

func newShardedRoundTripper(policy HTTPTransportPolicy, factory func() *http.Transport) *shardedRoundTripper {
	n := max(1, policy.Shards)
	shards := make([]http.RoundTripper, n)
	for i := range shards {
		transport := factory()
		transport.MaxIdleConns = max(1, transport.MaxIdleConns/n)
		transport.MaxIdleConnsPerHost = max(1, transport.MaxIdleConnsPerHost/n)
		shards[i] = transport
	}
	return &shardedRoundTripper{shards: shards, n: uint32(n)}
}

func originKey(req *http.Request) string {
	if req == nil || req.URL == nil {
		return ""
	}
	return strings.ToLower(req.URL.Scheme) + "://" + req.URL.Host
}

func (s *shardedRoundTripper) pickShard(origin string) uint32 {
	if s.n <= 1 {
		return 0
	}
	counterAny, _ := s.counters.LoadOrStore(origin, &atomic.Uint32{})
	return (counterAny.(*atomic.Uint32).Add(1) - 1) % s.n
}

func (s *shardedRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return s.shards[s.pickShard(originKey(req))].RoundTrip(req)
}

func (s *shardedRoundTripper) CloseIdleConnections() {
	for _, shard := range s.shards {
		if closer, ok := shard.(interface{ CloseIdleConnections() }); ok {
			closer.CloseIdleConnections()
		}
	}
}

func applyHTTPTransportPolicy(transport *http.Transport, policy HTTPTransportPolicy) {
	if policy.Protocol == dto.HTTPProtocolHTTP1 {
		transport.ForceAttemptHTTP2 = false
		transport.TLSNextProto = make(map[string]func(string, *tls.Conn) http.RoundTripper)
		if transport.TLSClientConfig != nil {
			config := transport.TLSClientConfig.Clone()
			config.NextProtos = nil
			transport.TLSClientConfig = config
		}
		return
	}
	transport.ForceAttemptHTTP2 = true
}
