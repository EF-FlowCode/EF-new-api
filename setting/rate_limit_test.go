package setting

import (
	"fmt"
	"strconv"
	"sync"
	"testing"
)

func TestGetUserModelRPMUsesHighestOwnedCustomerTier(t *testing.T) {
	previous := ModelRPMConfig2JSONString()
	t.Cleanup(func() {
		if err := UpdateModelRPMConfigByJSONString(previous); err != nil {
			t.Fatalf("restore model RPM config: %v", err)
		}
	})

	config := `{
		"enabled": true,
		"models": {
			"gpt-5.6-luna": {
				"global_rpm": 20,
				"group_rpm": {"vip": 30, "svip": 60, "trial": 10}
			}
		}
	}`
	if err := UpdateModelRPMConfigByJSONString(config); err != nil {
		t.Fatalf("update model RPM config: %v", err)
	}

	tests := []struct {
		name       string
		model      string
		primary    string
		additional []string
		want       int
	}{
		{name: "global default", model: "gpt-5.6-luna", primary: "default", want: 20},
		{name: "primary override", model: "gpt-5.6-luna", primary: "vip", want: 30},
		{name: "highest additional tier", model: "gpt-5.6-luna", primary: "vip", additional: []string{"svip"}, want: 60},
		{name: "lower tier cannot reduce entitlement", model: "gpt-5.6-luna", primary: "trial", want: 20},
		{name: "unconfigured model", model: "gpt-5.6-sol", primary: "svip", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetUserModelRPM(tt.model, tt.primary, tt.additional); got != tt.want {
				t.Fatalf("GetUserModelRPM() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCheckModelRPMConfigRejectsInvalidRPM(t *testing.T) {
	err := CheckModelRPMConfig(`{"enabled":true,"models":{"gpt-5.6-luna":{"global_rpm":0}}}`)
	if err == nil {
		t.Fatal("expected zero RPM to be rejected")
	}
}

func TestUpdateModelRequestRateLimitGroupDoesNotPublishInvalidJSON(t *testing.T) {
	previous := ModelRequestRateLimitGroup2JSONString()
	t.Cleanup(func() {
		if err := UpdateModelRequestRateLimitGroupByJSONString(previous); err != nil {
			t.Fatalf("restore model request rate limit group: %v", err)
		}
	})

	const valid = `{"vip":[120,100]}`
	if err := UpdateModelRequestRateLimitGroupByJSONString(valid); err != nil {
		t.Fatalf("publish initial model request rate limit group: %v", err)
	}
	if err := UpdateModelRequestRateLimitGroupByJSONString(`{"vip":`); err == nil {
		t.Fatal("expected invalid JSON to be rejected")
	}

	total, success, found := GetGroupRateLimit("vip")
	if !found || total != 120 || success != 100 {
		t.Fatalf("published state changed after invalid JSON: (%d, %d, %v)", total, success, found)
	}
}

func TestModelRequestRateLimitGroupConcurrentReadWrite(t *testing.T) {
	previous := ModelRequestRateLimitGroup2JSONString()
	t.Cleanup(func() {
		if err := UpdateModelRequestRateLimitGroupByJSONString(previous); err != nil {
			t.Fatalf("restore model request rate limit group: %v", err)
		}
	})
	if err := UpdateModelRequestRateLimitGroupByJSONString(`{"group":[0,1]}`); err != nil {
		t.Fatalf("publish initial model request rate limit group: %v", err)
	}

	const iterations = 500
	start := make(chan struct{})
	errCh := make(chan error, 1)
	report := func(err error) {
		select {
		case errCh <- err:
		default:
		}
	}

	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		for version := 1; version <= iterations; version++ {
			value := fmt.Sprintf(`{"group":[%d,%d]}`, version, version+1)
			if err := UpdateModelRequestRateLimitGroupByJSONString(value); err != nil {
				report(err)
				return
			}
		}
	}()
	for range 4 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for range iterations {
				total, success, found := GetGroupRateLimit("group")
				if !found || success != total+1 {
					report(fmt.Errorf("observed torn model rate limit state: (%d, %d, %v)", total, success, found))
					return
				}
			}
		}()
	}

	close(start)
	workers.Wait()
	select {
	case err := <-errCh:
		t.Fatal(err)
	default:
	}
}

func TestModelRequestRateLimitSettingsPublishCompleteVersions(t *testing.T) {
	previous := GetModelRequestRateLimitSettings()
	previousGroups := ModelRequestRateLimitGroup2JSONString()
	t.Cleanup(func() {
		err := UpdateModelRequestRateLimitSettings(map[string]string{
			"ModelRequestRateLimitEnabled":         strconv.FormatBool(previous.Enabled),
			"ModelRequestRateLimitDurationMinutes": strconv.Itoa(previous.DurationMinutes),
			"ModelRequestRateLimitCount":           strconv.Itoa(previous.TotalCount),
			"ModelRequestRateLimitSuccessCount":    strconv.Itoa(previous.SuccessCount),
			"ModelRequestRateLimitGroup":           previousGroups,
		})
		if err != nil {
			t.Fatalf("restore model request rate limit settings: %v", err)
		}
	})

	publishVersion := func(version int) error {
		return UpdateModelRequestRateLimitSettings(map[string]string{
			"ModelRequestRateLimitEnabled":         "true",
			"ModelRequestRateLimitDurationMinutes": strconv.Itoa(version),
			"ModelRequestRateLimitCount":           strconv.Itoa(version * 2),
			"ModelRequestRateLimitSuccessCount":    strconv.Itoa(version*2 + 1),
		})
	}
	if err := publishVersion(1); err != nil {
		t.Fatalf("publish initial model request rate limit settings: %v", err)
	}

	const iterations = 500
	start := make(chan struct{})
	errCh := make(chan error, 1)
	report := func(err error) {
		select {
		case errCh <- err:
		default:
		}
	}

	var workers sync.WaitGroup
	workers.Add(1)
	go func() {
		defer workers.Done()
		<-start
		for version := 2; version <= iterations; version++ {
			if err := publishVersion(version); err != nil {
				report(err)
				return
			}
		}
	}()
	for range 4 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for range iterations {
				settings := GetModelRequestRateLimitSettings()
				if !settings.Enabled || settings.TotalCount != settings.DurationMinutes*2 || settings.SuccessCount != settings.TotalCount+1 {
					report(fmt.Errorf("observed torn model rate limit settings: %+v", settings))
					return
				}
			}
		}()
	}

	close(start)
	workers.Wait()
	select {
	case err := <-errCh:
		t.Fatal(err)
	default:
	}
}

func TestUpdateModelRequestRateLimitSettingsRejectsPartialPublication(t *testing.T) {
	before := GetModelRequestRateLimitSettings()
	err := UpdateModelRequestRateLimitSettings(map[string]string{
		"ModelRequestRateLimitEnabled":         strconv.FormatBool(!before.Enabled),
		"ModelRequestRateLimitDurationMinutes": "not-an-integer",
	})
	if err == nil {
		t.Fatal("expected invalid settings to be rejected")
	}
	if after := GetModelRequestRateLimitSettings(); after != before {
		t.Fatalf("settings changed after rejected update: before=%+v after=%+v", before, after)
	}
}

func TestModelRequestRateLimitGroupAcceptsEmpty(t *testing.T) {
	for _, value := range []string{"", "  "} {
		if err := CheckModelRequestRateLimitGroup(value); err != nil {
			t.Fatalf("empty group config %q rejected: %v", value, err)
		}
	}
}
