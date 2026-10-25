package jsonutil

import (
	"fmt"

	kitutil "eflow.local/new-api/relaykit/relayconvert/kitutil"
)

func ToJSONString(v any) string {
	bytes, err := kitutil.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(bytes)
}
