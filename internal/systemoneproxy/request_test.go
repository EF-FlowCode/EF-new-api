package systemoneproxy

import (
	"encoding/json"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestSystemOneRequest(t *testing.T) {
	for _, state := range []string{`"refund"`, `{"text":"refund","n":9007199254740993}`, `[{"text":"refund"}]`} {
		for _, question := range []string{
			`{"type":"noul","instructions":{"ask":"refund?"}}`,
			`{"type":"noul","instructions":"refund?","criteria":{"true":"yes","false":"no"}}`,
			`{"type":"choice","instructions":["team?"],"criteria":{"billing":null,"other":"other"}}`,
			`{"type":"score","instructions":"urgency?","criteria":["low","high"]}`,
		} {
			var request SystemOneRequest
			raw := `{"model":"public-alias","state":` + state + `,"questions":{"q":` + question + `},"extension":{"exact":9007199254740993}}`
			require.NoError(t, json.Unmarshal([]byte(raw), &request))
			require.NoError(t, request.Validate())
			request.Model = "typesafe/jev-1.13"
			encoded, err := json.Marshal(request)
			require.NoError(t, err)
			require.Contains(t, string(encoded), `"model":"typesafe/jev-1.13"`)
			require.Contains(t, string(encoded), `9007199254740993`)
			require.NotContains(t, string(encoded), `"messages"`)
		}
	}
	for _, body := range []string{`null`, `{}`, `{"model":"m","state":null,"questions":{}}`, `{"model":"m","state":123,"questions":{}}`,
		`{"model":"m","state":"s","questions":{"q":{"type":"score","instructions":"s","criteria":["one"]}}}`,
		`{"model":"m","state":"s","questions":{"q":{"type":"choice","instructions":"s","criteria":{}}}}`,
		`{"model":"m","state":"s","questions":{"q":{"type":"score","instructions":"s","criteria":[null,"high"]}}}`,
		`{"model":"m","state":"s","questions":{"q":{"type":"noul","instructions":"s","criteria":{"true":null}}}}`,
		`{"model":"m","state":"s","questions":{"q":{"type":"noul","instructions":false}}}`,
		`{"model":"m","state":"s","questions":{"q":{"type":"chat","instructions":"s"}}}`,
		`{"model":"m","state":"s","stream":true,"questions":{"q":{"type":"noul","instructions":"s"}}}`} {
		var request SystemOneRequest
		require.NoError(t, json.Unmarshal([]byte(body), &request))
		require.Error(t, request.Validate(), body)
	}
}
