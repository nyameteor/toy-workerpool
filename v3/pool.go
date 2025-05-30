package v3

import (
	"context"
	"errors"
	"log"
	"sync"
	"sync/atomic"
	"time"
)

const (
	defaultMinWorkers  = 0
	defaultIdleTimeout = 5 * time.Second
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
	minWorkers    int
	maxWorkers    int
	queueCapacity int
	panicHandler  func(r any)
	idleTimeout   time.Duration
	ctx           context.Context
	ctxCancel     context.CancelFunc

	// Internal state
	taskQueue  chan func()
	mutex      sync.Mutex
	tasksWg    sync.WaitGroup
	workersWg  sync.WaitGroup
	purgerWg   sync.WaitGroup
	stopOnce   sync.Once
	stopSignal chan struct{}
	stopped    atomic.Bool

	// Atomic counters
	workerCount     atomic.Int32
	idleWorkerCount atomic.Int32
}

// NewPool creates a new Pool with the given number of workers and task queue capacity.
func NewPool(maxWorkers, queueCapacity int, options ...Option) *Pool {
	p := &Pool{
		minWorkers:    defaultMinWorkers,
		maxWorkers:    maxWorkers,
		queueCapacity: queueCapacity,
		idleTimeout:   defaultIdleTimeout,
		panicHandler:  defaultPanicHandler,
		stopSignal:    make(chan struct{}),
	}

	for _, opt := range options {
		opt(p)
	}

	if p.maxWorkers <= 0 {
		p.maxWorkers = 1
	}

	if p.minWorkers > p.maxWorkers {
		p.minWorkers = p.maxWorkers
	}

	if p.idleTimeout <= 0 {
		p.idleTimeout = defaultIdleTimeout
	}

	if p.ctx == nil {
		p.ctx, p.ctxCancel = context.WithCancel(context.Background())
	}

	if p.queueCapacity <= 0 {
		p.queueCapacity = 1
	}

	p.taskQueue = make(chan func(), p.queueCapacity)

	for i := 0; i < p.minWorkers; i++ {
		p.tryStartWorker()
	}

	p.startPurger()

	return p
}

type Option func(*Pool)

// WithMinWorkers sets the minimum number of workers.
func WithMinWorkers(minWorkers int) Option {
	return func(p *Pool) {
		p.minWorkers = minWorkers
	}
}

// WithIdleTimeout sets the idle timeout for workers.
func WithIdleTimeout(idleTimeout time.Duration) Option {
	return func(p *Pool) {
		p.idleTimeout = idleTimeout
	}
}

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

// Stopped reports if the pool has been stopped.
func (p *Pool) Stopped() bool {
	return p.stopped.Load()
}

// RunningWorkers returns the number of currently running workers.
func (p *Pool) RunningWorkers() int {
	return int(p.workerCount.Load())
}

// IdleWorkers returns the number of currently idle workers.
func (p *Pool) IdleWorkers() int {
	return int(p.idleWorkerCount.Load())
}

// PendingTasks returns the number of tasks waiting in the queue.
func (p *Pool) PendingTasks() int {
	return len(p.taskQueue)
}

// IdleTimeout returns the configured idle timeout.
func (p *Pool) IdleTimeout() time.Duration {
	return p.idleTimeout
}

// MinWorkers returns the configured minimum number of workers.
func (p *Pool) MinWorkers() int {
	return p.minWorkers
}

// MaxWorkers returns the configured maximum number of workers.
func (p *Pool) MaxWorkers() int {
	return p.maxWorkers
}

// QueueCapacity returns the capacity of the task queue.
func (p *Pool) QueueCapacity() int {
	return p.queueCapacity
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

	defer p.tryStartWorker()

	p.tasksWg.Add(1)
	p.taskQueue <- func() {
		defer p.tasksWg.Done()

		select {
		case <-p.ctx.Done():
			return
		default:
		}

		task()
	}
}

// Stop shuts down the pool without waiting for queued tasks to complete.
// Tasks currently being executed by workers will complete.
func (p *Pool) Stop() {
	p.stop(false)
}

// StopAndWait shuts down the pool after all queued tasks have been completed.
func (p *Pool) StopAndWait() {
	p.stop(true)
}

// stop shuts down the pool, optionally waiting for queued tasks to complete.
// It stops all workers and the purger, cancels the context, and closes the task queue.
func (p *Pool) stop(waitQueueTasksComplete bool) {
	p.stopOnce.Do(func() {
		p.stopped.Store(true)

		if waitQueueTasksComplete {
			p.tasksWg.Wait()
		}

		p.resetWorkerSlot()

		p.ctxCancel()

		close(p.stopSignal)

		p.purgerWg.Wait()
		p.workersWg.Wait()

		close(p.taskQueue)
	})
}

func (p *Pool) tryStartWorker() bool {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// check if it's safe to start a new worker
	if p.RunningWorkers() >= p.MaxWorkers() || p.IdleWorkers() > 0 ||
		p.PendingTasks() == 0 || p.Stopped() {
		return false
	}

	// acquire worker slot
	p.workerCount.Add(1)

	p.workersWg.Add(1)
	go func() {
		defer p.workersWg.Done()
		worker(p.taskQueue, p.stopSignal, p.panicHandler, &p.idleWorkerCount)
	}()

	return true
}

func (p *Pool) tryStopIdleWorker() bool {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	// check if it's safe to stop an idle worker
	if p.RunningWorkers() <= p.MinWorkers() ||
		p.IdleWorkers() <= 0 || p.Stopped() {
		return false
	}

	// release worker slot
	p.workerCount.Add(-1)
	p.idleWorkerCount.Add(-1)

	// send stop signal (nil task) to an idle worker
	select {
	case p.taskQueue <- nil:
		return true
	default:
		// queue full, rollback slot release
		p.workerCount.Add(1)
		p.idleWorkerCount.Add(1)
		return false
	}
}

func (p *Pool) resetWorkerSlot() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	p.workerCount.Store(0)
	p.idleWorkerCount.Store(0)
}

func worker(taskQueue chan func(), stopSignal chan struct{}, panicHandler func(r any), idleWorkerCount *atomic.Int32) {
	for {
		idleWorkerCount.Add(1)
		select {
		case <-stopSignal:
			return
		case task := <-taskQueue:
			idleWorkerCount.Add(-1)

			if task == nil {
				// receive stop signal for idle worker
				return
			}

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
}

func (p *Pool) startPurger() {
	p.purgerWg.Add(1)
	go func() {
		defer p.purgerWg.Done()
		purger(p.idleTimeout, p.stopSignal, func() {
			p.tryStopIdleWorker()
		})
	}()
}

func purger(idleTimeout time.Duration, stopSignal chan struct{}, tryStopIdleWorker func()) {
	ticker := time.NewTicker(idleTimeout)
	defer ticker.Stop()

	for {
		select {
		case <-stopSignal:
			return
		case <-ticker.C:
			tryStopIdleWorker()
		}
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

	defer g.pool.tryStartWorker()

	g.wg.Add(1)
	g.pool.tasksWg.Add(1)
	g.pool.taskQueue <- func() {
		defer g.pool.tasksWg.Done()
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
