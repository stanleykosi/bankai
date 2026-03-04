package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestJobQueueFailureInjectionToDLQ(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	queue := NewJobQueue(rdb, "jobs:test")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var attempts int32
	queue.StartWorkers(ctx, 1, func(_ context.Context, _ Job) error {
		atomic.AddInt32(&attempts, 1)
		return errors.New("injected worker failure")
	})

	if _, err := queue.Enqueue(ctx, Job{
		Type:        JobTypeTPSLEvaluate,
		MaxAttempts: 2,
	}); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	deadline := time.Now().Add(6 * time.Second)
	for {
		dlqLen := rdb.LLen(ctx, queue.DLQName()).Val()
		if dlqLen > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job did not reach DLQ in time; attempts=%d", attempts)
		}
		time.Sleep(50 * time.Millisecond)
	}

	if attempts < 2 {
		t.Fatalf("expected at least 2 attempts, got %d", attempts)
	}
	t.Logf("job-queue failure injection result: attempts=%d dlq_len=%d", attempts, rdb.LLen(ctx, queue.DLQName()).Val())
}

func TestJobQueueHandlerPanicIsRetriedAndDLQed(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	queue := NewJobQueue(rdb, "jobs:panic")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var calls int32
	queue.StartWorkers(ctx, 1, func(_ context.Context, _ Job) error {
		atomic.AddInt32(&calls, 1)
		panic("injected panic")
	})

	if _, err := queue.Enqueue(ctx, Job{
		Type:        JobTypeTPSLEvaluate,
		MaxAttempts: 2,
	}); err != nil {
		t.Fatalf("enqueue failed: %v", err)
	}

	deadline := time.Now().Add(6 * time.Second)
	for {
		if got := rdb.LLen(ctx, queue.DLQName()).Val(); got > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("panicing job did not reach DLQ in time; calls=%d", calls)
		}
		time.Sleep(50 * time.Millisecond)
	}

	if calls < 2 {
		t.Fatalf("expected panicing job to be retried, got calls=%d", calls)
	}
}

func TestJobQueueRecoversReservedJobsOnStart(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	queue := NewJobQueue(rdb, "jobs:recover")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	job := Job{
		ID:          "recover-job-1",
		Type:        JobTypeTPSLEvaluate,
		MaxAttempts: 1,
		EnqueuedAt:  time.Now().UTC(),
	}
	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	// Simulate a crash between reserve and ack by placing the job in processing.
	if err := rdb.RPush(ctx, queue.processing, raw).Err(); err != nil {
		t.Fatalf("failed to seed processing queue: %v", err)
	}

	var processed int32
	queue.StartWorkers(ctx, 1, func(_ context.Context, _ Job) error {
		atomic.AddInt32(&processed, 1)
		return nil
	})

	deadline := time.Now().Add(3 * time.Second)
	for atomic.LoadInt32(&processed) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("recovered job was not processed in time")
		}
		time.Sleep(25 * time.Millisecond)
	}

	if got := rdb.LLen(ctx, queue.processing).Val(); got != 0 {
		t.Fatalf("expected empty processing queue after ack, got len=%d", got)
	}
}

