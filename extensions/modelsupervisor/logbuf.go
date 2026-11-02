package modelsupervisor

import (
	"strings"
	"sync"
	"time"
)

// DefaultLogLines bounds how much output one instance retains. A model that
// fails to load can emit thousands of lines; keeping them all would turn a
// long-running supervisor into a memory leak.
const DefaultLogLines = 500

// LogLine is one retained output line. Consecutive identical lines collapse into
// a single entry so that a repeating progress bar does not evict real messages,
// while Time and LastAt preserve the window in which it was actually repeated.
type LogLine struct {
	Seq     uint64    `json:"seq"`
	Content string    `json:"content"`
	Count   int       `json:"count"`
	Time    time.Time `json:"time"`
	LastAt  time.Time `json:"last_at"`
}

// LogBuffer is a bounded, concurrency-safe ring of collapsed log lines.
type LogBuffer struct {
	mu    sync.Mutex
	max   int
	seq   uint64
	lines []LogLine
	now   func() time.Time
}

// NewLogBuffer creates a buffer holding at most max entries. A non-positive max
// falls back to DefaultLogLines.
func NewLogBuffer(max int) *LogBuffer {
	if max <= 0 {
		max = DefaultLogLines
	}
	return &LogBuffer{max: max, now: time.Now}
}

// Append records one line, collapsing it into the previous entry when identical.
// Blank lines are ignored.
func (b *LogBuffer) Append(content string) {
	content = strings.TrimRight(content, "\r\n")
	if strings.TrimSpace(content) == "" {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	at := b.now().UTC()
	if n := len(b.lines); n > 0 && b.lines[n-1].Content == content {
		b.lines[n-1].Count++
		b.lines[n-1].LastAt = at
		return
	}
	b.seq++
	b.lines = append(b.lines, LogLine{Seq: b.seq, Content: content, Count: 1, Time: at, LastAt: at})
	if len(b.lines) > b.max {
		trimmed := make([]LogLine, b.max)
		copy(trimmed, b.lines[len(b.lines)-b.max:])
		b.lines = trimmed
	}
}

// Since returns every retained line newer than the given sequence together with
// the current head sequence. Passing the returned head on the next call yields
// an incremental tail without re-reading the whole buffer.
func (b *LogBuffer) Since(seq uint64) ([]LogLine, uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]LogLine, 0, len(b.lines))
	for _, line := range b.lines {
		if line.Seq > seq {
			out = append(out, line)
		}
	}
	return out, b.seq
}

// Len reports how many collapsed entries are currently retained.
func (b *LogBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.lines)
}

// Tail returns up to n of the newest lines as one single-line string. It exists
// so a failed start can say why it failed in the same response: llama-server
// reports a bad model or a missing DLL on stderr and then exits, leaving the
// operator nothing but an exit code unless the last words are carried along.
//
// Each line is trimmed and clamped, and the whole result is bounded, because it
// is embedded in a JSON error field.
func (b *LogBuffer) Tail(n int, maxRunes int) string {
	if n <= 0 {
		return ""
	}
	b.mu.Lock()
	lines := make([]string, 0, n)
	for i := len(b.lines) - 1; i >= 0 && len(lines) < n; i-- {
		content := strings.TrimSpace(b.lines[i].Content)
		if content == "" {
			continue
		}
		lines = append(lines, content)
	}
	b.mu.Unlock()
	// Collected newest-first; a reader wants chronological order.
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	out := strings.Join(lines, " | ")
	if maxRunes > 0 && len([]rune(out)) > maxRunes {
		runes := []rune(out)
		out = "…" + string(runes[len(runes)-maxRunes:])
	}
	return out
}
