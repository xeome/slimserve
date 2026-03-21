package storage

import (
	"sync/atomic"

	"github.com/hashicorp/golang-lru/v2"
)

type ByteCache struct {
	lru       *lru.Cache[string, []byte]
	maxBytes  int64
	currBytes int64
}

func NewByteCache(maxBytes int64) *ByteCache {
	maxCount := maxBytes / (128 * 1024)
	if maxCount < 10 {
		maxCount = 10
	}
	c, _ := lru.New[string, []byte](int(maxCount))
	return &ByteCache{lru: c, maxBytes: maxBytes}
}

func (c *ByteCache) Get(key string) ([]byte, bool) {
	return c.lru.Get(key)
}

func (c *ByteCache) Set(key string, data []byte) {
	dataLen := int64(len(data))
	if dataLen > c.maxBytes/2 {
		return
	}

	for {
		currBytes := atomic.LoadInt64(&c.currBytes)
		available := c.maxBytes - currBytes

		if dataLen <= available {
			if atomic.CompareAndSwapInt64(&c.currBytes, currBytes, currBytes+dataLen) {
				c.lru.Add(key, data)
				return
			}
			continue
		}

		_, evicted, ok := c.lru.RemoveOldest()
		if !ok {
			atomic.StoreInt64(&c.currBytes, 0)
			continue
		}
		atomic.AddInt64(&c.currBytes, -int64(len(evicted)))
	}
}

func (c *ByteCache) Delete(key string) {
	val, ok := c.lru.Peek(key)
	if ok {
		atomic.AddInt64(&c.currBytes, -int64(len(val)))
		c.lru.Remove(key)
	}
}

func (c *ByteCache) Stats() (count int, usedBytes int64, maxBytes int64) {
	return c.lru.Len(), atomic.LoadInt64(&c.currBytes), c.maxBytes
}
