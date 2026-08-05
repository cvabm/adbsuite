package tasks

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusSuccess   Status = "success"
	StatusFailed    Status = "failed"
	StatusCancelled Status = "cancelled"
)

type Item struct {
	ID        string `json:"id"`
	Kind      string `json:"kind"`
	Serial    string `json:"serial"`
	Label     string `json:"label"`
	Status    Status `json:"status"`
	Message   string `json:"message"`
	CreatedAt int64  `json:"createdAt"`
	UpdatedAt int64  `json:"updatedAt"`
}

type JobFunc func(serial string) (string, error)

type Queue struct {
	mu       sync.Mutex
	items    []*Item
	max      int32
	cancel   map[string]bool
	onChange func()
}

func New(max int) *Queue {
	if max <= 0 {
		max = 4
	}
	return &Queue{
		items:  []*Item{},
		max:    int32(max),
		cancel: map[string]bool{},
	}
}

func (q *Queue) SetMax(n int) {
	if n <= 0 {
		n = 4
	}
	atomic.StoreInt32(&q.max, int32(n))
}

func (q *Queue) OnChange(fn func()) {
	q.onChange = fn
}

func (q *Queue) List() []Item {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]Item, len(q.items))
	for i, it := range q.items {
		out[i] = *it
	}
	return out
}

func (q *Queue) ClearFinished() {
	q.mu.Lock()
	defer q.mu.Unlock()
	kept := make([]*Item, 0, len(q.items))
	for _, it := range q.items {
		if it.Status == StatusQueued || it.Status == StatusRunning {
			kept = append(kept, it)
		}
	}
	q.items = kept
	q.emit()
}

func (q *Queue) Cancel(id string) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.cancel[id] = true
	for _, it := range q.items {
		if it.ID == id && it.Status == StatusQueued {
			it.Status = StatusCancelled
			it.Message = "已取消"
			it.UpdatedAt = time.Now().UnixMilli()
		}
	}
	q.emit()
}

func (q *Queue) EnqueueMany(kind, label string, serials []string, fn JobFunc) []string {
	ids := make([]string, 0, len(serials))
	q.mu.Lock()
	now := time.Now().UnixMilli()
	for _, serial := range serials {
		id := uuid.NewString()
		it := &Item{
			ID:        id,
			Kind:      kind,
			Serial:    serial,
			Label:     label,
			Status:    StatusQueued,
			CreatedAt: now,
			UpdatedAt: now,
		}
		q.items = append(q.items, it)
		ids = append(ids, id)
		go q.runItem(it, fn)
	}
	q.emit()
	q.mu.Unlock()
	return ids
}

var semMu sync.Mutex
var active int32

func (q *Queue) runItem(it *Item, fn JobFunc) {
	// simple global concurrency gate
	for {
		q.mu.Lock()
		if q.cancel[it.ID] {
			it.Status = StatusCancelled
			it.Message = "已取消"
			it.UpdatedAt = time.Now().UnixMilli()
			q.emit()
			q.mu.Unlock()
			return
		}
		q.mu.Unlock()

		semMu.Lock()
		max := atomic.LoadInt32(&q.max)
		if active < max {
			active++
			semMu.Unlock()
			break
		}
		semMu.Unlock()
		time.Sleep(80 * time.Millisecond)
	}
	defer func() {
		semMu.Lock()
		active--
		semMu.Unlock()
	}()

	q.mu.Lock()
	if q.cancel[it.ID] {
		it.Status = StatusCancelled
		it.Message = "已取消"
		it.UpdatedAt = time.Now().UnixMilli()
		q.emit()
		q.mu.Unlock()
		return
	}
	it.Status = StatusRunning
	it.UpdatedAt = time.Now().UnixMilli()
	q.emit()
	q.mu.Unlock()

	msg, err := fn(it.Serial)

	q.mu.Lock()
	defer q.mu.Unlock()
	it.UpdatedAt = time.Now().UnixMilli()
	if err != nil {
		it.Status = StatusFailed
		it.Message = err.Error()
		if msg != "" {
			it.Message = fmt.Sprintf("%s | %s", err.Error(), msg)
		}
	} else {
		it.Status = StatusSuccess
		it.Message = msg
		if it.Message == "" {
			it.Message = "完成"
		}
	}
	q.emit()
}

func (q *Queue) emit() {
	if q.onChange != nil {
		// call outside lock ideally; callers already hold/not hold carefully
		go q.onChange()
	}
}
