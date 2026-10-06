package scheduler

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewRejectsInvalidGraphs(t *testing.T) {
	cases := [][]Task{
		{{ID: "duplicate", Action: func(context.Context) error { return nil }}, {ID: "duplicate", Action: func(context.Context) error { return nil }}},
		{{ID: "child", Dependencies: []string{"missing"}, Action: func(context.Context) error { return nil }}},
		{{ID: "a", Dependencies: []string{"b"}, Action: func(context.Context) error { return nil }}, {ID: "b", Dependencies: []string{"a"}, Action: func(context.Context) error { return nil }}},
	}
	for _, tasks := range cases {
		if _, err := New(tasks, 2); !errors.Is(err, ErrInvalidTaskGraph) {
			t.Fatalf("expected invalid graph error, got %v", err)
		}
	}
}

func TestSchedulerHonorsDependenciesLocksAndDeterministicResults(t *testing.T) {
	var active int32
	var maxActive int32
	started := make(chan string, 3)
	step := func(id string) func(context.Context) error {
		return func(context.Context) error {
			current := atomic.AddInt32(&active, 1)
			for {
				maximum := atomic.LoadInt32(&maxActive)
				if current <= maximum || atomic.CompareAndSwapInt32(&maxActive, maximum, current) {
					break
				}
			}
			started <- id
			time.Sleep(15 * time.Millisecond)
			atomic.AddInt32(&active, -1)
			return nil
		}
	}
	scheduler, err := New([]Task{
		{ID: "a", Lock: "device:r1", Action: step("a")},
		{ID: "b", Lock: "device:r1", Action: step("b")},
		{ID: "c", Dependencies: []string{"a", "b"}, Action: step("c")},
	}, 2)
	if err != nil {
		t.Fatal(err)
	}
	results, err := scheduler.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 || atomic.LoadInt32(&maxActive) > 1 {
		t.Fatalf("lock or result count violated: max=%d results=%#v", maxActive, results)
	}
	ids := make([]string, 0, len(results))
	for _, result := range results {
		ids = append(ids, result.ID)
		if result.Status != StatusSuccess || result.Attempts != 1 {
			t.Fatalf("unexpected task result: %#v", result)
		}
	}
	if !sort.StringsAreSorted(ids) || strings.Join(ids, ",") != "a,b,c" {
		t.Fatalf("results are not deterministic: %v", ids)
	}
	close(started)
}

func TestSchedulerRetriesOnlyRetryableFailures(t *testing.T) {
	var attempts int32
	scheduler, err := New([]Task{{
		ID: "retry", Retry: RetryPolicy{MaxAttempts: 3},
		Action: func(context.Context) error {
			current := atomic.AddInt32(&attempts, 1)
			if current < 3 {
				return Retry(errors.New("temporary failure"))
			}
			return nil
		},
	}, {
		ID: "permanent", Retry: RetryPolicy{MaxAttempts: 3},
		Action: func(context.Context) error { return Permanent(errors.New("invalid configuration")) },
	}}, 2)
	if err != nil {
		t.Fatal(err)
	}
	results, err := scheduler.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if results[0].ID != "permanent" || results[0].Status != StatusFailed || results[0].Attempts != 1 {
		t.Fatalf("permanent failure was retried: %#v", results[0])
	}
	if results[1].ID != "retry" || results[1].Status != StatusSuccess || results[1].Attempts != 3 {
		t.Fatalf("retry policy was not applied: %#v", results[1])
	}
}

func TestSchedulerTimeoutAndCancellation(t *testing.T) {
	scheduler, err := New([]Task{
		{ID: "slow", Timeout: 10 * time.Millisecond, Retry: RetryPolicy{MaxAttempts: 2}, Action: func(ctx context.Context) error {
			<-ctx.Done()
			return ctx.Err()
		}},
		{ID: "dependent", Dependencies: []string{"slow"}, Action: func(context.Context) error { return nil }},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	results, err := scheduler.Run(context.Background())
	byID := make(map[string]Result, len(results))
	for _, result := range results {
		byID[result.ID] = result
	}
	if err != nil || byID["slow"].Status != StatusFailed || byID["slow"].Attempts != 2 || byID["dependent"].Status != StatusBlocked {
		t.Fatalf("unexpected timeout results: %#v, %v", results, err)
	}

	contextToCancel, cancel := context.WithCancel(context.Background())
	cancelScheduler, err := New([]Task{{ID: "running", Action: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}, {ID: "queued", Dependencies: []string{"running"}, Action: func(context.Context) error { return nil }}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	cancelled, err := cancelScheduler.Run(contextToCancel)
	byID = make(map[string]Result, len(cancelled))
	for _, result := range cancelled {
		byID[result.ID] = result
	}
	if err != nil || byID["running"].Status != StatusCancelled || byID["queued"].Status != StatusCancelled {
		t.Fatalf("unexpected cancellation results: %#v, %v", cancelled, err)
	}
}

func TestSchedulerContainsActionPanics(t *testing.T) {
	scheduler, err := New([]Task{{ID: "panic", Action: func(context.Context) error { panic("adapter bug") }}}, 1)
	if err != nil {
		t.Fatal(err)
	}
	results, err := scheduler.Run(context.Background())
	if err != nil || len(results) != 1 || results[0].Status != StatusFailed || results[0].Error != "task action panicked" {
		t.Fatalf("panic was not converted to a failed task: %#v, %v", results, err)
	}
}
