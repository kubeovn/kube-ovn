package frameworkhttp

import (
	"fmt"
	"net/http"
	"testing"
	"time"
)

type Report struct {
	Index       int       `json:"index"`
	Name        string    `json:"name"`
	Timestamp   time.Time `json:"timestamp"`
	Success     bool      `json:"success"`
	StartTime   int64     `json:"start_time"`
	Elapsed     int64     `json:"elapsed"`
	Attachments string    `json:"attachments,omitempty"`
}

func runCaseOnce(client *http.Client, method, url string, expectedStatusCode int) (failure *Report) {
	startTime := time.Now()

	req, err := http.NewRequest(method, url, nil)
	if err != nil {
		return &Report{
			Timestamp:   startTime,
			Success:     false,
			StartTime:   startTime.Unix(),
			Elapsed:     time.Since(startTime).Milliseconds(),
			Attachments: fmt.Sprintf("failed to create request: %v", err),
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return &Report{
			Timestamp:   startTime,
			Success:     false,
			StartTime:   startTime.Unix(),
			Elapsed:     time.Since(startTime).Milliseconds(),
			Attachments: fmt.Sprintf("failed to send request: %v", err),
		}
	}
	defer resp.Body.Close()

	if resp.StatusCode != expectedStatusCode {
		return &Report{
			Timestamp:   startTime,
			Success:     false,
			StartTime:   startTime.Unix(),
			Elapsed:     time.Since(startTime).Milliseconds(),
			Attachments: fmt.Sprintf("expected status code %d, got %d", expectedStatusCode, resp.StatusCode),
		}
	}

	return nil
}

func Loop(_ *testing.T, name, url, method string, count, interval, requestTimeout, expectedStatusCode int, stopCh <-chan struct{}) ([]*Report, error) {
	client := &http.Client{
		Timeout: time.Duration(requestTimeout) * time.Millisecond,
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

		result := runCaseOnce(client, method, url, expectedStatusCode)
		if result != nil {
			result.Index = i
			result.Name = name
			failures = append(failures, result)
		} else if interval != 0 {
			time.Sleep(time.Duration(interval) * time.Millisecond)
		}
	}

	return failures, nil
}
