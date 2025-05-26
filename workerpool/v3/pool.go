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
	// Atomic counters, placed first for proper alignment
	workerCount     int32
	idleWorkerCount int32

	// Configuration options
	minWorkers    int
	maxWorkers    int
	queueCapacity int
	ctx           context.Context
	ctxCancel     context.CancelFunc
	idleTimeout   time.Duration
	panicHandler  func(r any)

	// Internal state
	taskQueue  chan func()
	mutex      sync.Mutex
	tasksWg    sync.WaitGroup
	workersWg  sync.WaitGroup
	purgerWg   sync.WaitGroup
	stopOnce   sync.Once
	stopSignal chan struct{}
	stopped    int32
}

// NewPool creates a new Pool with the given number of workers and task queue capacity.
// If no context is set, context.Background() is used.
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

// Stopped reports if the pool is closed and no longer accepts tasks.
func (p *Pool) Stopped() bool {
	return atomic.LoadInt32(&p.stopped) == 1
}

func (p *Pool) RunningWorkers() int {
	return int(atomic.LoadInt32(&p.workerCount))
}

func (p *Pool) IdleWorkers() int {
	return int(atomic.LoadInt32(&p.idleWorkerCount))
}

func (p *Pool) PendingTasks() int {
	return len(p.taskQueue)
}

func (p *Pool) IdleTimeout() time.Duration {
	return p.idleTimeout
}

func (p *Pool) MinWorkers() int {
	return p.minWorkers
}

func (p *Pool) MaxWorkers() int {
	return p.maxWorkers
}

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

// Stop closes the task queue. No new tasks can be submitted after this.
func (p *Pool) Stop() {
	p.stop(false)
}

// StopAndWait stops the pool and waits for all tasks to finish.
func (p *Pool) StopAndWait() {
	p.stop(true)
}

func (p *Pool) stop(waitQueueTasksComplete bool) {
	p.stopOnce.Do(func() {
		atomic.StoreInt32(&p.stopped, 1)

		if waitQueueTasksComplete {
			p.tasksWg.Wait()
		}

		p.resetWorkerCount()

		p.ctxCancel()

		close(p.stopSignal)

		p.purgerWg.Wait()
		p.workersWg.Wait()

		close(p.taskQueue)
	})
}

func (p *Pool) tryStartWorker() bool {
	if !p.tryIncWorkerCount() {
		return false
	}

	go func() {
		defer p.workersWg.Done()
		worker(p.taskQueue, p.stopSignal, p.panicHandler, &p.idleWorkerCount)
	}()

	return true
}

func (p *Pool) tryStopIdleWorker() bool {
	if !p.tryDecWorkerCount() {
		return false
	}

	select {
	// Only purge if we can send to the task queue
	case p.taskQueue <- nil:
		// Send signal to stop an idle worker
		return true
	default:
		// Queue full: skip purge attempt
		// (roll back the decrement)
		atomic.AddInt32(&p.workerCount, 1)
		atomic.AddInt32(&p.idleWorkerCount, 1)
		return false
	}
}

func (p *Pool) tryIncWorkerCount() bool {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.RunningWorkers() >= p.MaxWorkers() {
		return false
	}

	if p.PendingTasks() == 0 || p.IdleWorkers() > 0 || p.Stopped() {
		return false
	}

	// Increment running worker count *before* starting the goroutine to
	// ensure the count is accurate and prevent exceeding MaxWorkers
	// due to race conditions between checking and actual worker startup.
	atomic.AddInt32(&p.workerCount, 1)

	p.workersWg.Add(1)

	return true
}

func (p *Pool) tryDecWorkerCount() bool {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	if p.RunningWorkers() <= p.MinWorkers() {
		return false
	}

	if p.IdleWorkers() <= 0 || p.Stopped() {
		return false
	}

	// Decrement running worker count *before* stop the goroutine to
	// ensure the count is accurate and prevent exceeding MinWorkers
	// due to race conditions between checking and actual worker stop.
	atomic.AddInt32(&p.workerCount, -1)

	atomic.AddInt32(&p.idleWorkerCount, -1)

	return true
}

func (p *Pool) resetWorkerCount() {
	p.mutex.Lock()
	defer p.mutex.Unlock()

	atomic.StoreInt32(&p.workerCount, 0)

	atomic.StoreInt32(&p.idleWorkerCount, 0)
}

func worker(taskQueue chan func(), stopSignal chan struct{}, panicHandler func(r any), idleWorkerCount *int32) {
	for {
		atomic.AddInt32(idleWorkerCount, 1)
		select {
		case <-stopSignal:
			return
		case task := <-taskQueue:
			atomic.AddInt32(idleWorkerCount, -1)

			if task == nil {
				// Shutdown signal for idle worker
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

type Option func(*Pool)

func WithMinWorkers(minWorkers int) Option {
	return func(p *Pool) {
		p.minWorkers = minWorkers
	}
}

func WithIdleTimeout(idleTimeout time.Duration) Option {
	return func(p *Pool) {
		p.idleTimeout = idleTimeout
	}
}

// WithContext sets a context for the pool.
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

// NewGroup creates a new TaskGroup with context.Background().
func (p *Pool) NewGroup() *TaskGroup {
	return p.NewGroupContext(context.Background())
}

// NewGroupContext creates a new TaskGroup with the given context.
func (p *Pool) NewGroupContext(parentCtx context.Context) *TaskGroup {
	ctx, ctxCancel := context.WithCancel(parentCtx)
	return &TaskGroup{
		pool:      p,
		ctx:       ctx,
		ctxCancel: ctxCancel,
	}
}

// TaskGroup allows tracking a batch of related tasks.
type TaskGroup struct {
	pool      *Pool
	wg        sync.WaitGroup
	ctx       context.Context
	ctxCancel context.CancelFunc
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
