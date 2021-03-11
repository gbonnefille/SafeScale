/*
 * Copyright 2018-2021, CS Systemes d'Information, http://csgroup.eu
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cache

//go:generate minimock -o ../mocks/mock_clonable.go -i github.com/CS-SI/SafeScale/lib/utils/data/cache.Cache

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/CS-SI/SafeScale/lib/utils/data"
	"github.com/CS-SI/SafeScale/lib/utils/data/observer"
	"github.com/CS-SI/SafeScale/lib/utils/debug/callstack"
	"github.com/CS-SI/SafeScale/lib/utils/fail"
)

// Cache interface describing what a struct must implement to be considered as a Cache
type Cache interface {
	// concurrency.TaskedLock
	observer.Observer

	GetEntry(key string) (*Entry, fail.Error)                       // returns a cache entry from its key
	ReserveEntry(key string) fail.Error                             // locks an entry identified by key for update
	CommitEntry(key string, content Cacheable) (*Entry, fail.Error) // fills a previously reserved entry with content
	FreeEntry(key string) fail.Error                                // frees a cache entry (removing the reservation from cache)
	AddEntry(content Cacheable) (*Entry, fail.Error)                // adds a content in cache (doing ReserverEntry+CommitEntry i a whole)
}

type cache struct {
	lock sync.RWMutex

	name  string
	cache map[string]*Entry
	reserved map[string]struct{}
}

// NewCache creates a new cache
func NewCache(name string) (Cache, fail.Error) {
	if name == "" {
		return &cache{}, fail.InvalidParameterCannotBeEmptyStringError("id")
	}

	c := cache{
		name:  name,
		cache: map[string]*Entry{},
		reserved: map[string]struct{}{},
	}
	return &c, nil
}

func (c *cache) isNull() bool {
	return c == nil || c.name == "" || c.cache == nil
}

// GetID satisfies interface data.Identifiable
func (c cache) GetID() string {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.name
}

// GetName satisfies interface data.Identifiable
func (c cache) GetName() string {
	c.lock.RLock()
	defer c.lock.RUnlock()

	return c.name
}

// GetEntry returns a cache entry from its key
func (c *cache) GetEntry(key string) (*Entry, fail.Error) {
	if c.isNull() {
		return nil, fail.InvalidInstanceError()
	}
	if key == "" {
		return nil, fail.InvalidParameterCannotBeEmptyStringError("id")
	}

	c.lock.RLock()
	defer c.lock.RUnlock()

	if _, ok := c.reserved[key]; ok {
		return nil, fail.NotAvailableError("cache entry '%s' is reserved and cannot be use until freed or committed")
	}
	if ce, ok := c.cache[key]; ok {
		return ce, nil
	}

	return nil, fail.NotFoundError("failed to find cache entry with key '%s'", key)
}

// ReserveEntry locks an entry identified by key for update
// if entry does not exist, create an empty one
func (c *cache) ReserveEntry(key string) (xerr fail.Error) {
	if c.isNull() {
		return fail.InvalidInstanceError()
	}
	if key = strings.TrimSpace(key); key == "" {
		return fail.InvalidParameterCannotBeEmptyStringError("key")
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	return c.unsafeReserveEntry(key)
}

// unsafeReserveEntry is the workforce of ReserveEntry, without locking
func (c *cache) unsafeReserveEntry(key string) (xerr fail.Error) {
	if _, ok := c.reserved[key]; ok {
		return fail.NotAvailableError("the cache entry '%s' is already reserved", key)
	}
	if _, ok := c.cache[key]; ok {
		return fail.DuplicateError(callstack.DecorateWith("", "", fmt.Sprintf("there is already an entry in the cache with key '%s'", key), 0))
	}

	ce := newEntry(&reservation{key: key})
	ce.lock()
	c.cache[key] = &ce
	c.reserved[key] = struct{}{}
	return nil
}

// CommitEntry fills a previously reserved entry with content
func (c *cache) CommitEntry(key string, content Cacheable) (ce *Entry, xerr fail.Error) {
	if c.isNull() {
		return nil, fail.InvalidInstanceError()
	}
	if key = strings.TrimSpace(key); key == "" {
		return nil, fail.InvalidParameterCannotBeEmptyStringError("key")
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	return c.unsafeCommitEntry(key, content)
}

// unsafeCommitEntry is the workforce of CommitEntry, without locking
func (c *cache) unsafeCommitEntry(key string, content Cacheable) (ce *Entry, xerr fail.Error) {
	if _, ok := c.reserved[key]; !ok {
		return nil, fail.NotAvailableError("the cache entry '%s' is not reserved", key)
	}

	// content may bring new key, based on content.GetID(), than the key reserved; we have to check if this new key has not been reserved by someone else...
	if content.GetID() != key {
		if _, ok := c.reserved[content.GetID()]; ok {
			return nil, fail.InconsistentError("the cache entry '%s' corresponding to the ID of the content is reserved; content cannot be committed", content.GetID())
		}
	}

	// Everything seems ok, we can update
	var ok bool
	if ce, ok = c.cache[key]; ok {
		ce.content = data.NewImmutableKeyValue(content.GetID(), content)
		// reserved key may have to change accordingly with the ID of content
		delete(c.cache, key)
		delete(c.reserved, key)
		c.cache[content.GetID()] = ce
		ce.unlock()
		return ce, fail.ConvertError(content.AddObserver(c))
	}

	return nil, fail.NotFoundError("failed to find cache entry identified by '%s'", key)
}

// FreeEntry unlocks the cache entry and removes the reservation
func (c *cache) FreeEntry(key string) (xerr fail.Error) {
	if c.isNull() {
		return fail.InvalidInstanceError()
	}
	if key = strings.TrimSpace(key); key == "" {
		return fail.InvalidParameterCannotBeEmptyStringError("key")
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	return c.unsafeFreeEntry(key)
}

// unsafeFreeEntry is the workforce of FreeEntry, without locking
func (c *cache) unsafeFreeEntry(key string) fail.Error {
	if _, ok := c.reserved[key]; !ok {
		return fail.NotAvailableError("the cache entry '%s' is not reserved", key)
	}

	var (
		ce *Entry
		ok bool
	)
	if ce, ok = c.cache[key]; ok {
		delete(c.cache, key)
		delete(c.reserved, key)
		ce.unlock()
	}

	return nil
}

// AddEntry adds a content in cache
func (c *cache) AddEntry(content Cacheable) (*Entry, fail.Error) {
	if c == nil {
		return nil, fail.InvalidInstanceError()
	}
	if content == nil {
		return nil, fail.InvalidParameterCannotBeNilError("content")
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	id := content.GetID()
	if xerr := c.unsafeReserveEntry(id); xerr != nil {
		return nil, xerr
	}
	cacheEntry, xerr := c.unsafeCommitEntry(id, content)
	if xerr != nil {
		return nil, xerr
	}
	return cacheEntry, nil
}

// SignalChange tells the cache entry something has been changed in the content
func (c cache) SignalChange(key string) {
	if key == "" {
		return
	}

	c.lock.RLock()
	defer c.lock.RUnlock()

	if ce, ok := c.cache[key]; ok {
		ce.lock()
		defer ce.unlock()

		ce.lastUpdated = time.Now()
	}
}

// MarkAsFreed tells the cache to unlock content (decrementing the counter of uses)
func (c cache) MarkAsFreed(id string) {
	if id == "" {
		return
	}

	c.lock.RLock()
	defer c.lock.RUnlock()

	if ce, ok := c.cache[id]; ok {
		ce.UnlockContent()
	}
}

// MarkAsDeleted tells the cache entry to be considered as deleted
func (c cache) MarkAsDeleted(key string) {
	if key == "" {
		return
	}

	c.lock.Lock()
	defer c.lock.Unlock()

	delete(c.cache, key)
}
