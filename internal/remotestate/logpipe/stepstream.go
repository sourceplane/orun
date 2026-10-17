package logpipe

import (
	"strings"
	"sync"
	"time"
)

// Step stream defaults (saas-live-runs LR8, LR-D9): a running step's output
// is sent every 2 s, or as soon as 64 KiB of it waits.
const (
	DefaultStreamInterval = 2 * time.Second
	DefaultStreamMaxBytes = 64 * 1024
)

// Redactor is what a step stream masks with: the run's secret redactor.
type Redactor interface {
	Filter(s string) string
	// SafeCut is the largest offset at or before at where s may be split
	// and each side filtered as s is filtered whole.
	SafeCut(s string, at int) int
	// MaxLen is the longest value the redactor masks.
	MaxLen() int
}

// StepStreamOptions tunes a StepStream; the zero value is the defaults.
type StepStreamOptions struct {
	Interval time.Duration
	MaxBytes int
}

// StepStream is one running step's output on its way to the remote log
// while the step runs (saas-live-runs LR8). The executor writes the raw
// output to it as it arrives; every Interval it sends the complete lines
// that wait, and once MaxBytes wait it sends what it safely can without
// waiting for a newline. Each piece is masked before it is sent, and cut
// only where no secret spans the cut, with the last MaxLen()-1 bytes always
// held back — so a secret still arriving is never sent in halves either.
//
// It never sends the end of the step: Finish stops it and says what was
// sent, and the caller sends the rest with the step's `end` event.
type StepStream struct {
	emit     func(text string)
	redact   Redactor
	maxBytes int

	mu       sync.Mutex
	raw      []byte // received and not yet sent
	sent     strings.Builder
	finished bool
	stop     chan struct{}
	done     chan struct{}
}

// NewStepStream starts a stream that hands each masked piece to emit (which
// must not block for long: the pipeline's Append). redact may be nil.
func NewStepStream(emit func(text string), redact Redactor, opts StepStreamOptions) *StepStream {
	interval := opts.Interval
	if interval <= 0 {
		interval = DefaultStreamInterval
	}
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultStreamMaxBytes
	}
	s := &StepStream{emit: emit, redact: redact, maxBytes: maxBytes, stop: make(chan struct{}), done: make(chan struct{})}
	go s.tick(interval)
	return s
}

func (s *StepStream) tick(interval time.Duration) {
	defer close(s.done)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-s.stop:
			return
		case <-t.C:
			s.mu.Lock()
			if !s.finished {
				s.flushLocked(true)
			}
			s.mu.Unlock()
		}
	}
}

// Write takes raw output. Safe for concurrent use; it never fails.
func (s *StepStream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.finished {
		return len(p), nil
	}
	s.raw = append(s.raw, p...)
	if len(s.raw) >= s.maxBytes {
		s.flushLocked(false)
	}
	return len(p), nil
}

// flushLocked sends what may go now: up to the last complete line when
// linesOnly, else up to the hold-back (preferring a line end). mu held.
func (s *StepStream) flushLocked(linesOnly bool) {
	if len(s.raw) == 0 {
		return
	}
	text := string(s.raw)
	hold := 0
	if s.redact != nil {
		if m := s.redact.MaxLen(); m > 1 {
			hold = m - 1
		}
	}
	limit := len(text) - hold
	if limit <= 0 {
		return
	}
	cut := limit
	if nl := strings.LastIndexByte(text[:limit], '\n'); nl >= 0 {
		cut = nl + 1
	} else if linesOnly {
		return
	}
	if s.redact != nil {
		cut = s.redact.SafeCut(text, cut)
	} else {
		cut = runeCut(text, cut)
	}
	if cut <= 0 {
		return
	}
	piece := text[:cut]
	if s.redact != nil {
		piece = s.redact.Filter(piece)
	}
	s.raw = append(s.raw[:0], s.raw[cut:]...)
	s.sent.WriteString(piece)
	s.emit(piece)
}

// Finish stops the stream and returns what it sent (masked). What it held
// is discarded: the step's recorded output carries it. Idempotent.
func (s *StepStream) Finish() string {
	s.mu.Lock()
	if !s.finished {
		s.finished = true
		close(s.stop)
	}
	s.mu.Unlock()
	<-s.done
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sent.String()
}

// FinishRest stops the stream and returns what it sent and, masked, what it
// held — for a step that ended with no recorded output to send instead.
func (s *StepStream) FinishRest() (sent, rest string) {
	sent = s.Finish()
	s.mu.Lock()
	defer s.mu.Unlock()
	rest = string(s.raw)
	s.raw = nil
	if s.redact != nil {
		rest = s.redact.Filter(rest)
	}
	return sent, rest
}

// runeCut backs at off to a UTF-8 rune start.
func runeCut(s string, at int) int {
	for at > 0 && at < len(s) && s[at]&0xC0 == 0x80 {
		at--
	}
	return at
}

// StepTail is what a step's `end` event carries once a stream sent `sent`
// of it and the step's recorded (masked) output is `final`: the part of
// final not yet sent. Executors record a step's output trimmed of trailing
// newlines, so a stream that sent them still lines up. When the stream and
// the record disagree — the step was retried, or the executor added text
// the process did not print — the whole record follows a marker line
// rather than a log that silently drops or repeats part of it.
func StepTail(sent, final string) string {
	if sent == "" {
		return final
	}
	if strings.HasPrefix(final, sent) {
		return final[len(sent):]
	}
	trimmed := strings.TrimRight(sent, "\n")
	if strings.HasPrefix(final, trimmed) {
		tail := final[len(trimmed):]
		// The newlines the stream already sent are not sent again.
		for n := len(sent) - len(trimmed); n > 0 && strings.HasPrefix(tail, "\n"); n-- {
			tail = tail[1:]
		}
		return tail
	}
	lead := ""
	if !strings.HasSuffix(sent, "\n") {
		lead = "\n"
	}
	return lead + "--- orun: the step's recorded output ---\n" + final
}
