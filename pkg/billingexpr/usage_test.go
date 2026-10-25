package billingexpr

import (
	"math"
	"testing"

	"eflow.local/new-api/pkg/jsplugin"
	"github.com/stretchr/testify/require"
)

func TestUsageExpressionContract(t *testing.T) {
	schema := map[string]jsplugin.UsageFieldSchema{"seconds": {Type: "number", Unit: "second"}, "resolution": {Enum: []string{"720p", "1080p"}}, "audio": {Type: "boolean"}}
	expression := `v2:u("seconds") * (u("resolution") == "1080p" ? 0.02 : 0.01) * (u("audio") ? 2 : 1)`
	require.NoError(t, ValidateUsageExpression(expression, schema))
	request := RequestInput{UsageSchema: schema, Usage: map[string]any{"seconds": float64(5), "resolution": "1080p", "audio": false}}
	amount, _, err := RunExprWithRequest(expression, TokenParams{}, request)
	require.NoError(t, err)
	require.Equal(t, 0.1, amount)
	request.Usage["seconds"] = float64(0)
	amount, _, err = RunExprWithRequest(expression, TokenParams{}, request)
	require.NoError(t, err)
	require.Zero(t, amount)
	for _, invalid := range []string{`v2:let u = 1; u`, `v2:$env.u("seconds")`, `v2:$env["u"]("seconds")`, `v2:false ? $env[param("fn")]("seconds") : 0`, `v2:let e = $env; e.u("seconds")`} {
		require.Error(t, ValidateUsageExpression(invalid, schema), invalid)
	}
	_, _, err = RunExpr(`v2:$env.p * 0.01`, TokenParams{P: 5})
	require.NoError(t, err, "static access to existing non-usage variables remains supported")
	for _, invalid := range []string{`u("seconds")`, `v1:u("seconds")`, `v2:u("")`, `v2:u(" seconds")`, `v2:u(param("key"))`, `v2:false ? u(param("key")) : 0`, `v2:let f = u; f("seconds")`, `v2:u("missing") * 0`, `v2:u("audio") * 0.1`, `v2:u("resolution") + 1`, `v2:false ? u("missing") : 0`} {
		t.Run(invalid, func(t *testing.T) { require.Error(t, ValidateUsageExpression(invalid, schema)) })
	}
	for _, value := range []any{nil, "5", -1.0, math.Inf(1), math.NaN(), float64(jsplugin.MaxTaskDurationSeconds + 1)} {
		request.Usage["seconds"] = value
		_, _, err := RunExprWithRequest(expression, TokenParams{}, request)
		require.Error(t, err)
	}
	request.Usage["seconds"] = float64(5)
	request.Usage["resolution"] = "invalid"
	_, _, err = RunExprWithRequest(expression, TokenParams{}, request)
	require.Error(t, err)
	_, _, err = RunExprWithRequest(`v2:false ? u("seconds") : 0`, TokenParams{}, RequestInput{UsageSchema: schema})
	require.ErrorContains(t, err, "missing")
	_, _, err = RunExprWithRequest(expression, TokenParams{}, RequestInput{Body: []byte(`{"seconds":5,"resolution":"1080p","audio":false}`)})
	require.Error(t, err, "client body must not provide usage facts")
}
