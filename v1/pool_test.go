package v1

import (
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

func TestSubmitNoTasks(t *testing.T) {
	pool := NewPool(100, 200)
	pool.StopAndWait()
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
