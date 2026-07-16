package data

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

const (
	// debounceFor coalesces fs-event storms (object-store writes are
	// rename-heavy) into one delta per topic.
	debounceFor = 100 * time.Millisecond
)

// fallbackEvery is the degraded cadence when the watcher cannot run — the
// v1 cockpit's 3s ticker, demoted from default to last resort. A var so
// tests can shorten it.
var fallbackEvery = 3 * time.Second

// watcher fans fs-change notifications out to subscribers, classified by
// topic and debounced. One watcher serves any number of Subscribe calls.
type watcher struct {
	orunRoot string

	mu   sync.Mutex
	seq  uint64
	subs map[int]*subscriber
	next int

	startOnce sync.Once
	stop      context.CancelFunc
}

type subscriber struct {
	topics map[Topic]bool
	ch     chan Delta
}

func newWatcher(orunRoot string) *watcher {
	return &watcher{orunRoot: orunRoot, subs: make(map[int]*subscriber)}
}

// topicDirs maps the watched directories to topics. Watching refs — not
// objects — keeps event volume proportional to logical changes: content
// writes land as loose objects, but every visible mutation moves a ref
// (memory: the object store is git-like; refs are the mutation points).
func (w *watcher) topicDirs() map[string]Topic {
	return map[string]Topic{
		filepath.Join(w.orunRoot, "refs"):                              TopicRuns,
		filepath.Join(w.orunRoot, "objectmodel", "refs"):               TopicCatalog,
		filepath.Join(w.orunRoot, "objectmodel", "refs", "catalogs"):   TopicCatalog,
		filepath.Join(w.orunRoot, "objectmodel", "refs", "executions"): TopicRuns,
		// The live run working tree: objrun projects step state here on
		// every runner tick, so external runs (a CLI `orun run` in another
		// terminal) stream step progress through the same deltas.
		filepath.Join(w.orunRoot, "objectmodel", "run"): TopicRuns,
		filepath.Join(w.orunRoot, "agents", "live"):     TopicSessions,
		filepath.Join(w.orunRoot, "plans"):              TopicRuns,
	}
}

func (w *watcher) subscribe(ctx context.Context, topics []Topic) <-chan Delta {
	if len(topics) == 0 {
		topics = AllTopics
	}
	sub := &subscriber{
		topics: make(map[Topic]bool, len(topics)),
		ch:     make(chan Delta, 2*len(AllTopics)),
	}
	for _, t := range topics {
		sub.topics[t] = true
	}

	w.mu.Lock()
	id := w.next
	w.next++
	w.subs[id] = sub
	w.mu.Unlock()

	w.startOnce.Do(w.start)

	go func() {
		<-ctx.Done()
		// Removal and close happen under the same lock emit sends under,
		// so a send on a closed channel is impossible.
		w.mu.Lock()
		delete(w.subs, id)
		close(sub.ch)
		w.mu.Unlock()
	}()
	return sub.ch
}

// emit fans a topic delta to interested subscribers. Delivery never
// blocks: a subscriber whose buffer is full is behind, and dropping is
// harmless — deltas carry no payload, and the re-snapshot it will do when
// it drains already reflects this change. That drop IS the coalescing.
func (w *watcher) emit(topic Topic, degraded bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.seq++
	d := Delta{Topic: topic, Seq: w.seq, Degraded: degraded}
	for _, s := range w.subs {
		if !s.topics[topic] {
			continue
		}
		select {
		case s.ch <- d:
		default:
		}
	}
}

// start runs the fs watcher, or the degraded ticker when watching fails.
// It never returns errors: the data plane must keep working on filesystems
// where fsnotify cannot (design risk R4).
func (w *watcher) start() {
	ctx, cancel := context.WithCancel(context.Background())
	w.stop = cancel

	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		go w.runFallback(ctx)
		return
	}

	dirs := w.topicDirs()
	watching := 0
	for dir := range dirs {
		// Ensure watchable dirs exist — an empty workspace grows them later;
		// watching the parent catches creation.
		if _, statErr := os.Stat(dir); statErr == nil {
			if fsw.Add(dir) == nil {
				watching++
			}
		} else if parent := filepath.Dir(dir); parent != "" {
			_ = fsw.Add(parent)
		}
	}
	if watching == 0 {
		// Nothing watchable yet (fresh workspace): watch the root so the
		// first write upgrades us, and tick degraded meanwhile.
		_ = fsw.Add(w.orunRoot)
	}

	go w.runWatcher(ctx, fsw, dirs)
}

func (w *watcher) runWatcher(ctx context.Context, fsw *fsnotify.Watcher, dirs map[string]Topic) {
	defer fsw.Close()
	pending := make(map[Topic]bool)
	var timer *time.Timer
	var timerC <-chan time.Time

	flush := func() {
		for t := range pending {
			w.emit(t, false)
			delete(pending, t)
		}
		timerC = nil
	}

	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-fsw.Events:
			if !ok {
				go w.runFallback(ctx)
				return
			}
			// New directories grow the watch: fsnotify is not recursive,
			// and both the run working tree and ref namespaces nest.
			if info, err := os.Stat(ev.Name); err == nil && info.IsDir() {
				clean := filepath.Clean(ev.Name)
				if _, isTarget := dirs[clean]; isTarget || classify(clean, dirs) != "" {
					_ = fsw.Add(clean)
				}
			}
			topic := classify(ev.Name, dirs)
			if topic == "" {
				continue
			}
			pending[topic] = true
			if timer == nil {
				timer = time.NewTimer(debounceFor)
			} else {
				timer.Reset(debounceFor)
			}
			timerC = timer.C
		case <-timerC:
			flush()
		case _, ok := <-fsw.Errors:
			if !ok {
				go w.runFallback(ctx)
				return
			}
			// Individual errors are survivable; the stream continues.
		}
	}
}

// runFallback is the degraded path: tick every topic on a slow cadence.
// Deltas are marked Degraded so the status line can say so.
func (w *watcher) runFallback(ctx context.Context) {
	t := time.NewTicker(fallbackEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, topic := range AllTopics {
				w.emit(topic, true)
			}
		}
	}
}

func (w *watcher) close() {
	if w.stop != nil {
		w.stop()
	}
}

// classify resolves an event path to its topic by longest matching dir.
func classify(path string, dirs map[string]Topic) Topic {
	path = filepath.Clean(path)
	best, bestLen := Topic(""), 0
	for dir, topic := range dirs {
		if (strings.HasPrefix(path, dir+string(filepath.Separator)) || path == dir) && len(dir) > bestLen {
			best, bestLen = topic, len(dir)
		}
	}
	return best
}
