package connect

import "sync"

type dataTableKey struct {
	instanceID  string
	dataTableID string
}

type dataTableLock struct {
	mutex      sync.Mutex
	references int
}

type dataTableCoordinator struct {
	mutex sync.Mutex
	locks map[dataTableKey]*dataTableLock
}

func newDataTableCoordinator() *dataTableCoordinator {
	return &dataTableCoordinator{locks: make(map[dataTableKey]*dataTableLock)}
}

func (c *dataTableCoordinator) withLock(key dataTableKey, operation func() error) error {
	c.mutex.Lock()
	lock := c.locks[key]
	if lock == nil {
		lock = &dataTableLock{}
		c.locks[key] = lock
	}
	lock.references++
	c.mutex.Unlock()

	lock.mutex.Lock()
	defer func() {
		lock.mutex.Unlock()
		c.mutex.Lock()
		lock.references--
		if lock.references == 0 {
			delete(c.locks, key)
		}
		c.mutex.Unlock()
	}()

	return operation()
}
