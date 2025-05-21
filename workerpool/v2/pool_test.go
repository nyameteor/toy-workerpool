package v2

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nyameteor/learn-go-concurrency/internal/assert"
)

func TestPoolSubmit(t *testing.T) {
	pool := NewPool(100, 200)

	taskCount := 1000
	var executedCount atomic.Int64

	for i := 0; i < taskCount; i++ {
		pool.Submit(func() {
			time.Sleep(10 * time.Millisecond)
			executedCount.Add(1)
		})
	}

	pool.StopAndWait()

	assert.Equal(t, int64(taskCount), executedCount.Load())
}

func TestPoolSubmitNilTasks(t *testing.T) {
	pool := NewPool(100, 200)

	// Submit a batch of nil tasks (should be ignored)
	for i := 0; i < 100; i++ {
		pool.Submit(nil)
	}

	taskCount := 1000
	var executedCount atomic.Int64

	for i := 0; i < taskCount; i++ {
		pool.Submit(func() {
			time.Sleep(10 * time.Millisecond)
			executedCount.Add(1)
		})
	}

	pool.StopAndWait()

	assert.Equal(t, int64(taskCount), executedCount.Load())
}

func TestPoolSubmitNoTasks(t *testing.T) {
	pool := NewPool(100, 200)
	pool.StopAndWait()
}

func TestPoolContextSkipTasks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	pool := NewPool(5, 10, WithPoolContext(ctx))

	taskCount := 100
	var executedCount atomic.Int64

	for i := 0; i < taskCount; i++ {
		pool.Submit(func() {
			time.Sleep(10 * time.Millisecond)
			executedCount.Add(1)
		})
	}

	pool.StopAndWait()

	if executed := executedCount.Load(); executed >= int64(taskCount) {
		t.Errorf("expected fewer than %d tasks to run due to context cancel, got %d", taskCount, executed)
	}
}

func TestPoolSubmitContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	pool := NewPool(1, 5, WithPoolContext(ctx))

	var taskDoneCount, taskStartCount int32

	// Submit a long-running, cancellable task
	pool.Submit(func() {
		atomic.AddInt32(&taskStartCount, 1)
		select {
		case <-ctx.Done():
			return
		case <-time.After(1 * time.Minute):
			atomic.AddInt32(&taskDoneCount, 1)
			return
		}
	})

	// Cancel the context
	cancel()

	pool.StopAndWait()

	assert.Equal(t, int32(0), atomic.LoadInt32(&taskStartCount))
	assert.Equal(t, int32(0), atomic.LoadInt32(&taskDoneCount))
}

func TestGroupSubmit(t *testing.T) {
	pool := NewPool(100, 200)
	group := pool.NewGroup()

	taskCount := 1000
	var executedCount atomic.Int64

	for i := 0; i < taskCount; i++ {
		group.Submit(func() {
			time.Sleep(10 * time.Millisecond)
			executedCount.Add(1)
		})
	}

	group.Wait()
	pool.StopAndWait()

	assert.Equal(t, int64(taskCount), executedCount.Load())
}

func TestGroupContextSkipTasks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	pool := NewPool(5, 10)
	group := pool.NewGroupCtx(ctx)

	taskCount := 100
	var executedCount atomic.Int64

	for i := 0; i < taskCount; i++ {
		group.Submit(func() {
			time.Sleep(10 * time.Millisecond)
			executedCount.Add(1)
		})
	}

	group.Wait()
	pool.StopAndWait()

	if executed := executedCount.Load(); executed >= int64(taskCount) {
		t.Errorf("expected fewer than %d tasks to run due to context cancel, got %d", taskCount, executed)
	}
}

func TestGroupParentContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	pool := NewPool(5, 10, WithPoolContext(ctx))
	group := pool.NewGroupCtx(context.Background())

	taskCount := 100
	var executedCount atomic.Int64

	for i := 0; i < taskCount; i++ {
		group.Submit(func() {
			time.Sleep(10 * time.Millisecond)
			executedCount.Add(1)
		})
	}

	group.Wait()
	pool.StopAndWait()

	if executed := executedCount.Load(); executed >= int64(taskCount) {
		t.Errorf("expected fewer than %d tasks to run due to context cancel, got %d", taskCount, executed)
	}
}
