package billing_setting

import "testing"

func TestSmokeTestExprAllowsRequestParamPricing(t *testing.T) {
	expression := `param("metadata.resolution") == "1080p" ? tier("1080P @ $2.70/秒", param("metadata.duration") * 2.70 * 1000000) : param("metadata.resolution") == "720p" ? tier("720P @ $1.40/秒", param("metadata.duration") * 1.40 * 1000000) : tier("480P @ $0.75/秒", param("metadata.duration") * 0.75 * 1000000)`
	if err := SmokeTestExpr(expression); err != nil {
		t.Fatal(err)
	}
}

func TestSmokeTestExprStillValidatesParamExpressionSyntax(t *testing.T) {
	if err := SmokeTestExpr(`param("metadata.duration") *`); err == nil {
		t.Fatal("expected invalid expression to be rejected")
	}
}
