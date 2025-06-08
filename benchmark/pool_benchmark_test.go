package benchmark

import (
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	v1 "github.com/nyameteor/toy-workerpool/v1"
	v2 "github.com/nyameteor/toy-workerpool/v2"
	v3 "github.com/nyameteor/toy-workerpool/v3"
)

type workloadCase struct {
	name             string
	userCount        int           // number of concurrent users
	taskCountPerUser int           // tasks each user submits
	taskInterval     time.Duration // delay between each task submission
}

type taskFunc func()
type taskKind int

const (
	taskUnknown taskKind = iota
	taskCPUBound
	taskIOBound
)

type taskCase struct {
	name string
	task taskFunc
	kind taskKind
}

type poolConfig struct {
	minWorkers    int
	maxWorkers    int
	queueCapacity int
	idleTimeout   time.Duration
}

type poolSubmitFunc func(func())
type poolStopFunc func()
type poolSetupFunc func(poolConfig) (poolSubmitFunc, poolStopFunc)
type poolCase struct {
	name  string
	setup poolSetupFunc
}

var workloadCases = []workloadCase{
	{
		name:             "U1_T1M",
		userCount:        1,
		taskCountPerUser: 1000_000,
		taskInterval:     0,
	},
	{
		name:             "U100_T10K",
		userCount:        100,
		taskCountPerUser: 10_000,
		taskInterval:     0,
	},
	{
		name:             "U1K_T1K",
		userCount:        1000,
		taskCountPerUser: 1000,
		taskInterval:     0,
	},
	// Cases with high userCount (e.g., 10K or 1M) may skew benchmark results
	// due to goroutine overhead and are currently not recommended.
}

var taskCases = []taskCase{
	{
		name: "Sleep_10ms",
		task: func() {
			time.Sleep(10 * time.Millisecond)
		},
		kind: taskIOBound,
	},
	{
		name: "IsPrime_2to100",
		task: func() {
			for i := 2; i <= 100; i++ {
				isPrime := true
				for j := 2; j*j <= i; j++ {
					if i%j == 0 {
						isPrime = false
						break
					}
				}
				_ = isPrime
			}
		},
		kind: taskCPUBound,
	},
	{
		name: "Fibonacci_15",
		task: func() {
			var fib func(n int) int
			fib = func(n int) int {
				if n <= 1 {
					return n
				}
				return fib(n-1) + fib(n-2)
			}
			_ = fib(15)
		},
		kind: taskCPUBound,
	},
}

var poolCases = []poolCase{
	{
		name: "RawGoroutines",
		setup: func(_ poolConfig) (poolSubmitFunc, poolStopFunc) {
			var wg sync.WaitGroup

			submit := func(task func()) {
				wg.Add(1)
				go func() {
					defer wg.Done()
					task()
				}()
			}

			stop := func() {
				wg.Wait()
			}

			return submit, stop
		},
	},
	{
		name: "UnbufferedPool",
		setup: func(cfg poolConfig) (poolSubmitFunc, poolStopFunc) {
			var wg sync.WaitGroup
			taskQueue := make(chan func())

			for i := 0; i < cfg.maxWorkers; i++ {
				go func() {
					for task := range taskQueue {
						task()
					}
				}()
			}

			submit := func(task func()) {
				wg.Add(1)
				taskQueue <- func() {
					defer wg.Done()
					task()
				}
			}

			stop := func() {
				close(taskQueue)
				wg.Wait()
			}

			return submit, stop
		},
	},
	{
		name: "BufferedPool",
		setup: func(cfg poolConfig) (poolSubmitFunc, poolStopFunc) {
			var wg sync.WaitGroup
			taskQueue := make(chan func(), cfg.queueCapacity)

			for i := 0; i < cfg.maxWorkers; i++ {
				go func() {
					for task := range taskQueue {
						task()
					}
				}()
			}

			submit := func(task func()) {
				wg.Add(1)
				taskQueue <- func() {
					defer wg.Done()
					task()
				}
			}

			stop := func() {
				close(taskQueue)
				wg.Wait()
			}

			return submit, stop
		},
	},
	{
		name: "WorkerPool_V1",
		setup: func(cfg poolConfig) (poolSubmitFunc, poolStopFunc) {
			pool := v1.NewPool(cfg.maxWorkers, cfg.queueCapacity)
			return pool.Submit, pool.StopAndWait
		},
	},
	{
		name: "WorkerPool_V2",
		setup: func(cfg poolConfig) (poolSubmitFunc, poolStopFunc) {
			pool := v2.NewPool(cfg.maxWorkers, cfg.queueCapacity)
			return pool.Submit, pool.StopAndWait
		},
	},
	{
		name: "WorkerPool_V3",
		setup: func(cfg poolConfig) (poolSubmitFunc, poolStopFunc) {
			pool := v3.NewPool(cfg.maxWorkers, cfg.queueCapacity, v3.WithMinWorkers(cfg.minWorkers), v3.WithIdleTimeout(cfg.idleTimeout))
			return pool.Submit, pool.StopAndWait
		},
	},
}

