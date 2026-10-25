package service

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"

	"eflow.local/new-api/common"
	"eflow.local/new-api/constant"
)

func TestGetEmailTemplateObservesAtomicSubjectBodyUpdates(t *testing.T) {
	key := constant.EmailTemplateKeyTicketCreatedAdmin
	subjectKey := constant.EmailTemplateSubjectKey(key)
	bodyKey := constant.EmailTemplateBodyKey(key)

	common.OptionMapRWMutex.Lock()
	previous := common.OptionMap
	common.OptionMap = map[string]string{
		subjectKey: "subject-0",
		bodyKey:    "body-0",
	}
	common.OptionMapRWMutex.Unlock()
	t.Cleanup(func() {
		common.OptionMapRWMutex.Lock()
		common.OptionMap = previous
		common.OptionMapRWMutex.Unlock()
	})

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
			common.OptionMapRWMutex.Lock()
			common.OptionMap[subjectKey] = fmt.Sprintf("subject-%d", version)
			runtime.Gosched()
			common.OptionMap[bodyKey] = fmt.Sprintf("body-%d", version)
			common.OptionMapRWMutex.Unlock()
		}
	}()
	for range 4 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-start
			for range iterations {
				subject, body, _, ok := GetEmailTemplate(key)
				if !ok {
					report(fmt.Errorf("email template %q not found", key))
					return
				}
				subjectVersion := strings.TrimPrefix(subject, "subject-")
				bodyVersion := strings.TrimPrefix(body, "body-")
				if subjectVersion != bodyVersion {
					report(fmt.Errorf("observed torn email template: subject=%q body=%q", subject, body))
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
