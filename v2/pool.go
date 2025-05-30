package v2

import (
	"context"
	"errors"
	"log"
	"sync"
	"sync/atomic"
)

var (
	ErrPoolStopped = errors.New("worker pool is stopped and no longer accepts tasks")
)

func defaultPanicHandler(r any) {
	log.Printf("Worker panic recovered: %v\n", r)
}

// Pool manages a set of worker goroutines to run submitted tasks.
type Pool struct {
	tasks         chan func()
	maxWorkers    int
	queueCapacity int
	wg            sync.WaitGroup
	stopOnce      sync.Once
	stopped       int32
	ctx           context.Context
	panicHandler  func(r any)
}

// NewPool creates a new Pool with the given number of workers and task queue capacity.
// If no context is set, context.Background() is used.
func NewPool(maxWorkers, queueCapacity int, options ...Option) *Pool {
	p := &Pool{
		tasks:         make(chan func(), queueCapacity),
		maxWorkers:    maxWorkers,
		queueCapacity: queueCapacity,
		panicHandler:  defaultPanicHandler,
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
	if task == nil {
		return
	}

	if p.Stopped() {
		panic(ErrPoolStopped)
	}

	p.wg.Add(1)
	p.tasks <- func() {
		defer p.wg.Done()

		select {
		case <-p.ctx.Done():
			return
		default:
		}

		task()
	}
}

// Stop closes the task queue. No new tasks can be submitted after this.
func (p *Pool) Stop() {
	atomic.StoreInt32(&p.stopped, 1)

	p.stopOnce.Do(func() {
		close(p.tasks)
	})
}

// Stopped reports if the pool is closed and no longer accepts tasks.
func (p *Pool) Stopped() bool {
	return atomic.LoadInt32(&p.stopped) == 1
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
		go worker(p.tasks, p.panicHandler)
	}
}

func worker(tasks chan func(), panicHandler func(r any)) {
	for task := range tasks {
		func() {
			defer func() {
				if r := recover(); r != nil {
					panicHandler(r)
				}
			}()
			task()
		}()
	}
}

type Option func(*Pool)

// WithContext sets a context for the pool.
func WithContext(ctx context.Context) Option {
	return func(p *Pool) {
		p.ctx = ctx
	}
}

// WithPanicHandler sets a panic handler for the pool.
func WithPanicHandler(panicHandler func(r any)) Option {
	return func(p *Pool) {
		p.panicHandler = panicHandler
	}
}

// NewGroup creates a new TaskGroup with context.Background().
func (p *Pool) NewGroup() *TaskGroup {
	return p.NewGroupContext(context.Background())
}

// NewGroupContext creates a new TaskGroup with the given context.
func (p *Pool) NewGroupContext(ctx context.Context) *TaskGroup {
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
	if task == nil {
		return
	}

	if g.pool.Stopped() {
		panic(ErrPoolStopped)
	}

	g.wg.Add(1)
	g.pool.wg.Add(1)
	g.pool.tasks <- func() {
		defer g.pool.wg.Done()
		defer g.wg.Done()

		select {
		case <-g.ctx.Done():
			return
		case <-g.pool.ctx.Done():
			return
		default:
		}

		task()
	}
}

// Wait blocks until all tasks in the group are done.
func (g *TaskGroup) Wait() {
	g.wg.Wait()
}
