package keymutex

import (
	"sync"
)

// KeyMutex a hash key mutex
type KeyMutex struct {
	locks  []sync.Mutex
	count  uint
	handle HashHandle
}

// New return a keymutex
// It require the number of mutexs(prime number is better)
func New(count uint) *KeyMutex {
	var this KeyMutex
	this.count = count
	this.handle = ELFHash
	this.locks = make([]sync.Mutex, count, count)
	return &this
}

// NewByHash new a keymutex with a hashhandle
func NewByHash(count uint, handle HashHandle) *KeyMutex {
	var this KeyMutex
	this.count = count
	this.handle = handle
	this.locks = make([]sync.Mutex, count, count)
	return &this
}

// Count the number of mutexs
func (km *KeyMutex) Count() uint {
	return km.count
}

// LockID lock by idx
func (km *KeyMutex) LockID(idx uint) {
	km.locks[idx%km.count].Lock()
}

// UnlockID unlock by idx
func (km *KeyMutex) UnlockID(idx uint) {
	km.locks[idx%km.count].Unlock()
}

// Lock the key
func (km *KeyMutex) Lock(key string) {
	km.LockID(km.handle(key))
}

// Unlock the key
func (km *KeyMutex) Unlock(key string) {
	km.UnlockID(km.handle(key))
}
