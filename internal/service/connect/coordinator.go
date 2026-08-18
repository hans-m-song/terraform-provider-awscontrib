package connect

import "sync"

type queueKey struct {
	instanceID string
	queueID    string
}

type queueLock struct {
	mutex      sync.Mutex
	references int
}

type queueCoordinator struct {
	mutex sync.Mutex
	locks map[queueKey]*queueLock
}

func newQueueCoordinator() *queueCoordinator {
	return &queueCoordinator{locks: make(map[queueKey]*queueLock)}
}

func (c *queueCoordinator) withLock(key queueKey, operation func() error) error {
	c.mutex.Lock()
	lock := c.locks[key]
	if lock == nil {
		lock = &queueLock{}
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
