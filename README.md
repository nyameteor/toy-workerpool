# toy-workerpool

A toy project to learn and build worker pools in Go from scratch.

## Progress

- [x] v1: Basic fixed-size pool, task submission, and graceful shutdown.
- [x] v2: Adds context support and panic recovery inside workers.
- [x] v3: Support dynamic worker scaling with on-demand start and idle timeout.
- [ ] v4: Switch to dedicated queues (push model) for better scalability.
- [ ] v5: Support task results and errors (Future-like interface).

## Design: Key Concepts & Strategies

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

- **Idle Timeout (TTL - Time to Live)**:

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

## Tests

Run all unit tests with race detection:

```sh
go test -race ./...
```

## References

Thanks to these repositories for inspiration and guidance:

- [alitto/pond](https://github.com/alitto/pond)
- [panjf2000/ants](https://github.com/panjf2000/ants)
- [gammazero/workerpool](https://github.com/gammazero/workerpool)
