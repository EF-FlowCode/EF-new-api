package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"testing"

	"eflow.local/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOptionSaveErrorMarksCommittedButUnpublishedValues(t *testing.T) {
	gin.SetMode(gin.TestMode)
	decode := func(err error) map[string]any {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		writeOptionSaveError(c, err)
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		return body
	}

	saved := decode(fmt.Errorf("%w: %w", model.ErrOptionSavedNotPublished, errors.New("publish failed")))
	require.Equal(t, false, saved["success"])
	require.Equal(t, map[string]any{"saved": true}, saved["data"])
	require.NotContains(t, saved["message"], "publish failed")

	failed := decode(errors.New("invalid value"))
	require.Equal(t, false, failed["success"])
	require.Nil(t, failed["data"])
	require.Equal(t, "invalid value", failed["message"])
}
