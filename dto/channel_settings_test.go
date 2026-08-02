package dto

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChannelSettingsValidateHTTPTransport(t *testing.T) {
	for _, settings := range []ChannelSettings{
		{},
		{HTTPProtocol: "AUTO"},
		{HTTPProtocol: HTTPProtocolHTTP1, HTTP2ConnectionShards: 1},
		{HTTP2ConnectionShards: MaxHTTP2ConnectionShards},
	} {
		require.NoError(t, settings.ValidateHTTPTransport())
	}
	for _, settings := range []ChannelSettings{
		{HTTPProtocol: "http2"},
		{HTTP2ConnectionShards: -1},
		{HTTP2ConnectionShards: MaxHTTP2ConnectionShards + 1},
		{HTTPProtocol: HTTPProtocolHTTP1, HTTP2ConnectionShards: 2},
	} {
		require.Error(t, settings.ValidateHTTPTransport())
	}
}
