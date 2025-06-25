package http

import (
	"fmt"
	"testing"
	"time"

	httprunner "github.com/httprunner/httprunner/v5"
	"github.com/rs/zerolog"
)

type Report struct {
	Index     int
	Timestamp time.Time `json:"timestamp"`
	*httprunner.StepResult
}

func init() {
	zerolog.SetGlobalLevel(zerolog.WarnLevel)
}

func runCaseOnce(runner *httprunner.CaseRunner) (failure *Report) {
	session := runner.NewSession()
	startTime := time.Now()
	failure = &Report{
		Timestamp: startTime,
		StepResult: &httprunner.StepResult{
			Success:   false,
			StartTime: startTime.Unix(),
		},
	}
	summary, err := session.Start(nil)
	if err != nil {
		failure.Elapsed = time.Since(startTime).Milliseconds()
		failure.Attachments = fmt.Errorf("failed to start session: %w", err).Error()
		return
	}

	if !summary.Success {
		if len(summary.Records) != 0 {
			failure.StepResult = summary.Records[0]
		} else {
			failure.Elapsed = time.Since(startTime).Milliseconds()
			failure.Attachments = "unexpected empty summary records"
		}
		return
	}

	if len(summary.Records) != 1 {
		failure.Elapsed = time.Since(startTime).Milliseconds()
		failure.Attachments = fmt.Sprintf("record count should be 1, but got %#v", summary.Records)
	}

	return nil
}

func Loop(t *testing.T, name, url, method string, count, interval, requestTimeout, expectedStatusCode int, stopCh <-chan struct{}) ([]*Report, error) {
	tc := httprunner.TestCase{
		Config: httprunner.NewConfig(name).SetRequestTimeout(float32(requestTimeout) / 1000),
		TestSteps: []httprunner.IStep{
			httprunner.NewStep(method).GET(url).Validate().AssertEqual("status_code", expectedStatusCode, "check status code"),
		},
	}

	runner, err := httprunner.NewRunner(t).SetFailfast(false).NewCaseRunner(tc)
	if err != nil {
		return nil, fmt.Errorf("failed to create new case runner: %w", err)
	}

	var failures []*Report
LOOP:
	for i := 0; count == 0 || i < count; i++ {
		if stopCh != nil {
			select {
			case <-stopCh:
				break LOOP
			default:
			}
		}

		result := runCaseOnce(runner)
		if result != nil {
			result.Index, result.Name = i, name
			failures = append(failures, result)
		} else if interval != 0 {
			time.Sleep(time.Duration(interval) * time.Millisecond)
		}
	}

	return failures, nil
}
