package v2

import (
	"context"
	"fmt"
	"sync"
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

func TestPoolSubmitPanic(t *testing.T) {
	pool := NewPool(100, 200)

	taskCount := 1000
	var executedCount, panicCount atomic.Int64

	for i := 0; i < taskCount; i++ {
		pool.Submit(func() {
			idx := i // capture
			if idx >= 100 && idx <= 102 {
				panicCount.Add(1)
				panic(fmt.Sprintf("task %d panic", idx))
			}
			time.Sleep(10 * time.Millisecond)
			executedCount.Add(1)
		})
	}

	pool.StopAndWait()

	assert.Equal(t, int64(taskCount), executedCount.Load()+panicCount.Load())
}

func TestSubmitOnStoppedPool(t *testing.T) {
	// Create a pool and stop it immediately
	pool := NewPool(1, 0)
	assert.Equal(t, false, pool.Stopped())
	pool.StopAndWait()
	assert.Equal(t, true, pool.Stopped())

	// Attempt to submit a task on a stopped pool
	var err any = nil
	func() {
		defer func() {
			err = recover()
		}()
		pool.Submit(func() {})
	}()

	assert.Equal(t, ErrPoolStopped, err)
}

func TestPoolWithCustomPanicHandler(t *testing.T) {
	var (
		mu            sync.Mutex
		capturedPanic any = nil
	)

	panicHandler := func(r any) {
		mu.Lock()
		defer mu.Unlock()
		capturedPanic = r
	}

	pool := NewPool(1, 5, WithPanicHandler(panicHandler))

	// Submit a task that panics
	pool.Submit(func() {
		panic("panic now!")
	})

	pool.StopAndWait()

	mu.Lock()
	defer mu.Unlock()
	// Panic should have been captured
	assert.Equal(t, "panic now!", capturedPanic)
}

func TestPoolContextSkipTasks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	pool := NewPool(5, 10, WithContext(ctx))

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

	pool := NewPool(1, 5, WithContext(ctx))

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
	group := pool.NewGroupContext(ctx)

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

	pool := NewPool(5, 10, WithContext(ctx))
	group := pool.NewGroupContext(context.Background())

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
