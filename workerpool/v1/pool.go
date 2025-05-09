package v1

import (
	"sync"
)

// Pool manages a set of worker goroutines to run submitted tasks.
type Pool struct {
	tasks         chan func()
	maxWorkers    int
	queueCapacity int
	wg            sync.WaitGroup
	stopOnce      sync.Once
}

// NewPool creates a new Pool with the given number of workers and task queue capacity.
func NewPool(maxWorkers, queueCapacity int) *Pool {
	p := &Pool{
		tasks:         make(chan func(), queueCapacity),
		maxWorkers:    maxWorkers,
		queueCapacity: queueCapacity,
	}
	p.startWorkers()
	return p
}

// Submit adds a task to the pool. Blocks if the task queue is full. Ignores nil tasks.
func (p *Pool) Submit(task func()) {
	if task == nil {
		return
	}

	p.wg.Add(1)
	p.tasks <- func() {
		defer p.wg.Done()
		task()
	}
}

// Stop closes the task queue. No new tasks can be submitted after this.
func (p *Pool) Stop() {
	p.stopOnce.Do(func() {
		close(p.tasks)
	})
}

// Wait blocks until all submitted tasks are finished.
func (p *Pool) Wait() {
	p.wg.Wait()
}

// StopAndWait stops the pool and waits for all tasks to finish.
func (p *Pool) StopAndWait() {
	p.Stop()
	p.Wait()
}

// startWorkers launches the worker goroutines.
func (p *Pool) startWorkers() {
	for i := 0; i < p.maxWorkers; i++ {
		go func() {
			for task := range p.tasks {
				task()
			}
		}()
	}
}

// NewGroup creates a new TaskGroup associated with the pool.
func (p *Pool) NewGroup() *TaskGroup {
	return &TaskGroup{pool: p}
}

// TaskGroup allows tracking a batch of related tasks.
type TaskGroup struct {
	pool *Pool
	wg   sync.WaitGroup
}

// Submit adds a task to the group and schedules it in the pool.
func (g *TaskGroup) Submit(task func()) {
	if task == nil {
		return
	}

	g.wg.Add(1)
	g.pool.Submit(func() {
		defer g.wg.Done()
		task()
	})
}

// Wait blocks until all tasks in the group are done.
func (g *TaskGroup) Wait() {
	g.wg.Wait()
}
