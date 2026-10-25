package media

import (
	"errors"
	"strings"
	"sync"

	"context"
	"eflow.local/new-api/relaykit/types"
)

type MediaResolver struct {
	GetBase64Data        func(c context.Context, source types.FileSource, reason ...string) (string, string, error)
	DecodeBase64FileData func(base64String string) (string, string, error)
}

var (
	mediaResolverMu sync.RWMutex
	mediaResolver   MediaResolver
)

func SetMediaResolver(resolver MediaResolver) {
	mediaResolverMu.Lock()
	defer mediaResolverMu.Unlock()

	mediaResolver = resolver
}

func ResolveBase64Data(c context.Context, source types.FileSource, reason ...string) (string, string, error) {
	mediaResolverMu.RLock()
	resolver := mediaResolver.GetBase64Data
	mediaResolverMu.RUnlock()
	if resolver == nil {
		return "", "", errors.New("relayconvert media resolver is not configured")
	}
	return resolver(c, source, reason...)
}

func DecodeBase64FileData(base64String string) (string, string, error) {
	mediaResolverMu.RLock()
	resolver := mediaResolver.DecodeBase64FileData
	mediaResolverMu.RUnlock()
	if resolver == nil {
		return "", "", errors.New("relayconvert media resolver is not configured")
	}
	return resolver(base64String)
}

// DefaultResponsesImageDetail is used when the source protocol carries no detail
// hint. Sending the default explicitly also supports strict Responses validators
// that reject input_image parts without detail.
const DefaultResponsesImageDetail = "auto"

// ResponsesImageDetail preserves supported image detail levels and defaults
// missing or unrecognized hints to "auto".
func ResponsesImageDetail(detail string) string {
	switch normalized := strings.ToLower(strings.TrimSpace(detail)); normalized {
	case "low", "high", "auto", "original":
		return normalized
	default:
		return DefaultResponsesImageDetail
	}
}
