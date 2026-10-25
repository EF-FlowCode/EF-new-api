package middleware

import (
	"io"
	"net/http/httptest"
	"strings"
	"testing"

	"eflow.local/new-api/common"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestBodyStorageCleanupAfterPanic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var storage common.BodyStorage
	router := gin.New()
	router.Use(gin.RecoveryWithWriter(io.Discard), BodyStorageCleanup())
	router.POST("/", func(c *gin.Context) {
		var err error
		storage, err = common.GetBodyStorage(c)
		require.NoError(t, err)
		panic("test downstream failure")
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest("POST", "/", strings.NewReader("payload")))
	require.Equal(t, 500, rec.Code)
	_, err := storage.Bytes()
	require.ErrorIs(t, err, common.ErrStorageClosed)
}
