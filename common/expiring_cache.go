package common

import (
	"sync"
	"time"

	"github.com/patrickmn/go-cache"
)

type ExpiringCache interface {
	GetOrInsert(key string, value interface{}) (interface{}, bool)
	Get(key string) (interface{}, bool)
	Set(key string, value interface{})
}

type expiringCache struct {
	cache *cache.Cache
	mutex sync.Mutex
}

func NewExpiringCache(defaultExpiration, cleanupInterval time.Duration) ExpiringCache {
	return &expiringCache{cache: cache.New(defaultExpiration, cleanupInterval)}
}

func (e *expiringCache) GetOrInsert(key string, value interface{}) (actualIntf interface{}, loaded bool) {
	actualIntf, loaded = e.Get(key)
	if loaded {
		return
	}
	e.mutex.Lock()
	defer e.mutex.Unlock()
	actualIntf, loaded = e.Get(key)
	if loaded {
		return
	}
	e.cache.SetDefault(key, value)
	return value, false
}

func (e *expiringCache) Get(key string) (interface{}, bool) {
	actualIntf, loaded := e.cache.Get(key)
	if loaded {
		e.cache.SetDefault(key, actualIntf)
	}
	return actualIntf, loaded
}

func (e *expiringCache) Set(key string, value interface{}) {
	e.cache.SetDefault(key, value)
}
