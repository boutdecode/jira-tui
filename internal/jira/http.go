package jira

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maxRetries = 3

// Notify, when set, is called before waiting on a rate-limited request.
var Notify func(wait time.Duration, attempt int)

// WithRetry runs f, retrying on HTTP 429 (Retry-After, else 1s/2s/4s backoff).
// A 429 means the request was not processed, so retrying is safe for every method.
func WithRetry(f func() error) error {
	for attempt := 0; ; attempt++ {
		err := f()
		var je *Error
		if errors.As(err, &je) && je.Status == 429 && attempt < maxRetries {
			wait := je.RetryAfter
			if wait < 0 { // header absent: exponential backoff
				wait = time.Second << attempt
			}
			wait = min(wait, 30*time.Second)
			if Notify != nil {
				Notify(wait, attempt+1)
			}
			time.Sleep(wait)
			continue
		}
		return err
	}
}

// DoRequest performs one JSON request. Non-2xx responses become *Error.
func DoRequest(hc *http.Client, method, u string, headers map[string]string, payload []byte, out any) error {
	var rd io.Reader
	if payload != nil {
		rd = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, u, rd)
	if err != nil {
		return err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode/100 != 2 {
		e := &Error{Status: resp.StatusCode, Message: strings.TrimSpace(string(data)), RetryAfter: -1}
		if s, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
			e.RetryAfter = time.Duration(s) * time.Second
		}
		if len(e.Message) > 200 {
			e.Message = e.Message[:200]
		}
		return e
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	return json.Unmarshal(data, out)
}
