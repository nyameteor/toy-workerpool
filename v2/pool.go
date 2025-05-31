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
	// Configuration options
	maxWorkers    int
	queueCapacity int
	panicHandler  func(r any)
	ctx           context.Context
	ctxCancel     context.CancelFunc

	// Internal state
	taskQueue chan func()
	wg        sync.WaitGroup
	stopOnce  sync.Once
	stopped   atomic.Bool
}

// NewPool creates a new Pool with the given number of workers and task queue capacity.
func NewPool(maxWorkers, queueCapacity int, options ...Option) *Pool {
	p := &Pool{
		taskQueue:     make(chan func(), queueCapacity),
		maxWorkers:    maxWorkers,
		queueCapacity: queueCapacity,
		panicHandler:  defaultPanicHandler,
	}

	for _, opt := range options {
		opt(p)
	}

	if p.ctx == nil {
		p.ctx, p.ctxCancel = context.WithCancel(context.Background())
	}

	p.startWorkers()
	return p
}

type Option func(*Pool)

// WithContext sets the parent context for the pool.
func WithContext(parentCtx context.Context) Option {
	return func(p *Pool) {
		p.ctx, p.ctxCancel = context.WithCancel(parentCtx)
	}
}

// WithPanicHandler sets a panic handler for the pool.
func WithPanicHandler(panicHandler func(r any)) Option {
	return func(p *Pool) {
		p.panicHandler = panicHandler
	}
}

// Stopped reports if the pool is closed and no longer accepts tasks.
func (p *Pool) Stopped() bool {
	return p.stopped.Load()
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
	p.taskQueue <- func() {
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
	p.stopOnce.Do(func() {
		p.stopped.Store(true)
		close(p.taskQueue)
	})
}

// StopAndWait stops the pool and waits for all tasks to finish.
func (p *Pool) StopAndWait() {
	p.Stop()
	p.wg.Wait()
}

// startWorkers launches the worker goroutines.
func (p *Pool) startWorkers() {
	for i := 0; i < p.maxWorkers; i++ {
		go worker(p.taskQueue, p.panicHandler)
	}
}

func worker(taskQueue chan func(), panicHandler func(r any)) {
	for task := range taskQueue {
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

// TaskGroup allows tracking a batch of related tasks.
type TaskGroup struct {
	pool      *Pool
	wg        sync.WaitGroup
	ctx       context.Context
	ctxCancel context.CancelFunc
}

// NewGroup creates a new TaskGroup.
func (p *Pool) NewGroup() *TaskGroup {
	return p.NewGroupContext(context.Background())
}

// NewGroupContext creates a new TaskGroup with the given context as its parent.
func (p *Pool) NewGroupContext(parentCtx context.Context) *TaskGroup {
	ctx, ctxCancel := context.WithCancel(parentCtx)
	return &TaskGroup{
		pool:      p,
		ctx:       ctx,
		ctxCancel: ctxCancel,
	}
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
	g.pool.taskQueue <- func() {
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
