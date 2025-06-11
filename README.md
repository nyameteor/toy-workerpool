# toy-workerpool

A toy project to learn and build worker pools in Go from scratch.

## Table of Contents

- [Roadmap](#roadmap)
- [Installation](#installation)
- [Usage](#usage)
- [Tests](#tests)
- [Benchmarks](#benchmarks)
- [Concepts and Goals](#concepts-and-goals)
- [Design and Strategies](#design-and-strategies)
- [References](#references)

## Roadmap

This project is a simple way to learn how worker pools work in Go. It explores concurrency, task scheduling, and worker management step by step through multiple versions.

### Basic Fixed-Size Pool

Source: [v1/pool.go](v1/pool.go) (about 100 lines)

- Fixed-size worker pool with buffered task queue.
- Simple APIs to submit tasks and wait for all to complete.

### Cancellation and Panic Recovery

Source: [v2/pool.go](v2/pool.go) (about 200 lines)

- Adds `context.Context` support for canceling tasks.
- Adds panic recovery inside workers.

### Dynamic Worker Scaling

Source: [v3/pool.go](v3/pool.go) (about 400 lines)

- Starts new workers on demand during task submission.
- Stops idle workers with an idle timeout option.
- Adds APIs to query pool status.

### Dedicated Queues (TODO)

Switches to dedicated queues (push model) for better scalability.

### Task Results and Error Handling (TODO)

Adds support for returning task results and errors (Future-like interface).

## Installation

```sh
go get github.com/nyameteor/toy-workerpool
```

## Usage

> **Note**: This is a toy project, not intended for production. See [recommended projects](#repositories) for real-world use.

Each version has slight API differences, but the core usage is similar. See code comments for details.

```go
package main

import (
	"fmt"
	"time"

	v1 "github.com/nyameteor/toy-workerpool/v1"
)

func main() {
	// Create a worker pool with 5 workers and a queue capacity of 10
	pool := v1.NewPool(5, 10)

	// Submit 10 tasks to the pool
	for i := 0; i < 10; i++ {
		i := i
		pool.Submit(func() {
			time.Sleep(100 * time.Millisecond)
			fmt.Printf("task #%d done\n", i)
		})
	}

	// Wait for all tasks to finish and shut down the pool
	pool.StopAndWait()
}
```

## Tests

Run all unit tests with race detection:

```sh
go test -race ./...
```

## Benchmarks

Run all benchmarks:

```sh
go test -bench=. ./benchmark
```

See the [benchmark summary](https://gist.github.com/nyameteor/cdda38fef777084c31c5f3beec4c02d8) for full results and observations.

## Concepts and Goals

### What Is a Worker Pool?

A [worker pool][1] is a software design pattern for achieving concurrency of execution in a computer program.

It's designed to:

- **Bounded concurrency**: Limit the number of active workers to avoid resource exhaustion.
- **Efficient resource usage**: Reuse worker instances rather than spawning one per task.
- **Throughput and load balancing**: Spread work evenly across multiple workers.

A worker pool is **not typically** a [general-purpose scheduler][2]:

- It runs at the **application level**, above runtime schedulers (e.g., Go scheduler, Java virtual threads) at the **runtime level**, and OS schedulers at the **kernel level**.
- It typically acts as a **concurrency limiter** and **task dispatcher**, rather than a full scheduler with preemption, fairness, time slicing, or system-wide load balancing.
- It performs basic scheduling, usually with **simple strategies** like first-come, first-served (FCFS), rather than replicating the complex logic of lower-level schedulers.

### What Is It Good For?

Worker pools are great for handling lots of **short-lived, independent tasks**. Use a worker pool when:

- You have a large number of **short-lived, independent tasks** to process.
- You want to **control the level of concurrency** to prevent overloading the CPU, database, or other resources.
- Your tasks **do not require persistence, retries, or orchestration** across systems.

Real-world examples:

- **Network services**: Manage thousands of concurrent network connections or API calls without spawning unbounded threads.
- **Batch processing**: Perform concurrent transformations or computations on a dataset (e.g., image resizing, file encoding).
- **Database connection pools**: Limit the number of concurrent DB connections by wrapping connection acquisition in a worker-like model.

### What It Isn't Meant For

Worker pools are useful, but they **aren't designed for**:

- **Persistent or long-running jobs** (e.g. ones that should survive restarts)
- **Distributed task coordination** across machines or services
- **Retries, persistence, or complex scheduling**

If you need those things, look into:

- Message queues (e.g., RabbitMQ, Kafka)
- Workflow engines (e.g., Temporal, Celery)
- Distributed systems or job runners

## Design and Strategies

### Task Dispatching Models

- **Centralized Queue (Pull Model)**:

  Workers pull tasks from a shared queue.

  - Pros: Simple, fair, easy to scale.
  - Cons: Queue contention under heavy load.

- **Dedicated Queues (Push Model)**:

  Dispatcher pushes tasks to each worker's own queue; can support work-stealing.

  - Pros: Reduces contention, improves throughput and cache locality.
  - Cons: More complex implementation.

### Queue Strategies

- **Unbounded Queue**: Simple, but risks unbounded memory growth under heavy load.
- **Bounded Queue**: Controls memory use and backpressure but may block or reject tasks if full.
- **Priority Queue**: Supports task prioritization at some overhead cost.

### Worker Management Models

- **Fixed-Size Pool**:
  - Pros: Predictable resource usage, simple implementation.
  - Cons: Inflexible under bursty or variable workloads.

- **Dynamic-Size Pool**:
  - Pros: Scales with demand, improves resource efficiency.
  - Cons: Requires careful synchronization and lifecycle management.

### Worker Scale-up Strategies

- **On-Demand (Reactive)**:

  Spawn a new worker when a task arrives and all current workers are busy.

  - Common in dynamic pools where quick responsiveness is prioritized.
  - Pros: Simple, responsive to load spikes.
  - Cons: Risk of overprovisioning during temporary spikes.

- **Preemptive (Proactive)**:

  Periodically estimate future workload and increase workers in advance.

  - Suitable for systems with predictable load patterns or periodic bursts.
  - Pros: Minimizes task queueing latency.
  - Cons: Requires load forecasting; risk of under/overestimating.

- **Warm-Up Pool**:

  Maintain a few pre-spawned idle workers ("warm pool") to reduce spin-up latency.

  - Pros: Improves responsiveness without full eager scaling.
  - Cons: Slightly higher idle resource usage.

### Worker Scale-down Strategies

- **Idle Timeout**:

  A worker terminates itself after being idle for a configured duration.

  - Most common method in dynamic pools for reclaiming unused resources.
  - Pros: Simple, adaptive to demand drops.
  - Cons: May cause cold-start delays if tasks return shortly after scale-down.

- **Periodic Reaper**:

  A background goroutine periodically checks for excess or idle workers and stops them.

  - Offers centralized control and can enforce stricter resource limits.
  - Pros: More deterministic control over worker count.
  - Cons: Higher implementation complexity.

### Worker Resize Policies

- **Eager**: Maximize responsiveness by aggressively scaling up.
- **Balanced**: Trade-off between responsiveness and throughput.
- **Lazy:** Maximize throughput with minimal active workers (like Python's ThreadPoolExecutor).

### Rejection Strategies

When the queue is full:

- Block until space is available.
- Drop the task (with or without error).
- Caller-runs: caller executes task synchronously (common in Java).

## References

### Repositories

Thanks to these repositories for inspiration and guidance:

- [alitto/pond](https://github.com/alitto/pond)
- [panjf2000/ants](https://github.com/panjf2000/ants)
- [gammazero/workerpool](https://github.com/gammazero/workerpool)

### Further Reading

- [Thread pool][1]
- [Scheduling (computing)][2]

[1]: https://en.wikipedia.org/wiki/Thread_pool
[2]: https://en.wikipedia.org/wiki/Scheduling_(computing)
