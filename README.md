# learn-go-concurrency

A Golang project to learn and practice concurrency.

## Worker Pool

### Progress

- [x] v1: Basic fixed-size pool, task submission, and graceful shutdown.
- [x] v2: Adds context support and panic recovery inside workers.
- [ ] v3: Support idle timeout to dynamically resize workers.
- [ ] v4: Switch to dedicated queues (push model) for better scalability.
- [ ] v5: Support task results and errors (Future-like interface).

### Design: Key Concepts & Strategies

#### Task Dispatching

- **Centralized Queue (Pull Model)**: Workers pull tasks from a shared queue.
  - Pros: Simple, fair, easy to scale.
  - Cons: Queue contention under heavy load.

- **Dedicated Queues (Push Model)**: Dispatcher pushes tasks to each worker’s own queue; can support work-stealing.
  - Pros: Reduces contention, improves throughput and cache locality.
  - Cons: More complex implementation.

#### Worker Management

- **Fixed-Size Pool**:
  - Pros: Predictable resource use.
  - Cons: Inefficient under bursty workloads.

- **Dynamic-Size Pool**:
  - Pros: Adapts to workload, better resource efficiency.
  - Cons: More complexity managing worker creation and shutdown.

- **Idle Timeout**: Removes workers after being idle for some duration to conserve resources.

#### Worker Resize Strategies

- **Eager:** Maximize responsiveness by starting many workers upfront.
- **Balanced:** Trade-off between responsiveness and throughput.
- **Lazy:** Maximize throughput with minimal active workers (like Python's ThreadPoolExecutor).

#### Queue Strategies

- **Unbounded Queue**: Simple, but risks unbounded memory growth under heavy load.
- **Bounded Queue**: Controls memory use and backpressure but may block or reject tasks if full.
- **Priority Queue**: Supports task prioritization at some overhead cost.

#### Rejection Strategies

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

- [alitto/pond](https://github.com/alitto/pond)
- [panjf2000/ants](https://github.com/panjf2000/ants)
- [gammazero/workerpool](https://github.com/gammazero/workerpool)
