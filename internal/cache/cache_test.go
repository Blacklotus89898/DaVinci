package cache

import (
	"fmt"
	"sync"
	"testing"
)

func TestLRUBasic(t *testing.T) {
	c := New[string, int](3)

	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("c", 3)

	if v, ok := c.Get("a"); !ok || v != 1 {
		t.Errorf("Get(a) = %v,%v; want 1,true", v, ok)
	}

	// Overflow: "b" is now LRU (a was recently accessed); adding "d" evicts "b".
	c.Set("d", 4)
	if _, ok := c.Get("b"); ok {
		t.Error("b should have been evicted")
	}
	if v, ok := c.Get("d"); !ok || v != 4 {
		t.Errorf("Get(d) = %v,%v; want 4,true", v, ok)
	}
}

func TestLRUPurge(t *testing.T) {
	c := New[string, int](5)
	c.Set("x", 10)
	c.Set("y", 20)
	c.Purge()
	if _, ok := c.Get("x"); ok {
		t.Error("x should be gone after Purge")
	}
	// Can still set after purge.
	c.Set("z", 30)
	if v, ok := c.Get("z"); !ok || v != 30 {
		t.Errorf("Get after Purge+Set = %v,%v; want 30,true", v, ok)
	}
}

func TestLRUConcurrent(t *testing.T) {
	c := New[string, int](64)
	const goroutines = 8
	const ops = 200

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		g := g
		go func() {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				key := fmt.Sprintf("k%d-%d", g, i%16)
				c.Set(key, g*ops+i)
				c.Get(key)
				if i%50 == 0 {
					c.Purge()
				}
			}
		}()
	}
	wg.Wait()
	// If we reach here without panic the race detector is happy.
}
