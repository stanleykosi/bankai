package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type JobType string

const (
	JobTypeBroadcastNotification JobType = "broadcast_notification"
	JobTypeTPSLEvaluate          JobType = "tpsl_evaluate"
	JobTypeReconcileMarkets      JobType = "reconcile_markets"
	JobTypeReconcileOrderBooks   JobType = "reconcile_order_books"
	JobTypeCleanupNotifications  JobType = "cleanup_notifications"

	jobHeartbeatInterval = 10 * time.Second
	jobHeartbeatTTL      = 30 * time.Second
	jobHeartbeatFreshFor = 20 * time.Second
)

type Job struct {
	ID          string          `json:"id"`
	Type        JobType         `json:"type"`
	Payload     json.RawMessage `json:"payload,omitempty"`
	Attempts    int             `json:"attempts"`
	MaxAttempts int             `json:"max_attempts"`
	EnqueuedAt  time.Time       `json:"enqueued_at"`
	LastError   string          `json:"last_error,omitempty"`
}

type JobHandler func(context.Context, Job) error

type JobQueue struct {
	redis      *redis.Client
	queue      string
	processing string
	dlq        string
	instanceID string

	heartbeatKey       string
	heartbeatKeyPrefix string
	recoveryLockKey    string
	heartbeatOnce      sync.Once
}

func NewJobQueue(rdb *redis.Client, queue string) *JobQueue {
	name := queue
	if name == "" {
		name = "jobs:default"
	}
	return &JobQueue{
		redis:              rdb,
		queue:              name,
		processing:         name + ":processing",
		dlq:                name + ":dlq",
		instanceID:         uuid.NewString(),
		heartbeatKey:       name + ":workers:heartbeat", // legacy single-key heartbeat (backward compatibility)
		heartbeatKeyPrefix: name + ":workers:heartbeat:",
		recoveryLockKey:    name + ":recovery:lock",
	}
}

func (q *JobQueue) Enqueue(ctx context.Context, job Job) (string, error) {
	if q == nil || q.redis == nil {
		return "", errors.New("job queue unavailable")
	}
	if job.Type == "" {
		return "", errors.New("job type is required")
	}
	if job.ID == "" {
		job.ID = uuid.NewString()
	}
	if job.MaxAttempts <= 0 {
		job.MaxAttempts = 3
	}
	if job.EnqueuedAt.IsZero() {
		job.EnqueuedAt = time.Now().UTC()
	}

	data, err := json.Marshal(job)
	if err != nil {
		return "", err
	}

	if err := q.redis.RPush(ctx, q.queue, data).Err(); err != nil {
		return "", err
	}
	return job.ID, nil
}

func (q *JobQueue) QueueLength(ctx context.Context) int64 {
	if q == nil || q.redis == nil {
		return 0
	}
	length, err := q.redis.LLen(ctx, q.queue).Result()
	if err != nil {
		return 0
	}
	return length
}

func (q *JobQueue) StartWorkers(ctx context.Context, workers int, handler JobHandler) {
	if q == nil || q.redis == nil || handler == nil {
		return
	}
	if workers <= 0 {
		workers = 1
	}

	q.maybeRecoverInFlight(ctx)
	q.startHeartbeat(ctx)

	for i := 0; i < workers; i++ {
		go q.workerLoop(ctx, handler)
	}
}

func (q *JobQueue) startHeartbeat(ctx context.Context) {
	if q == nil || q.redis == nil {
		return
	}

	q.heartbeatOnce.Do(func() {
		q.publishHeartbeat(ctx)
		go func() {
			ticker := time.NewTicker(jobHeartbeatInterval)
			defer ticker.Stop()

			for {
				// Keep last-seen timestamp so stale-but-existing heartbeats can be detected.
				q.publishHeartbeat(ctx)

				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
				}
			}
		}()
	})
}

