package loadsmoke

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"
)

type Config struct {
	URL            string
	Requests       int
	Concurrency    int
	RequestTimeout time.Duration
	MaxP95         time.Duration
	MaxErrorRate   float64
}

type Result struct {
	Requests  int
	Failures  int
	ErrorRate float64
	P95       time.Duration
	Elapsed   time.Duration
}

func Run(ctx context.Context, config Config) (Result, error) {
	return run(ctx, config, &http.Client{Timeout: config.RequestTimeout})
}

func run(ctx context.Context, config Config, client *http.Client) (Result, error) {
	if err := validate(config); err != nil {
		return Result{}, err
	}
	jobs := make(chan struct{})
	durations := make(chan time.Duration, config.Requests)
	failures := make(chan struct{}, config.Requests)
	started := time.Now()
	var wait sync.WaitGroup
	for range config.Concurrency {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range jobs {
				request, err := http.NewRequestWithContext(ctx, http.MethodGet, config.URL, nil)
				if err != nil {
					failures <- struct{}{}
					continue
				}
				requestStarted := time.Now()
				response, err := client.Do(request)
				durations <- time.Since(requestStarted)
				if err != nil {
					failures <- struct{}{}
					continue
				}
				_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 64*1024))
				_ = response.Body.Close()
				if response.StatusCode < 200 || response.StatusCode >= 300 {
					failures <- struct{}{}
				}
			}
		}()
	}
	for range config.Requests {
		select {
		case jobs <- struct{}{}:
		case <-ctx.Done():
			close(jobs)
			wait.Wait()
			return Result{}, ctx.Err()
		}
	}
	close(jobs)
	wait.Wait()
	close(durations)
	close(failures)

	values := make([]time.Duration, 0, config.Requests)
	for duration := range durations {
		values = append(values, duration)
	}
	result := Result{Requests: config.Requests, Failures: len(failures), Elapsed: time.Since(started)}
	result.ErrorRate = float64(result.Failures) / float64(config.Requests)
	sort.Slice(values, func(left, right int) bool { return values[left] < values[right] })
	if len(values) > 0 {
		index := (95*len(values)+99)/100 - 1
		result.P95 = values[index]
	}
	if result.ErrorRate > config.MaxErrorRate || result.P95 > config.MaxP95 {
		return result, fmt.Errorf("load-smoke threshold failed: error_rate=%.4f p95=%s", result.ErrorRate, result.P95)
	}
	return result, nil
}

func validate(config Config) error {
	parsed, err := url.Parse(config.URL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return errors.New("load-smoke URL must be an absolute HTTP(S) URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("load-smoke URL must not contain credentials, query, or fragment")
	}
	if config.Requests < 1 || config.Concurrency < 1 || config.Concurrency > config.Requests {
		return errors.New("load-smoke request and concurrency values are invalid")
	}
	if config.RequestTimeout <= 0 || config.MaxP95 <= 0 || config.MaxErrorRate < 0 || config.MaxErrorRate > 1 {
		return errors.New("load-smoke thresholds are invalid")
	}
	return nil
}
