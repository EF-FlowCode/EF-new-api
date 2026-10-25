package service

import (
	"net/http"
	"testing"

	"eflow.local/new-api/dto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPTransportPolicyAndCache(t *testing.T) {
	InitHttpClient()
	t.Cleanup(ResetProxyClientCache)

	defaultClient, err := GetHttpClientWithProxySettings("", dto.ChannelSettings{})
	require.NoError(t, err)
	assert.Same(t, GetHttpClient(), defaultClient)

	http1Client, err := GetHttpClientWithProxySettings("", dto.ChannelSettings{HTTPProtocol: dto.HTTPProtocolHTTP1})
	require.NoError(t, err)
	assert.NotSame(t, defaultClient, http1Client)
	transport := http1Client.Transport.(*http.Transport)
	assert.False(t, transport.ForceAttemptHTTP2)
	assert.NotNil(t, transport.TLSNextProto)

	shardedClient, err := GetHttpClientWithProxySettings("", dto.ChannelSettings{HTTP2ConnectionShards: 2})
	require.NoError(t, err)
	sharded := shardedClient.Transport.(*shardedRoundTripper)
	assert.Equal(t, []uint32{0, 1, 0}, []uint32{sharded.pickShard("a"), sharded.pickShard("a"), sharded.pickShard("a")})

	ResetProxyClientCache()
	assert.Same(t, defaultClient, GetHttpClient())
}

func TestProxyClientCanonicalCacheAndInvalidation(t *testing.T) {
	InitHttpClient()
	t.Cleanup(ResetProxyClientCache)

	client, err := GetHttpClientWithProxy("http://proxy.example:8080")
	require.NoError(t, err)
	alias, err := GetHttpClientWithProxy("http://proxy.example:8080/")
	require.NoError(t, err)
	assert.Same(t, client, alias)

	http1, err := GetHttpClientWithProxySettings("http://proxy.example:8080", dto.ChannelSettings{HTTPProtocol: dto.HTTPProtocolHTTP1})
	require.NoError(t, err)
	assert.NotSame(t, client, http1)

	InvalidateProxyClient("http://proxy.example:8080/")
	recreated, err := GetHttpClientWithProxy("http://proxy.example:8080")
	require.NoError(t, err)
	assert.NotSame(t, client, recreated)
	recreatedHTTP1, err := GetHttpClientWithProxySettings("http://proxy.example:8080", dto.ChannelSettings{HTTPProtocol: dto.HTTPProtocolHTTP1})
	require.NoError(t, err)
	assert.NotSame(t, http1, recreatedHTTP1)
}
