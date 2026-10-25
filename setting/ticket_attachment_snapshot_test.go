package setting

import (
	"fmt"
	"strconv"
	"sync"
	"testing"
)

func restoreTicketAttachmentSettings(t *testing.T) {
	t.Helper()
	before := GetTicketAttachmentSettings()
	t.Cleanup(func() {
		_ = UpdateTicketAttachmentSettings(ticketAttachmentValues(before))
	})
}

func ticketAttachmentValues(settings TicketAttachmentSettings) map[string]string {
	return map[string]string{
		"TicketAttachmentEnabled":            strconv.FormatBool(settings.Enabled),
		"TicketAttachmentMaxSize":            strconv.FormatInt(settings.MaxSize, 10),
		"TicketAttachmentMaxCount":           strconv.Itoa(settings.MaxCount),
		"TicketAttachmentAllowedExts":        settings.AllowedExts,
		"TicketAttachmentAllowedMimes":       settings.AllowedMimes,
		"TicketAttachmentStorage":            settings.Storage,
		"TicketAttachmentLocalPath":          settings.LocalPath,
		"TicketAttachmentSignedURLTTL":       strconv.FormatInt(settings.SignedURLTTL, 10),
		"TicketAttachmentOSSEndpoint":        settings.OSSEndpoint,
		"TicketAttachmentOSSBucket":          settings.OSSBucket,
		"TicketAttachmentOSSRegion":          settings.OSSRegion,
		"TicketAttachmentOSSAccessKeyId":     settings.OSSAccessKeyID,
		"TicketAttachmentOSSAccessKeySecret": settings.OSSAccessKeySecret,
		"TicketAttachmentOSSCustomDomain":    settings.OSSCustomDomain,
		"TicketAttachmentS3Endpoint":         settings.S3Endpoint,
		"TicketAttachmentS3Bucket":           settings.S3Bucket,
		"TicketAttachmentS3Region":           settings.S3Region,
		"TicketAttachmentS3AccessKeyId":      settings.S3AccessKeyID,
		"TicketAttachmentS3AccessKeySecret":  settings.S3AccessKeySecret,
		"TicketAttachmentS3CustomDomain":     settings.S3CustomDomain,
		"TicketAttachmentCOSEndpoint":        settings.COSEndpoint,
		"TicketAttachmentCOSBucket":          settings.COSBucket,
		"TicketAttachmentCOSRegion":          settings.COSRegion,
		"TicketAttachmentCOSSecretId":        settings.COSSecretID,
		"TicketAttachmentCOSSecretKey":       settings.COSSecretKey,
		"TicketAttachmentCOSCustomDomain":    settings.COSCustomDomain,
	}
}

func TestTicketAttachmentSettingsGetterDeepCopiesLists(t *testing.T) {
	restoreTicketAttachmentSettings(t)
	if err := UpdateTicketAttachmentSettings(map[string]string{
		"TicketAttachmentAllowedExts":  "jpg,png",
		"TicketAttachmentAllowedMimes": "image/jpeg,image/png",
	}); err != nil {
		t.Fatal(err)
	}
	first := GetTicketAttachmentSettings()
	first.AllowedExtList[0] = "mutated"
	first.AllowedMimeList[0] = "mutated/type"
	second := GetTicketAttachmentSettings()
	if second.AllowedExtList[0] != "jpg" || second.AllowedMimeList[0] != "image/jpeg" {
		t.Fatalf("getter leaked mutable lists: %+v", second)
	}
}

func TestTicketAttachmentSettingsInvalidBatchDoesNotPublish(t *testing.T) {
	restoreTicketAttachmentSettings(t)
	before := GetTicketAttachmentSettings()
	if err := UpdateTicketAttachmentSettings(map[string]string{
		"TicketAttachmentStorage": "s3",
		"TicketAttachmentMaxSize": "0",
	}); err == nil {
		t.Fatal("expected invalid batch to fail")
	}
	after := GetTicketAttachmentSettings()
	if after.Storage != before.Storage || after.MaxSize != before.MaxSize {
		t.Fatalf("invalid batch published: got %+v, want %+v", after, before)
	}
}

func TestTicketAttachmentSettingsConcurrentVersions(t *testing.T) {
	restoreTicketAttachmentSettings(t)
	a := map[string]string{
		"TicketAttachmentStorage":           "s3",
		"TicketAttachmentS3Bucket":          "bucket-a",
		"TicketAttachmentS3AccessKeyId":     "key-a",
		"TicketAttachmentS3AccessKeySecret": "secret-a",
	}
	b := map[string]string{
		"TicketAttachmentStorage":           "s3",
		"TicketAttachmentS3Bucket":          "bucket-b",
		"TicketAttachmentS3AccessKeyId":     "key-b",
		"TicketAttachmentS3AccessKeySecret": "secret-b",
	}
	if err := UpdateTicketAttachmentSettings(a); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 1)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 2_000 {
			values := a
			if i%2 == 1 {
				values = b
			}
			if err := UpdateTicketAttachmentSettings(values); err != nil {
				select {
				case errs <- err:
				default:
				}
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range 4_000 {
			got := GetTicketAttachmentSettings()
			isA := got.S3Bucket == "bucket-a" && got.S3AccessKeyID == "key-a" && got.S3AccessKeySecret == "secret-a"
			isB := got.S3Bucket == "bucket-b" && got.S3AccessKeyID == "key-b" && got.S3AccessKeySecret == "secret-b"
			if !isA && !isB {
				select {
				case errs <- fmt.Errorf("torn attachment settings: %+v", got):
				default:
				}
				return
			}
		}
	}()
	wg.Wait()
	select {
	case err := <-errs:
		t.Fatal(err)
	default:
	}
}
