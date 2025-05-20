package v2

import (
	"context"
	"sync"
)

// Pool manages a set of worker goroutines to run submitted tasks.
type Pool struct {
	tasks         chan func()
	maxWorkers    int
	queueCapacity int
	wg            sync.WaitGroup
	stopOnce      sync.Once
	ctx           context.Context
}

// NewPool creates a new Pool with the given number of workers and task queue capacity.
// If no context is set, context.Background() is used.
func NewPool(maxWorkers, queueCapacity int, options ...PoolOption) *Pool {
	p := &Pool{
		tasks:         make(chan func(), queueCapacity),
		maxWorkers:    maxWorkers,
		queueCapacity: queueCapacity,
	}

	for _, opt := range options {
		opt(p)
	}

	if p.ctx == nil {
		p.ctx = context.Background()
	}

	p.startWorkers()
	return p
}

// Submit adds a task to the pool. Blocks if the queue is full.
// Skips if the pool context is done, or if the task is nil.
func (p *Pool) Submit(task func()) {
	p.trySubmit(task)
}

func (p *Pool) trySubmit(task func()) (submitted bool) {
	if task == nil {
		return
	}

	if err := p.ctx.Err(); err != nil {
		return
	}

	wrappedTask := func() {
		defer p.wg.Done()
		if p.ctx.Err() != nil {
			return
		}
		task()
	}

	select {
	case p.tasks <- wrappedTask:
		p.wg.Add(1) // Add after successful enqueue
		submitted = true
		return
	case <-p.ctx.Done():
		return
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

type PoolOption func(*Pool)

// WithPoolContext sets a context for the pool.
func WithPoolContext(ctx context.Context) PoolOption {
	return func(p *Pool) {
		p.ctx = ctx
	}
}

// NewGroup creates a new TaskGroup with context.Background().
func (p *Pool) NewGroup() *TaskGroup {
	return p.NewGroupCtx(context.Background())
}

// NewGroupCtx creates a new TaskGroup with the given context.
func (p *Pool) NewGroupCtx(ctx context.Context) *TaskGroup {
	return &TaskGroup{
		pool: p,
		ctx:  ctx,
	}
}

// TaskGroup allows tracking a batch of related tasks.
type TaskGroup struct {
	pool *Pool
	wg   sync.WaitGroup
	ctx  context.Context
}

// Submit adds a task to the group and schedules it in the pool.
// Skips if the group or pool context is done, or if the task is nil.
func (g *TaskGroup) Submit(task func()) {
	if g.ctx.Err() != nil || g.pool.ctx.Err() != nil {
		return
	}

	wrappedTask := func() {
		defer g.wg.Done()
		if g.ctx.Err() != nil || g.pool.ctx.Err() != nil {
			return
		}
		task()
	}

	select {
	case g.pool.tasks <- wrappedTask:
		g.wg.Add(1)
	case <-g.ctx.Done():
		return
	case <-g.pool.ctx.Done():
		return
	}
}

// Wait blocks until all tasks in the group are done.
func (g *TaskGroup) Wait() {
	g.wg.Wait()
}