func (q *JobQueue) publishHeartbeat(ctx context.Context) {
	if q == nil || q.redis == nil {
		return
	}

	now := strconv.FormatInt(time.Now().UTC().Unix(), 10)
	// New format: one TTL key per worker instance to avoid ownership stomping.
	_ = q.redis.Set(ctx, q.instanceHeartbeatKey(), now, jobHeartbeatTTL).Err()
	// Keep legacy key during migration; only old workers read this key.
	_ = q.redis.Set(ctx, q.heartbeatKey, q.instanceID+":"+now, jobHeartbeatTTL).Err()
}

func (q *JobQueue) instanceHeartbeatKey() string {
	if q == nil {
		return ""
	}
	return q.heartbeatKeyPrefix + q.instanceID
}

func (q *JobQueue) maybeRecoverInFlight(ctx context.Context) {
	if q == nil || q.redis == nil {
		return
	}
	if q.hasOtherActiveWorkers(ctx) {
		return
	}

	lockToken := uuid.NewString()
	locked, err := q.redis.SetNX(ctx, q.recoveryLockKey, lockToken, jobHeartbeatTTL).Result()
	if err != nil || !locked {
		return
	}
	defer func() {
		cur, getErr := q.redis.Get(ctx, q.recoveryLockKey).Result()
		if getErr == nil && cur == lockToken {
			_ = q.redis.Del(ctx, q.recoveryLockKey).Err()
		}
	}()

	// Check again after lock acquisition to avoid racing with a worker that just started.
	if q.hasOtherActiveWorkers(ctx) {
		return
	}

	q.recoverInFlight(ctx)
}

func (q *JobQueue) hasFreshHeartbeat(ctx context.Context) bool {
	if q == nil || q.redis == nil {
		return false
	}
	iter := q.redis.Scan(ctx, 0, q.heartbeatKeyPrefix+"*", 50).Iterator()
	for iter.Next(ctx) {
		return true
	}
	if iter.Err() != nil {
		return false
	}
	fresh, _ := q.legacyHeartbeatStatus(ctx)
	return fresh
}

func (q *JobQueue) hasOtherActiveWorkers(ctx context.Context) bool {
	if q == nil || q.redis == nil {
		return false
	}

	// Primary signal: active per-instance heartbeat keys.
	ownKey := q.instanceHeartbeatKey()
	iter := q.redis.Scan(ctx, 0, q.heartbeatKeyPrefix+"*", 50).Iterator()
	for iter.Next(ctx) {
		key := strings.TrimSpace(iter.Val())
		if key == "" || key == ownKey {
			continue
		}
		return true
	}
	if iter.Err() != nil {
		// Fail-safe: on uncertainty, avoid destructive recovery.
		return true
	}

	// Compatibility fallback for stale deployments that still emit only the legacy key.
	fresh, owner := q.legacyHeartbeatStatus(ctx)
	if !fresh {
		return false
	}
	if owner == "" {
		return true
	}
	return owner != q.instanceID
}

func (q *JobQueue) legacyHeartbeatStatus(ctx context.Context) (fresh bool, owner string) {
	if q == nil || q.redis == nil {
		return false, ""
	}

	raw, err := q.redis.Get(ctx, q.heartbeatKey).Result()
	if err != nil {
		return false, ""
	}

	owner, unixTS, ok := parseHeartbeatValue(raw)
	if !ok || unixTS <= 0 {
		return false, owner
	}

	age := time.Since(time.Unix(unixTS, 0))
	if age < 0 {
		// Clock skew should not trigger destructive recovery behavior.
		return true, owner
	}
	return age <= jobHeartbeatFreshFor, owner
}

func parseHeartbeatValue(raw string) (owner string, unixTS int64, ok bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", 0, false
	}

	// New format: "<instanceID>:<unix_ts>"
	if parts := strings.SplitN(value, ":", 2); len(parts) == 2 {
		candidateOwner := strings.TrimSpace(parts[0])
		candidateTS, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
		if err != nil {
			return candidateOwner, 0, false
		}
		return candidateOwner, candidateTS, true
	}

	// Legacy format: "<unix_ts>"
	legacyTS, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return "", 0, false
	}
	return "", legacyTS, true
}

