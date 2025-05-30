package v3

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nyameteor/toy-workerpool/internal/assert"
)

func TestSubmit(t *testing.T) {
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

func TestSubmitNilTasks(t *testing.T) {
	pool := NewPool(10, 20)

	// Submit a batch of nil tasks (should be ignored)
	for i := 0; i < 1000; i++ {
		pool.Submit(nil)
	}

	assert.Equal(t, 0, pool.PendingTasks())
	assert.Equal(t, 0, pool.RunningWorkers())

	pool.StopAndWait()
}

func TestSubmitNoTasks(t *testing.T) {
	pool := NewPool(100, 200)
	pool.StopAndWait()
}

func TestSubmitPanic(t *testing.T) {
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

func TestSubmitToStoppedPool(t *testing.T) {
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

func TestContextSkipTasks(t *testing.T) {
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

func TestContextCancel(t *testing.T) {
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

func TestStop(t *testing.T) {
	pool := NewPool(5, 100)

	taskCount := 100
	var executedCount atomic.Int64
	started := make(chan struct{}, taskCount)

	for i := 0; i < taskCount; i++ {
		pool.Submit(func() {
			started <- struct{}{}
			time.Sleep(50 * time.Millisecond)
			executedCount.Add(1)
		})
	}

	// Wait until at least 10 tasks have started
	for i := 0; i < 10; i++ {
		<-started
	}

	pool.Stop()

	assert.Equal(t, 0, pool.RunningWorkers())

	executed := executedCount.Load()
	if executed >= int64(taskCount) {
		t.Errorf("expected some tasks to be skipped after Stop(), but all %d were executed", taskCount)
	} else if executed == 0 {
		t.Errorf("expected some tasks to complete before Stop(), but none did")
	} else {
		t.Logf("executed %d/%d tasks before shutdown", executed, taskCount)
	}
}

func TestStopWithPurging(t *testing.T) {
	pool := NewPool(5, 5, WithIdleTimeout(20*time.Millisecond))

	// Submit a task
	for i := 0; i < 10; i++ {
		pool.Submit(func() {
			time.Sleep(10 * time.Millisecond)
		})
	}

	// Purge goroutine is clearing idle workers
	time.Sleep(200 * time.Millisecond)

	// Stop the pool to make sure there is no data race with purge goroutine
	pool.StopAndWait()

	assert.Equal(t, 0, pool.RunningWorkers())
}

func TestWorkersNotExceedLimit(t *testing.T) {
	const (
		minWorkers    = 5
		maxWorkers    = 10
		idleTimeout   = 10 * time.Millisecond
		burstRounds   = 3
		tasksPerBurst = 50
	)

	pool := NewPool(maxWorkers, 100, WithMinWorkers(minWorkers), WithIdleTimeout(idleTimeout))

	// Allow time for the pool to start initial workers
	time.Sleep(100 * time.Millisecond)

	var maxObserved atomic.Int32
	var minObserved atomic.Int32
	minObserved.Store(math.MaxInt32)

	logCh := make(chan string, 1000)
	done := make(chan struct{})

	// Start logger (safe t.Log use)
	// go func() {
	// 	for msg := range logCh {
	// 		t.Log(msg)
	// 	}
	// }()

	// Start worker count observer
	go func() {
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				cur := int32(pool.RunningWorkers())
				if cur > maxObserved.Load() {
					maxObserved.Store(cur)
				}
				if cur < minObserved.Load() {
					minObserved.Store(cur)
				}
				logCh <- fmt.Sprintf("Current workers: %d", cur)
			}
		}
	}()

	// Submit bursts of short-lived tasks
	for r := 0; r < burstRounds; r++ {
		for i := 0; i < tasksPerBurst; i++ {
			pool.Submit(func() {
				time.Sleep(10 * time.Millisecond)
			})
		}
		// Allow idle timeout to trigger purging between bursts
		time.Sleep(10 * idleTimeout)
	}

	close(done)
	pool.StopAndWait()

	if maxObserved.Load() > int32(maxWorkers) {
		t.Errorf("workers exceeded maxWorkers limit: %d, maxObserved: %d", maxWorkers, maxObserved.Load())
	}
	if minObserved.Load() < int32(minWorkers) {
		t.Errorf("workers dropped below minWorkers limit: %d, minObserved: %d", minWorkers, minObserved.Load())
	}

	t.Logf("Max observed workers: %d", maxObserved.Load())
	t.Logf("Min observed workers: %d", minObserved.Load())
}

func TestNoWorkerLeak(t *testing.T) {
	before := runtime.NumGoroutine()

	pool := NewPool(100, 200)
	for i := 0; i < 1000; i++ {
		pool.Submit(func() {
			time.Sleep(10 * time.Millisecond)
		})
	}
	pool.Stop()

	// Give time for all goroutines to exit
	time.Sleep(200 * time.Millisecond)

	after := runtime.NumGoroutine()
	diff := after - before
	// Go runtime may add/remove a goroutine or two in the background (GC, timers, etc.)
	if diff > 2 {
		t.Errorf("Potential goroutine leak: before=%d, after=%d, diff=%d", before, after, diff)
	}
}

func TestNewWithPanicHandler(t *testing.T) {
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

func TestNewWithInvalidOptions(t *testing.T) {
	pool := NewPool(-10, -5, WithMinWorkers(20), WithIdleTimeout(-1*time.Second))
	assert.Equal(t, 1, pool.MaxWorkers())
	assert.Equal(t, 1, pool.QueueCapacity())
	assert.Equal(t, 1, pool.MinWorkers())
	assert.Equal(t, defaultIdleTimeout, pool.IdleTimeout())
}

func TestTryStopIdleWorkerRollback(t *testing.T) {
	const (
		maxWorkers    = 2
		queueCapacity = 1
	)

	pool := NewPool(maxWorkers, queueCapacity, WithMinWorkers(0))

	ready := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())

	// Submit a long-running task to occupy one worker
	pool.Submit(func() {
		close(ready)
		<-ctx.Done()
	})

	// ensure task is running
	<-ready

	// fill the task queue to capacity
	pool.taskQueue <- func() {}

	// simulate 1 idle worker by manually setting counters
	atomic.StoreInt32(&pool.idleWorkerCount, 1)
	atomic.StoreInt32(&pool.workerCount, 1)

	// should fail due to full queue and rollback
	ok := pool.tryStopIdleWorker()

	cancel()

	assert.Equal(t, false, ok)
	assert.Equal(t, int32(1), atomic.LoadInt32(&pool.workerCount))
	assert.Equal(t, int32(1), atomic.LoadInt32(&pool.idleWorkerCount))

	pool.Stop()
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

func TestGroupSubmitNilTasks(t *testing.T) {
	pool := NewPool(10, 20)
	group := pool.NewGroup()

	// Submit a batch of nil tasks (should be ignored)
	for i := 0; i < 1000; i++ {
		group.Submit(nil)
	}

	assert.Equal(t, 0, pool.PendingTasks())
	assert.Equal(t, 0, pool.RunningWorkers())

	pool.StopAndWait()
}

func TestGroupSubmitToStoppedPool(t *testing.T) {
	pool := NewPool(0, 1)
	group := pool.NewGroup()
	assert.Equal(t, pool.Stopped(), false)
	pool.Stop()
	assert.Equal(t, pool.Stopped(), true)

	var err any = nil
	func() {
		defer func() {
			err = recover()
		}()
		group.Submit(func() {})
	}()

	assert.Equal(t, ErrPoolStopped, err)
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
