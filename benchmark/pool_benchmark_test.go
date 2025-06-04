package benchmark

import (
	"fmt"
	"sync"
	"testing"
	"time"

	v1 "github.com/nyameteor/toy-workerpool/v1"
	v2 "github.com/nyameteor/toy-workerpool/v2"
	v3 "github.com/nyameteor/toy-workerpool/v3"
)

const (
	MinWorkers    = 0
	MaxWorkers    = 50_000
	QueueCapacity = 100_000
	IdleTimeout   = 100 * time.Millisecond
)

type workloadCase struct {
	name             string
	userCount        int           // number of concurrent users
	taskCountPerUser int           // tasks each user submits
	taskInterval     time.Duration // delay between each task submission
}

type taskFunc func()
type taskCase struct {
	name string
	task taskFunc
}

type poolSubmitFunc func(func())
type poolStopFunc func()
type poolSetupFunc func() (poolSubmitFunc, poolStopFunc)
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
	{
		name:             "U10K_T100",
		userCount:        10_000,
		taskCountPerUser: 100,
		taskInterval:     0,
	},
	{
		name:             "U1M_T1",
		userCount:        1000_000,
		taskCountPerUser: 1,
		taskInterval:     0,
	},
}

var taskCases = []taskCase{
	{
		name: "Sleep_10ms",
		task: func() {
			time.Sleep(10 * time.Millisecond)
		},
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
	},
}

var poolCases = []poolCase{
	{
		name: "RawGoroutines",
		setup: func() (poolSubmitFunc, poolStopFunc) {
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
		setup: func() (poolSubmitFunc, poolStopFunc) {
			var wg sync.WaitGroup
			taskQueue := make(chan func())

			for i := 0; i < MaxWorkers; i++ {
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
		setup: func() (poolSubmitFunc, poolStopFunc) {
			var wg sync.WaitGroup
			taskQueue := make(chan func(), QueueCapacity)

			for i := 0; i < MaxWorkers; i++ {
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
		setup: func() (poolSubmitFunc, poolStopFunc) {
			pool := v1.NewPool(MaxWorkers, QueueCapacity)
			return pool.Submit, pool.StopAndWait
		},
	},
	{
		name: "WorkerPool_V2",
		setup: func() (poolSubmitFunc, poolStopFunc) {
			pool := v2.NewPool(MaxWorkers, QueueCapacity)
			return pool.Submit, pool.StopAndWait
		},
	},
	{
		name: "WorkerPool_V3",
		setup: func() (poolSubmitFunc, poolStopFunc) {
			pool := v3.NewPool(MaxWorkers, QueueCapacity, v3.WithMinWorkers(MinWorkers), v3.WithIdleTimeout(IdleTimeout))
			return pool.Submit, pool.StopAndWait
		},
	},
}

func BenchmarkAll(b *testing.B) {
	for _, workloadCase := range workloadCases {
		for _, taskCase := range taskCases {
			for _, poolCase := range poolCases {
				testName := fmt.Sprintf("%s/%s/%s", workloadCase.name, taskCase.name, poolCase.name)
				b.Run(testName, func(b *testing.B) {
					b.ReportAllocs() // Enable memory allocation stats

					for i := 0; i < b.N; i++ {
						runWorkloadCase(&workloadCase, poolCase.setup, taskCase.task)
					}
				})
			}
		}
	}
}

func runWorkloadCase(workloadCase *workloadCase, poolSetup poolSetupFunc, task taskFunc) {
	poolSubmit, poolStop := poolSetup()

	var wg sync.WaitGroup

	wg.Add(workloadCase.userCount * workloadCase.taskCountPerUser)
	wrappedTask := func() {
		defer wg.Done()
		task()
	}

	for i := 0; i < workloadCase.userCount; i++ {
		go func() {
			for i := 0; i < workloadCase.taskCountPerUser; i++ {
				poolSubmit(wrappedTask)
				if workloadCase.taskInterval > 0 {
					time.Sleep(workloadCase.taskInterval)
				}
			}
		}()
	}

	wg.Wait()

	poolStop()
}