func (q *JobQueue) workerLoop(ctx context.Context, handler JobHandler) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		raw, err := q.redis.BRPopLPush(ctx, q.queue, q.processing, 5*time.Second).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) || errors.Is(err, context.Canceled) {
				continue
			}
			time.Sleep(250 * time.Millisecond)
			continue
		}
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		var job Job
		if err := json.Unmarshal([]byte(raw), &job); err != nil {
			_ = q.moveReservedTo(ctx, raw, raw, q.dlq, true)
			continue
		}

		if err := q.handleSafely(ctx, handler, job); err == nil {
			_ = q.ackReserved(ctx, raw)
			continue
		} else {
			job.Attempts++
			job.LastError = err.Error()

			payload, marshalErr := json.Marshal(job)
			if marshalErr != nil {
				_ = q.moveReservedTo(ctx, raw, raw, q.dlq, true)
				continue
			}

			if job.Attempts >= job.MaxAttempts {
				_ = q.moveReservedTo(ctx, raw, string(payload), q.dlq, true)
				continue
			}

			backoff := retryBackoff(job.Attempts)
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}

			if err := q.moveReservedTo(ctx, raw, string(payload), q.queue, false); err != nil {
				// Avoid silent drops: if requeue fails, try DLQ with enriched context.
				job.LastError = fmt.Sprintf("%s; requeue failed: %v", job.LastError, err)
				if enriched, mErr := json.Marshal(job); mErr == nil {
					_ = q.moveReservedTo(ctx, raw, string(enriched), q.dlq, true)
				}
			}
		}
	}
}

func (q *JobQueue) handleSafely(ctx context.Context, handler JobHandler, job Job) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			// Preserve panic details as an error so retry/DLQ flows can proceed.
			err = fmt.Errorf("job handler panic: %v\n%s", recovered, string(debug.Stack()))
		}
	}()
	return handler(ctx, job)
}

func (q *JobQueue) recoverInFlight(ctx context.Context) {
	if q == nil || q.redis == nil {
		return
	}

	for {
		_, err := q.redis.RPopLPush(ctx, q.processing, q.queue).Result()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				return
			}
			return
		}
	}
}

func (q *JobQueue) ackReserved(ctx context.Context, raw string) error {
	if q == nil || q.redis == nil {
		return errors.New("job queue unavailable")
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	return q.redis.LRem(ctx, q.processing, 1, raw).Err()
}

func (q *JobQueue) moveReservedTo(ctx context.Context, reservedRaw, payload, destination string, pushLeft bool) error {
	if q == nil || q.redis == nil {
		return errors.New("job queue unavailable")
	}
	if strings.TrimSpace(reservedRaw) == "" || strings.TrimSpace(destination) == "" {
		return errors.New("invalid move arguments")
	}

	pipe := q.redis.TxPipeline()
	pipe.LRem(ctx, q.processing, 1, reservedRaw)
	if pushLeft {
		pipe.LPush(ctx, destination, payload)
	} else {
		pipe.RPush(ctx, destination, payload)
	}
	_, err := pipe.Exec(ctx)
	return err
}

func retryBackoff(attempt int) time.Duration {
	if attempt <= 0 {
		return 250 * time.Millisecond
	}
	d := time.Duration(attempt*attempt) * time.Second
	if d > 30*time.Second {
		return 30 * time.Second
	}
	return d
}

func (q *JobQueue) DLQName() string {
	if q == nil {
		return ""
	}
	return q.dlq
}

func (q *JobQueue) Name() string {
	if q == nil {
		return ""
	}
	return q.queue
}

func MarshalJobPayload(v interface{}) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %w", err)
	}
	return json.RawMessage(data), nil
}
