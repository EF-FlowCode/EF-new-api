package setting

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// TicketAttachmentSettings is one immutable runtime configuration version.
// The derived allow lists are defensively copied by the getter.
type TicketAttachmentSettings struct {
	Enabled  bool
	MaxSize  int64
	MaxCount int

	AllowedExts     string
	AllowedMimes    string
	AllowedExtList  []string
	AllowedMimeList []string

	Storage      string
	LocalPath    string
	SignedURLTTL int64

	OSSEndpoint        string
	OSSBucket          string
	OSSRegion          string
	OSSAccessKeyID     string
	OSSAccessKeySecret string
	OSSCustomDomain    string

	S3Endpoint        string
	S3Bucket          string
	S3Region          string
	S3AccessKeyID     string
	S3AccessKeySecret string
	S3CustomDomain    string

	COSEndpoint     string
	COSBucket       string
	COSRegion       string
	COSSecretID     string
	COSSecretKey    string
	COSCustomDomain string
}

var (
	ticketAttachmentSettings   atomic.Pointer[TicketAttachmentSettings]
	ticketAttachmentSettingsMu sync.Mutex
)

func init() {
	publishTicketAttachmentSettings(newDefaultTicketAttachmentSettings())
}

func newDefaultTicketAttachmentSettings() TicketAttachmentSettings {
	settings := TicketAttachmentSettings{
		Enabled:      true,
		MaxSize:      50 * 1024 * 1024,
		MaxCount:     5,
		AllowedExts:  "jpg,jpeg,png,gif,webp,bmp,json,xml,txt,log,md,csv,pdf",
		AllowedMimes: "image/*,application/json,application/xml,text/*,application/pdf",
		Storage:      "local",
		LocalPath:    "data/ticket_attachments",
		SignedURLTTL: 900,
	}
	settings.rebuildAllowedLists()
	return settings
}

func GetTicketAttachmentSettings() TicketAttachmentSettings {
	current := ticketAttachmentSettings.Load()
	if current == nil {
		return newDefaultTicketAttachmentSettings()
	}
	return cloneTicketAttachmentSettings(*current)
}

func ValidateTicketAttachmentOptionValues(values map[string]string) error {
	_, err := applyTicketAttachmentOptionValues(GetTicketAttachmentSettings(), values)
	return err
}

func UpdateTicketAttachmentSettings(values map[string]string) error {
	ticketAttachmentSettingsMu.Lock()
	defer ticketAttachmentSettingsMu.Unlock()

	next, err := applyTicketAttachmentOptionValues(GetTicketAttachmentSettings(), values)
	if err != nil {
		return err
	}
	publishTicketAttachmentSettings(next)
	return nil
}

