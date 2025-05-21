# learn-go-concurrency

A Golang project to learn and practice concurrency.

## Worker Pool

### v1: Basic

- [x] Fixed number of worker goroutines and a buffered task queue with fixed capacity.
- [x] `Submit` API for task submission.
- [x] `TaskGroup` for tracking a batch of related tasks.
- [x] Graceful shutdown with `Stop()` and `Wait()`.

### v2: Context-Aware Support

- [x] Context support at both the `Pool` and `TaskGroup` levels.
- [x] Task skipping when the context is already canceled.
- [x] Panic recovery and stopped pool check.

### v3: Future Support

- [ ] `Submit` API supports returning **futures**.
- [ ] `Future` can be resolved to a result and an error.

## Tests

Run all unit tests with race detection:

```sh
go test -race ./...
```

## References

- [alitto/pond](https://github.com/alitto/pond)
- [panjf2000/ants](https://github.com/panjf2000/ants)
