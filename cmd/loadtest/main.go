package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"net/http"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

func main() {
	var (
		base     = flag.String("base", "http://localhost:8080", "server base URL")
		seats    = flag.Int("seats", 50, "number of seats to spread attempts across")
		workers  = flag.Int("workers", 200, "concurrent workers")
		contend  = flag.Bool("contend", false, "if true, all workers hammer ONE seat")
	)
	flag.Parse()

	type result struct {
		latency time.Duration
		status  int
	}
	results := make([]result, *workers)

	var (
		wg      sync.WaitGroup
		start   = make(chan struct{})
		ok      int64
		conflct int64
	)

	client := &http.Client{Timeout: 10 * time.Second}

	for i := 0; i < *workers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			seatID := 1
			if *contend {
				seatID = 1 // everyone fights over seat 1
			} else {
				seatID = (idx % *seats) + 1
			}
			body := []byte(`{"user_id":1}`)

			<-start
			t0 := time.Now()
			// hold then confirm (confirm is the locked path we care about)
			post(client, fmt.Sprintf("%s/v1/seats/%d/hold", *base, seatID), body)
			resp := post(client, fmt.Sprintf("%s/v1/seats/%d/confirm", *base, seatID), body)
			lat := time.Since(t0)

			results[idx] = result{latency: lat, status: resp}
			switch resp {
			case http.StatusCreated:
				atomic.AddInt64(&ok, 1)
			case http.StatusConflict, http.StatusUnprocessableEntity:
				atomic.AddInt64(&conflct, 1)
			}
		}(i)
	}

	wallStart := time.Now()
	close(start)
	wg.Wait()
	wall := time.Since(wallStart)

	// Latency percentiles.
	lats := make([]time.Duration, len(results))
	for i, r := range results {
		lats[i] = r.latency
	}
	sort.Slice(lats, func(i, j int) bool { return lats[i] < lats[j] })
	p := func(q float64) time.Duration { return lats[int(float64(len(lats)-1)*q)] }

	fmt.Printf("\n=== seatlock load test ===\n")
	fmt.Printf("workers=%d  contend_single_seat=%v  seats=%d\n", *workers, *contend, *seats)
	fmt.Printf("wall_time=%v  throughput=%.0f req/s\n", wall.Round(time.Millisecond),
		float64(*workers)/wall.Seconds())
	fmt.Printf("booked(201)=%d  conflicts(409/422)=%d\n", ok, conflct)
	fmt.Printf("latency  p50=%v  p95=%v  p99=%v  max=%v\n",
		p(0.50).Round(time.Millisecond), p(0.95).Round(time.Millisecond),
		p(0.99).Round(time.Millisecond), lats[len(lats)-1].Round(time.Millisecond))
}

func post(c *http.Client, url string, body []byte) int {
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return 0
	}
	resp.Body.Close()
	return resp.StatusCode
}