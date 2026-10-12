package server

import (
	"context"
	"sync"
)

type registry[V any] struct {
	mu      sync.Mutex
	entries map[string]V
}

func newRegistry[V any]() *registry[V] {
	return &registry[V]{entries: map[string]V{}}
}

func (r *registry[V]) store(key string, value V) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries[key] = value
}

func (r *registry[V]) add(key string, value V) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, present := r.entries[key]; present {
		return false
	}
	r.entries[key] = value
	return true
}

func (r *registry[V]) take(key string) (V, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	value, ok := r.entries[key]
	delete(r.entries, key)
	return value, ok
}

func (r *registry[V]) remove(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, key)
}

func (r *registry[V]) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.entries)
}

type cancelRegistry struct {
	*registry[context.CancelFunc]
}

func newCancelRegistry() cancelRegistry {
	return cancelRegistry{newRegistry[context.CancelFunc]()}
}

func (r cancelRegistry) cancel(key string) {
	if cancel, ok := r.take(key); ok {
		cancel()
	}
}