func TestJobQueueSkipsRecoveryWhenLegacyHeartbeatIsFresh(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	queue := NewJobQueue(rdb, "jobs:recover-active")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	job := Job{
		ID:          "recover-active-job-1",
		Type:        JobTypeTPSLEvaluate,
		MaxAttempts: 1,
		EnqueuedAt:  time.Now().UTC(),
	}
	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	// Simulate a crash artifact from older versions that stored heartbeat as raw unix seconds.
	freshHeartbeat := fmt.Sprintf("%d", time.Now().UTC().Unix())
	if err := rdb.Set(ctx, queue.heartbeatKey, freshHeartbeat, time.Minute).Err(); err != nil {
		t.Fatalf("failed to seed worker heartbeat: %v", err)
	}
	if err := rdb.RPush(ctx, queue.processing, raw).Err(); err != nil {
		t.Fatalf("failed to seed processing queue: %v", err)
	}

	var processed int32
	queue.StartWorkers(ctx, 1, func(_ context.Context, _ Job) error {
		atomic.AddInt32(&processed, 1)
		return nil
	})

	// With a fresh legacy heartbeat and unknown ownership, recovery should be skipped.
	time.Sleep(250 * time.Millisecond)
	if atomic.LoadInt32(&processed) != 0 {
		t.Fatalf("expected no processing while fresh legacy heartbeat exists")
	}
	if got := rdb.LLen(ctx, queue.processing).Val(); got != 1 {
		t.Fatalf("expected reserved job to remain in processing queue, got len=%d", got)
	}

	// Once legacy heartbeat becomes stale, explicit recovery should reclaim the reserved job.
	staleHeartbeat := fmt.Sprintf("%d", time.Now().UTC().Add(-5*time.Minute).Unix())
	if err := rdb.Set(ctx, queue.heartbeatKey, staleHeartbeat, time.Minute).Err(); err != nil {
		t.Fatalf("failed to overwrite with stale heartbeat: %v", err)
	}
	queue.maybeRecoverInFlight(ctx)

	deadline := time.Now().Add(3 * time.Second)
	for atomic.LoadInt32(&processed) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("expected stale-heartbeat recovery to process reserved job")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func TestJobQueueSkipsRecoveryWhenAnotherWorkerHeartbeatExists(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	queue := NewJobQueue(rdb, "jobs:recover-live-worker")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	job := Job{
		ID:          "recover-live-worker-job-1",
		Type:        JobTypeTPSLEvaluate,
		MaxAttempts: 1,
		EnqueuedAt:  time.Now().UTC(),
	}
	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	if err := rdb.RPush(ctx, queue.processing, raw).Err(); err != nil {
		t.Fatalf("failed to seed processing queue: %v", err)
	}

	otherWorkerKey := queue.heartbeatKeyPrefix + "other-worker"
	if err := rdb.Set(ctx, otherWorkerKey, fmt.Sprintf("%d", time.Now().UTC().Unix()), time.Minute).Err(); err != nil {
		t.Fatalf("failed to seed other worker heartbeat: %v", err)
	}

	var processed int32
	queue.StartWorkers(ctx, 1, func(_ context.Context, _ Job) error {
		atomic.AddInt32(&processed, 1)
		return nil
	})

	time.Sleep(250 * time.Millisecond)
	if atomic.LoadInt32(&processed) != 0 {
		t.Fatalf("expected no recovery while another worker heartbeat is active")
	}
	if got := rdb.LLen(ctx, queue.processing).Val(); got != 1 {
		t.Fatalf("expected reserved job to remain in processing queue, got len=%d", got)
	}
}

func TestJobQueueRecoversWhenHeartbeatIsStale(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	queue := NewJobQueue(rdb, "jobs:recover-stale")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	job := Job{
		ID:          "recover-stale-job-1",
		Type:        JobTypeTPSLEvaluate,
		MaxAttempts: 1,
		EnqueuedAt:  time.Now().UTC(),
	}
	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	// Stale heartbeat key can still exist briefly after crashes.
	staleHeartbeat := fmt.Sprintf("%d", time.Now().UTC().Add(-5*time.Minute).Unix())
	if err := rdb.Set(ctx, queue.heartbeatKey, staleHeartbeat, time.Minute).Err(); err != nil {
		t.Fatalf("failed to seed stale heartbeat: %v", err)
	}
	if err := rdb.RPush(ctx, queue.processing, raw).Err(); err != nil {
		t.Fatalf("failed to seed processing queue: %v", err)
	}

	var processed int32
	queue.StartWorkers(ctx, 1, func(_ context.Context, _ Job) error {
		atomic.AddInt32(&processed, 1)
		return nil
	})

	deadline := time.Now().Add(3 * time.Second)
	for atomic.LoadInt32(&processed) == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("stale-heartbeat recovery did not process job in time")
		}
		time.Sleep(25 * time.Millisecond)
	}
}

func BenchmarkJobQueueEnqueue(b *testing.B) {
	mr, err := miniredis.Run()
	if err != nil {
		b.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	queue := NewJobQueue(rdb, "jobs:bench")
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := queue.Enqueue(ctx, Job{
			Type:        JobTypeReconcileMarkets,
			MaxAttempts: 3,
		})
		if err != nil {
			b.Fatalf("enqueue failed: %v", err)
		}
	}
}
