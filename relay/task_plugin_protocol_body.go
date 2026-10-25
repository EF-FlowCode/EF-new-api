package relay

import (
	"bytes"
	"eflow.local/new-api/common"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"io"
	"mime"
	"mime/multipart"
)

func readPluginProtocolBody(c *gin.Context, files []any) (map[string]any, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, err
	}
	raw, err := storage.Bytes()
	if err != nil {
		return nil, err
	}
	if len(raw) > host.MaxPayloadBytes {
		return nil, errors.New("protocol body too large")
	}
	media, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil {
		return nil, err
	}
	if media == "application/json" {
		var value map[string]any
		if err := json.Unmarshal(raw, &value); err != nil || value == nil {
			return nil, errors.New("protocol requires a JSON object")
		}
		return map[string]any{"kind": "json", "value": value}, nil
	}
	if media != "multipart/form-data" {
		return nil, errors.New("unsupported public protocol body")
	}
	fields := map[string][]string{}
	reader := multipart.NewReader(bytes.NewReader(raw), params["boundary"])
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(part)
		part.Close()
		if err != nil {
			return nil, err
		}
		if part.FileName() == "" {
			fields[part.FormName()] = append(fields[part.FormName()], string(data))
		}
	}
	return map[string]any{"kind": "multipart", "fields": fields, "files": files}, nil
}