func BenchmarkIOBoundTasks(b *testing.B) {
	taskIOBoundCases := filter(taskCases, func(t taskCase) bool { return t.kind == taskIOBound })
	runBenchmark(b, workloadCases, taskIOBoundCases, poolCases)
}

func BenchmarkCPUBoundTasks(b *testing.B) {
	taskCPUBoundCases := filter(taskCases, func(t taskCase) bool { return t.kind == taskCPUBound })
	runBenchmark(b, workloadCases, taskCPUBoundCases, poolCases)
}

func runBenchmark(b *testing.B, workloadCases []workloadCase, taskCases []taskCase, poolCases []poolCase) {
	for _, workloadCase := range workloadCases {
		for _, taskCase := range taskCases {
			for _, poolCase := range poolCases {
				testName := fmt.Sprintf("%s/%s/%s", workloadCase.name, taskCase.name, poolCase.name)
				b.Run(testName, func(b *testing.B) {
					b.ReportAllocs() // Enable memory allocation stats

					for i := 0; i < b.N; i++ {
						runWorkloadCase(workloadCase, poolCase.setup, taskCase.task, taskCase.kind)
					}
				})
			}
		}
	}
}

func runWorkloadCase(w workloadCase, setup poolSetupFunc, task taskFunc, kind taskKind) {
	config := derivePoolConfig(w, kind)
	poolSubmit, poolStop := setup(config)

	var wg sync.WaitGroup

	wg.Add(w.userCount * w.taskCountPerUser)
	wrappedTask := func() {
		defer wg.Done()
		task()
	}

	for i := 0; i < w.userCount; i++ {
		go func() {
			for i := 0; i < w.taskCountPerUser; i++ {
				poolSubmit(wrappedTask)
				if w.taskInterval > 0 {
					time.Sleep(w.taskInterval)
				}
			}
		}()
	}

	wg.Wait()

	poolStop()
}

func derivePoolConfig(w workloadCase, kind taskKind) poolConfig {
	const baseIdleTimeout = 100 * time.Millisecond
	totalTasks := w.userCount * w.taskCountPerUser
	numCPU := runtime.NumCPU()

	switch kind {
	case taskCPUBound:
		return poolConfig{
			minWorkers:    numCPU,
			maxWorkers:    numCPU * 2,
			queueCapacity: totalTasks / 10,
			idleTimeout:   baseIdleTimeout,
		}

	case taskIOBound:
		return poolConfig{
			minWorkers:    numCPU * 10,
			maxWorkers:    totalTasks / 10,
			queueCapacity: totalTasks / 4,
			idleTimeout:   baseIdleTimeout,
		}

	default: // unknown task kind
		return poolConfig{
			minWorkers:    numCPU,
			maxWorkers:    numCPU * 4,
			queueCapacity: totalTasks / 10,
			idleTimeout:   baseIdleTimeout,
		}
	}
}

func filter[T any](slice []T, filterFunc func(T) bool) []T {
	filteredSlice := []T{}
	for _, v := range slice {
		if filterFunc(v) {
			filteredSlice = append(filteredSlice, v)
		}
	}
	return filteredSlice
}
