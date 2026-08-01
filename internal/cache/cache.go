package cache

import "container/list"

// LRU is a generic least-recently-used cache.
type LRU[K comparable, V any] struct {
	cap   int
	items map[K]*list.Element
	ll    *list.List
}

type entry[K comparable, V any] struct {
	key K
	val V
}

func New[K comparable, V any](cap int) *LRU[K, V] {
	return &LRU[K, V]{
		cap:   cap,
		items: make(map[K]*list.Element, cap),
		ll:    list.New(),
	}
}

func (c *LRU[K, V]) Get(key K) (V, bool) {
	el, ok := c.items[key]
	if !ok {
		var zero V
		return zero, false
	}
	c.ll.MoveToFront(el)
	return el.Value.(*entry[K, V]).val, true
}

func (c *LRU[K, V]) Set(key K, val V) {
	if el, ok := c.items[key]; ok {
		c.ll.MoveToFront(el)
		el.Value.(*entry[K, V]).val = val
		return
	}
	if c.ll.Len() >= c.cap {
		back := c.ll.Back()
		if back != nil {
			c.ll.Remove(back)
			delete(c.items, back.Value.(*entry[K, V]).key)
		}
	}
	el := c.ll.PushFront(&entry[K, V]{key: key, val: val})
	c.items[key] = el
}

func (c *LRU[K, V]) Purge() {
	c.items = make(map[K]*list.Element, c.cap)
	c.ll.Init()
}
