package frameworkhttp

import (
	"fmt"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
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
	Trace       string    `json:"trace,omitempty"`
	Attachments string    `json:"attachments,omitempty"`
}

func runCaseOnce(client *http.Client, method, url string, expectedStatusCode int) (failure *Report) {
	startTime := time.Now()
	// Trace hooks can run concurrently, including after a timeout.
	var mu sync.Mutex
	var events []string
	record := func(event string) {
		mu.Lock()
		defer mu.Unlock()
		events = append(events, fmt.Sprintf("%s +%s %s", time.Now().UTC().Format(time.RFC3339Nano), time.Since(startTime), event))
	}
	defer func() {
		if failure != nil {
			mu.Lock()
			defer mu.Unlock()
			failure.Trace = strings.Join(events, "\n")
		}
	}()

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

	trace := &httptrace.ClientTrace{
		GetConn:      func(hostPort string) { record("get connection " + hostPort) },
		ConnectStart: func(network, addr string) { record("connect start " + network + " " + addr) },
		ConnectDone: func(network, addr string, err error) {
			record(fmt.Sprintf("connect done %s %s error=%v", network, addr, err))
		},
		GotConn: func(info httptrace.GotConnInfo) {
			record(fmt.Sprintf("got connection local=%s remote=%s reused=%t wasIdle=%t idleTime=%s",
				info.Conn.LocalAddr(), info.Conn.RemoteAddr(), info.Reused, info.WasIdle, info.IdleTime))
		},
		WroteRequest:         func(info httptrace.WroteRequestInfo) { record(fmt.Sprintf("wrote request error=%v", info.Err)) },
		GotFirstResponseByte: func() { record("got first response byte") },
	}
	req = req.WithContext(httptrace.WithClientTrace(req.Context(), trace))
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
	return LoopWithFailureHandler(name, url, method, count, interval, requestTimeout, expectedStatusCode, stopCh, nil)
}

// LoopWithFailureHandler calls onFailure as soon as each failed request completes.
// The handler must return promptly so that diagnostic work does not pause traffic.
func LoopWithFailureHandler(name, url, method string, count, interval, requestTimeout, expectedStatusCode int, stopCh <-chan struct{}, onFailure func(*Report)) ([]*Report, error) {
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
			if onFailure != nil {
				onFailure(result)
			}
		} else if interval != 0 {
			time.Sleep(time.Duration(interval) * time.Millisecond)
		}
	}

	return failures, nil
}
