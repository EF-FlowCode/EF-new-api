package common

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func pageInfoFromQuery(t *testing.T, query string) *PageInfo {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/api/test?"+query, nil)
	return GetPageQuery(c)
}

func TestGetPageQueryNormalizesNonPositivePageSize(t *testing.T) {
	page := pageInfoFromQuery(t, "p=2&page_size=-1")
	require.Equal(t, 2, page.Page)
	require.Equal(t, ItemsPerPage, page.PageSize)

	page = pageInfoFromQuery(t, "page_size=0&ps=-1&size=-2")
	require.Equal(t, ItemsPerPage, page.PageSize)
}

func TestGetPageQueryCapsLargePageSize(t *testing.T) {
	page := pageInfoFromQuery(t, "page_size=1000")
	require.Equal(t, 100, page.PageSize)
}