func applyTicketAttachmentOptionValues(current TicketAttachmentSettings, values map[string]string) (TicketAttachmentSettings, error) {
	next := cloneTicketAttachmentSettings(current)
	if value, ok := values["TicketAttachmentEnabled"]; ok {
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return current, fmt.Errorf("TicketAttachmentEnabled must be a boolean: %w", err)
		}
		next.Enabled = parsed
	}
	if value, ok := values["TicketAttachmentMaxSize"]; ok {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			return current, fmt.Errorf("TicketAttachmentMaxSize must be a positive integer")
		}
		next.MaxSize = parsed
	}
	if value, ok := values["TicketAttachmentMaxCount"]; ok {
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed <= 0 {
			return current, fmt.Errorf("TicketAttachmentMaxCount must be a positive integer")
		}
		next.MaxCount = parsed
	}
	if value, ok := values["TicketAttachmentAllowedExts"]; ok {
		next.AllowedExts = value
	}
	if value, ok := values["TicketAttachmentAllowedMimes"]; ok {
		next.AllowedMimes = value
	}
	if value, ok := values["TicketAttachmentStorage"]; ok {
		next.Storage = value
	}
	if value, ok := values["TicketAttachmentLocalPath"]; ok {
		next.LocalPath = value
	}
	if value, ok := values["TicketAttachmentSignedURLTTL"]; ok {
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil || parsed <= 0 {
			return current, fmt.Errorf("TicketAttachmentSignedURLTTL must be a positive integer")
		}
		next.SignedURLTTL = parsed
	}

	stringFields := map[string]*string{
		"TicketAttachmentOSSEndpoint":        &next.OSSEndpoint,
		"TicketAttachmentOSSBucket":          &next.OSSBucket,
		"TicketAttachmentOSSRegion":          &next.OSSRegion,
		"TicketAttachmentOSSAccessKeyId":     &next.OSSAccessKeyID,
		"TicketAttachmentOSSAccessKeySecret": &next.OSSAccessKeySecret,
		"TicketAttachmentOSSCustomDomain":    &next.OSSCustomDomain,
		"TicketAttachmentS3Endpoint":         &next.S3Endpoint,
		"TicketAttachmentS3Bucket":           &next.S3Bucket,
		"TicketAttachmentS3Region":           &next.S3Region,
		"TicketAttachmentS3AccessKeyId":      &next.S3AccessKeyID,
		"TicketAttachmentS3AccessKeySecret":  &next.S3AccessKeySecret,
		"TicketAttachmentS3CustomDomain":     &next.S3CustomDomain,
		"TicketAttachmentCOSEndpoint":        &next.COSEndpoint,
		"TicketAttachmentCOSBucket":          &next.COSBucket,
		"TicketAttachmentCOSRegion":          &next.COSRegion,
		"TicketAttachmentCOSSecretId":        &next.COSSecretID,
		"TicketAttachmentCOSSecretKey":       &next.COSSecretKey,
		"TicketAttachmentCOSCustomDomain":    &next.COSCustomDomain,
	}
	for key, target := range stringFields {
		if value, ok := values[key]; ok {
			*target = value
		}
	}
	next.rebuildAllowedLists()
	return next, nil
}

func (settings *TicketAttachmentSettings) rebuildAllowedLists() {
	settings.AllowedExtList = splitAttachmentList(settings.AllowedExts, true)
	settings.AllowedMimeList = splitAttachmentList(settings.AllowedMimes, false)
}

func splitAttachmentList(value string, trimDot bool) []string {
	raw := strings.Split(value, ",")
	out := make([]string, 0, len(raw))
	for _, item := range raw {
		item = strings.TrimSpace(strings.ToLower(item))
		if trimDot {
			item = strings.TrimPrefix(item, ".")
		}
		if item != "" {
			out = append(out, item)
		}
	}
	return out
}

func cloneTicketAttachmentSettings(settings TicketAttachmentSettings) TicketAttachmentSettings {
	settings.AllowedExtList = append([]string(nil), settings.AllowedExtList...)
	settings.AllowedMimeList = append([]string(nil), settings.AllowedMimeList...)
	return settings
}

func publishTicketAttachmentSettings(next TicketAttachmentSettings) {
	next = cloneTicketAttachmentSettings(next)
	ticketAttachmentSettings.Store(&next)
}

func (settings TicketAttachmentSettings) IsExtAllowed(ext string) bool {
	ext = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(ext)), ".")
	if ext == "" {
		return false
	}
	return slices.Contains(settings.AllowedExtList, ext)
}

func (settings TicketAttachmentSettings) IsMimeAllowed(mime string) bool {
	mime = strings.ToLower(strings.TrimSpace(mime))
	if idx := strings.Index(mime, ";"); idx >= 0 {
		mime = strings.TrimSpace(mime[:idx])
	}
	if mime == "" {
		return false
	}
	for _, allowed := range settings.AllowedMimeList {
		if allowed == mime {
			return true
		}
		if strings.HasSuffix(allowed, "/*") && strings.HasPrefix(mime, strings.TrimSuffix(allowed, "/*")+"/") {
			return true
		}
	}
	return false
}

func TicketAttachmentAllowedExtList() []string {
	return GetTicketAttachmentSettings().AllowedExtList
}

func TicketAttachmentAllowedMimeList() []string {
	return GetTicketAttachmentSettings().AllowedMimeList
}

func IsTicketAttachmentExtAllowed(ext string) bool {
	return GetTicketAttachmentSettings().IsExtAllowed(ext)
}

func IsTicketAttachmentMimeAllowed(mime string) bool {
	return GetTicketAttachmentSettings().IsMimeAllowed(mime)
}
