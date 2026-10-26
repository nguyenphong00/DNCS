package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"dncs/backend/internal/loadsmoke"
)

func main() {
	config := loadsmoke.Config{}
	flag.StringVar(&config.URL, "url", "http://localhost:8080/health/live", "absolute health/read-only URL without query or credentials")
	flag.IntVar(&config.Requests, "requests", 1000, "total request count")
	flag.IntVar(&config.Concurrency, "concurrency", 32, "concurrent workers")
	flag.DurationVar(&config.RequestTimeout, "request-timeout", 2*time.Second, "timeout for one request")
	flag.DurationVar(&config.MaxP95, "max-p95", 250*time.Millisecond, "maximum accepted p95 latency")
	flag.Float64Var(&config.MaxErrorRate, "max-error-rate", 0, "maximum accepted error rate from 0 to 1")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	result, err := loadsmoke.Run(ctx, config)
	fmt.Printf("requests=%d failures=%d error_rate=%.4f p95=%s elapsed=%s\n", result.Requests, result.Failures, result.ErrorRate, result.P95, result.Elapsed)
	if err != nil {
		log.Fatal(err)
	}
}
