package solution

import (
	"context"
	"sync/atomic"

	internal "github.com/piotrmiskiewicz/custom-api-server/pkg/apis/solution"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/watch"
)

const watchChanSize = 100

// watcher implements watch.Interface for Solution objects.
// It filters events by namespace and field selector before forwarding them.
type watcher struct {
	ch       chan watch.Event
	ns       string
	fieldSel fields.Selector
	cancel   context.CancelFunc
	stopped  uint32 // accessed atomically
}

// newWatcher creates a watcher for the given namespace and field selector.
// ns == "" means all namespaces. fieldSel == nil means no field filtering.
func newWatcher(ns string, fieldSel fields.Selector, cancel context.CancelFunc) *watcher {
	return &watcher{
		ch:       make(chan watch.Event, watchChanSize),
		ns:       ns,
		fieldSel: fieldSel,
		cancel:   cancel,
	}
}

// Stop implements watch.Interface. Safe to call multiple times.
func (w *watcher) Stop() {
	if atomic.CompareAndSwapUint32(&w.stopped, 0, 1) {
		w.cancel()
		close(w.ch)
	}
}

// ResultChan implements watch.Interface.
func (w *watcher) ResultChan() <-chan watch.Event {
	return w.ch
}

// isStopped reports whether Stop has been called.
func (w *watcher) isStopped() bool {
	return atomic.LoadUint32(&w.stopped) != 0
}

// send evaluates namespace and field selector filters and, if the event matches,
// sends it non-blocking to the watcher's channel (drops silently if full).
// Returns false if the watcher has been stopped.
func (w *watcher) send(eventType watch.EventType, obj *internal.Solution) bool {
	if w.isStopped() {
		return false
	}
	// Namespace filter.
	if w.ns != "" && obj.Namespace != w.ns {
		return true // filtered, but watcher is still alive
	}
	// Field selector filter.
	if w.fieldSel != nil && !w.fieldSel.Empty() {
		_, fieldSet, err := GetAttrs(obj)
		if err != nil || !w.fieldSel.Matches(fieldSet) {
			return true // filtered, but watcher is still alive
		}
	}
	ev := watch.Event{Type: eventType, Object: obj.DeepCopyObject()}
	select {
	case w.ch <- ev:
	default:
		// Buffer full — drop silently.
	}
	return true
}
