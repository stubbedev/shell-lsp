package server

import "sync"

type coalescer struct {
	mu      sync.Mutex
	running map[string]bool
	again   map[string]bool
}

func newCoalescer() *coalescer {
	return &coalescer{running: map[string]bool{}, again: map[string]bool{}}
}

func (c *coalescer) run(key string, fn func()) {
	c.mu.Lock()
	if c.running[key] {
		c.again[key] = true
		c.mu.Unlock()
		return
	}
	c.running[key] = true
	c.mu.Unlock()
	go func() {
		for {
			fn()
			c.mu.Lock()
			if !c.again[key] {
				delete(c.running, key)
				c.mu.Unlock()
				return
			}
			delete(c.again, key)
			c.mu.Unlock()
		}
	}()
}
