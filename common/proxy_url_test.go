package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseProxyURL(t *testing.T) {
	strict, err := ParseProxyURLStrict(" SOCKS5://user:pass@127.0.0.1 ")
	require.NoError(t, err)
	assert.Equal(t, "socks5://user:pass@127.0.0.1:1080", strict.String())

	_, err = ParseProxyURLStrict("http://proxy.example:8080/path?x=1#fragment")
	require.Error(t, err)

	runtimeURL, stripped, err := ParseProxyURLRuntime("http://proxy.example:8080/path?x=1#fragment")
	require.NoError(t, err)
	assert.True(t, stripped)
	assert.Equal(t, "http://proxy.example:8080", runtimeURL.String())

	for _, invalid := range []string{"proxy.example:8080", "ftp://proxy.example", "http://", "http://proxy.example:70000"} {
		_, err := ParseProxyURLStrict(invalid)
		assert.Error(t, err, invalid)
	}
}
