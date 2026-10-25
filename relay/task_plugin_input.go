package relay

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"

	"eflow.local/new-api/common"
	host "eflow.local/new-api/relay/channel/task/jsplugin"
	"eflow.local/new-api/service"
	"github.com/gin-gonic/gin"
)

// Read the public payload once. Provider conversion still belongs to scripts;
// uploads are bounded host resources, not arbitrary paths supplied to JS.
func readPluginInput(c *gin.Context, provider string) (map[string]any, map[string]host.File, []any, error) {
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		return nil, nil, nil, err
	}
	raw, err := storage.Bytes()
	if err != nil || len(raw) > host.MaxPayloadBytes {
		return nil, nil, nil, errors.New("plugin request exceeds 1 MiB")
	}
	body := map[string]any{}
	files := map[string]host.File{}
	var descriptions []any
	mediaType, params, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil {
		return nil, nil, nil, errors.New("invalid content type")
	}
	switch mediaType {
	case "application/json":
		if err := json.Unmarshal(raw, &body); err != nil || body == nil {
			return nil, nil, nil, errors.New("plugin submission requires a JSON object")
		}
	case "multipart/form-data":
		reader := multipart.NewReader(bytes.NewReader(raw), params["boundary"])
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, nil, nil, err
			}
			data, err := io.ReadAll(io.LimitReader(part, host.MaxPayloadBytes+1))
			part.Close()
			if err != nil || len(data) > host.MaxPayloadBytes {
				return nil, nil, nil, errors.New("invalid multipart part")
			}
			field := part.FormName()
			if part.FileName() == "" {
				if service.TaskPublicRoute(c) != nil {
					values, _ := body[field].([]string)
					body[field] = append(values, string(data))
					continue
				}
				if field == "metadata" || field == "images" {
					var value any
					if json.Unmarshal(data, &value) != nil {
						return nil, nil, nil, errors.New("invalid multipart JSON field")
					}
					body[field] = value
				} else {
					body[field] = string(data)
				}
				continue
			}
			if len(files) >= 8 {
				return nil, nil, nil, errors.New("too many plugin files")
			}
			ref := field + ":" + part.FileName()
			if _, duplicate := files[ref]; duplicate {
				return nil, nil, nil, errors.New("duplicate file reference")
			}
			contentType := http.DetectContentType(data)
			files[ref] = host.File{Name: part.FileName(), ContentType: contentType, Data: data}
			descriptions = append(descriptions, map[string]any{"field": field, "ref": ref, "filename": part.FileName(), "mimeType": contentType})
			if provider != "sora" && (field == "input_reference" || field == "image") {
				// Inline data is shared public input supported by image-capable
				// scripts. Sora forwards the original file via ctx.files instead.
				value := "data:" + contentType + ";base64," + base64.StdEncoding.EncodeToString(data)
				body["images"] = []any{value}
				body["image"] = value
			}
		}
	default:
		return nil, nil, nil, errors.New("plugin requires JSON or multipart")
	}
	// Sora forwards the native payload and original multipart files. Do not
	// inject canonical image/duration aliases as extra provider form fields.
	if provider == "sora" {
		return body, files, descriptions, nil
	}
	if ref, ok := body["input_reference"].(string); ok && strings.TrimSpace(ref) != "" {
		if _, exists := body["images"]; !exists {
			body["images"] = []any{ref}
		}
		if _, exists := body["image"]; !exists {
			body["image"] = ref
		}
	}
	if _, exists := body["duration"]; !exists {
		if seconds, exists := body["seconds"]; exists {
			body["duration"] = seconds
		}
	}
	return body, files, descriptions, nil
}
