package portable

import "sync"

// winOptQuota tracks the requested SO_RCVBUF sizes of active sockets. The
// figures are a safety budget, NOT an OS measurement of physical TCP memory.
type winOptQuota struct {
	sync.Mutex
	next  uint64
	used  uint64
	slots map[uint64]uint32
}

func (q *winOptQuota) reserve(id *uint64, target uint32, limit uint64) bool {
	q.Lock()
	defer q.Unlock()
	if q.slots == nil {
		q.slots = make(map[uint64]uint32)
	}
	previous := q.slots[*id]
	after := q.used - uint64(previous) + uint64(target)
	if after > limit {
		return false
	}
	if *id == 0 {
		q.next++
		*id = q.next
	}
	q.slots[*id] = target
	q.used = after
	return true
}
func (q *winOptQuota) release(id uint64) {
	if id == 0 {
		return
	}
	q.Lock()
	defer q.Unlock()
	if previous, ok := q.slots[id]; ok {
		q.used -= uint64(previous)
		delete(q.slots, id)
	}
}
func (q *winOptQuota) reserved(id uint64) uint32 {
	q.Lock()
	defer q.Unlock()
	return q.slots[id]
}
