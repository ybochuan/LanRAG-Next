package modelsupervisor

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func fixedClock(buffer *LogBuffer) {
	var step int
	buffer.now = func() time.Time {
		step++
		return time.Unix(int64(step), 0).UTC()
	}
}

func TestLogBufferCollapsesConsecutiveDuplicates(t *testing.T) {
	buffer := NewLogBuffer(10)
	fixedClock(buffer)
	for i := 0; i < 3; i++ {
		buffer.Append("loaded 3/3 tensors\n")
	}
	buffer.Append("listening on http://127.0.0.1:8081")
	lines, head := buffer.Since(0)
	if len(lines) != 2 || head != 2 {
		t.Fatalf("lines=%+v head=%d", lines, head)
	}
	if lines[0].Content != "loaded 3/3 tensors" || lines[0].Count != 3 {
		t.Fatalf("collapse wrong: %+v", lines[0])
	}
	if !lines[0].Time.Before(lines[0].LastAt) {
		t.Fatalf("repeat window not preserved: %+v", lines[0])
	}
	if lines[1].Count != 1 {
		t.Fatalf("distinct line count=%d", lines[1].Count)
	}
}

func TestLogBufferIgnoresBlankLines(t *testing.T) {
	buffer := NewLogBuffer(10)
	buffer.Append("")
	buffer.Append("\r\n")
	buffer.Append("   ")
	if buffer.Len() != 0 {
		t.Fatalf("blank lines retained: %d", buffer.Len())
	}
}

func TestLogBufferBoundsRetentionToNewest(t *testing.T) {
	buffer := NewLogBuffer(3)
	for i := 0; i < 5; i++ {
		buffer.Append(fmt.Sprintf("line-%d", i))
	}
	if buffer.Len() != 3 {
		t.Fatalf("len=%d", buffer.Len())
	}
	lines, _ := buffer.Since(0)
	if len(lines) != 3 || lines[0].Content != "line-2" || lines[2].Content != "line-4" {
		t.Fatalf("retention window wrong: %+v", lines)
	}
	if lines[0].Seq != 3 || lines[2].Seq != 5 {
		t.Fatalf("sequence not monotonic across eviction: %+v", lines)
	}
}

func TestLogBufferSinceReturnsIncrementalTail(t *testing.T) {
	buffer := NewLogBuffer(10)
	buffer.Append("first")
	_, head := buffer.Since(0)
	buffer.Append("second")
	tail, next := buffer.Since(head)
	if len(tail) != 1 || tail[0].Content != "second" || next != head+1 {
		t.Fatalf("tail=%+v next=%d head=%d", tail, next, head)
	}
	empty, _ := buffer.Since(next)
	if len(empty) != 0 {
		t.Fatalf("expected empty tail, got %+v", empty)
	}
}

func TestLogBufferIsConcurrencySafe(t *testing.T) {
	buffer := NewLogBuffer(1000)
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				buffer.Append(fmt.Sprintf("worker-%d-%d", worker, i))
			}
		}(worker)
	}
	wg.Wait()
	if buffer.Len() != 400 {
		t.Fatalf("len=%d", buffer.Len())
	}
}

// Tail exists so a failed start can quote the reason. It must return the newest
// lines in chronological order, clamped, and survive an empty buffer.
func TestLogBufferTailKeepsNewestLinesInOrder(t *testing.T) {
	buffer := NewLogBuffer(10)
	if got := buffer.Tail(3, 0); got != "" {
		t.Fatalf("empty buffer tail = %q", got)
	}
	for _, line := range []string{"loading model", "error loading model", "failed to load model"} {
		buffer.Append(line)
	}
	buffer.Append("failed to load model") // collapses into the previous entry
	if got := buffer.Tail(2, 0); got != "error loading model | failed to load model" {
		t.Fatalf("tail = %q", got)
	}
	long := NewLogBuffer(4)
	long.Append(strings.Repeat("x", 50))
	if got := long.Tail(1, 10); !strings.HasPrefix(got, "…") || !strings.HasSuffix(got, strings.Repeat("x", 10)) {
		t.Fatalf("clamped tail = %q", got)
	}
}
