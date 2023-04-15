package installercache

import "container/heap"

type Comparable[E any] interface {
	Compare(E) bool
}

// heapitems implements heap.Interface
type heapItems[E Comparable[E]] []E

func (hi heapItems[E]) Len() int { return len(hi) }
func (hi heapItems[E]) Less(i, j int) bool {
	return hi[i].Compare(hi[j])
}

func (hi heapItems[E]) Swap(i, j int) {
	hi[i], hi[j] = hi[j], hi[i]
}

func (hi *heapItems[E]) Push(item any) {
	*hi = append(*hi, item.(E))
}

func (hi *heapItems[E]) Pop() any {
	item := (*hi)[hi.Len()-1]
	*hi = (*hi)[0 : hi.Len()-1]
	return item
}

// PriorityQueue implements a non-thread safe priority queue
// Each item implmenets the Comparable interface. The priority
// is determined by the Compare function of that interface
type PriorityQueue[T Comparable[T]] struct {
	items heapItems[T]
	empty T
}

func NewPriorityQueue[T Comparable[T]](empty T) *PriorityQueue[T] {
	pq := &PriorityQueue[T]{
		items: make([]T, 0),
		empty: empty,
	}
	heap.Init(&pq.items)
	return pq
}

// Returns the size of the queue
func (pq *PriorityQueue[T]) Len() int {
	return pq.items.Len()
}

// Add the specified item into this priority queue.
func (pq *PriorityQueue[T]) Add(item T) {
	heap.Push(&pq.items, item)
}

// Retrieves and removes the head of this queue,
// If the queue is empty, it returns an empty value
// ok indicates whether value was found in the queue.
func (pq *PriorityQueue[T]) Pop() (value T, ok bool) {
	if pq.items.Len() > 0 {
		return heap.Pop(&pq.items).(T), true
	}
	return pq.empty, false
}
