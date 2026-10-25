package dto

import (
	"reflect"

	relaydto "eflow.local/new-api/relaykit/dto"
)

const MaxImageN = relaydto.MaxImageN

type ImageRequest = relaydto.ImageRequest
type ImageResponse = relaydto.ImageResponse
type ImageData = relaydto.ImageData

func GetJSONFieldNames(t reflect.Type) map[string]struct{} {
	return relaydto.GetJSONFieldNames(t)
}
