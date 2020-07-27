# keymutex
A golang key set mutex. Supposing you have a set of keys describing resources that each need an independent mutex keymutex allows you to provide a pseudo-independent set of mutexes in constant size. Keymutex allocates a fixed sized array of mutexes to provide locking for an entire (potentially much larger) space of keys. This works by hashing the string key to provide an index into the backing array of mutexes. If keys collide in the hash function modulo the backing array size then they will share a mutex, meaning some indepedent keys will share a mutex but some will not. This allows you to trade space (size of mutex array) against mutex contention by chaning the size of the backing array.

## Example
```go
// Create a keymutex with backing array of 47 mutexes (prime number is good for hash coverage)
// Using a larger number here increases the memory usage by decreases the mutex contention.
var kmutex = keymutex.New(47)

// ...
go func(key string) {
    kmutex.Lock(key)
    defer kmutex.Unlock(key)
    // do something with the resource decribed by the key
}(akey)
```
