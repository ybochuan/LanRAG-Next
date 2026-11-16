package tools

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// producerFixture records what the wiring asked the Host to do.
type producerFixture struct {
	reserved   []extensionbridge.SourceProducer
	committed  []extensionbridge.SourceCopyBinding
	commitErr  error
	reserveErr error
	steps      []string
}

// installProducerFixture replaces the producer seams for one test.
func installProducerFixture(t *testing.T, f *producerFixture) {
	t.Helper()
	prevReserve := reserveSandboxProducer
	prevCommit := commitSandboxProducer
	prevBegin := beginSandboxProducerStep
	reserveSandboxProducer = func(_ context.Context, q extensionbridge.SourceProducer) (string, error) {
		f.reserved = append(f.reserved, q)
		if f.reserveErr != nil {
			return "", f.reserveErr
		}
		return "producer-1", nil
	}
	commitSandboxProducer = func(_ context.Context, _ string, b extensionbridge.SourceCopyBinding, _ string) error {
		f.committed = append(f.committed, b)
		return f.commitErr
	}
	beginSandboxProducerStep = func(ctx context.Context, kind string) (context.Context, error) {
		f.steps = append(f.steps, kind)
		return ctx, nil
	}
	t.Cleanup(func() {
		reserveSandboxProducer = prevReserve
		commitSandboxProducer = prevCommit
		beginSandboxProducerStep = prevBegin
	})
}

func TestReserveSandboxWriteWithoutProvenanceHandleIsUnavailable(t *testing.T) {
	// A context without a source run has no handle, so the reservation must
	// report the producer as unavailable rather than inventing one.
	_, err := reserveSandboxWrite(context.Background(), "/workspace/output/a.txt", []byte("x"), "user", "session")
	require.Error(t, err)
	assert.True(t, errors.Is(err, errSandboxProducerUnavailable))
}

func TestReserveSandboxWriteWithoutHandleOpensNoStep(t *testing.T) {
	// Without a provenance handle there is nothing to reserve against, so no
	// step is opened and no reservation is attempted.
	fixture := &producerFixture{}
	installProducerFixture(t, fixture)
	_, err := reserveSandboxWrite(context.Background(), "/workspace/output/a.txt", []byte("x"), "user", "session")
	require.Error(t, err)
	assert.Empty(t, fixture.steps)
	assert.Empty(t, fixture.reserved)
}

func TestSandboxProducerPathIsRelativeAndCanonical(t *testing.T) {
	// The Host refuses absolute paths and parent traversal, so the
	// layout-relative tail is what must be sent.
	assert.Equal(t, "report.md", sandboxProducerPath("/workspace/output/report.md"))
	assert.Equal(t, "notes.txt", sandboxProducerPath("/workspace/notes.txt"))
	assert.Equal(t, "nested/report.md", sandboxProducerPath("/workspace/output/nested/report.md"))
}

func TestSandboxProducerCopyIDIsStablePerPath(t *testing.T) {
	// A retry of the same write must target the same copy.
	first := sandboxProducerCopyID("/workspace/output/report.md")
	second := sandboxProducerCopyID("/workspace/output/report.md")
	other := sandboxProducerCopyID("/workspace/output/other.md")
	assert.Equal(t, first, second)
	assert.NotEqual(t, first, other)
}

func TestCommitSandboxWriteRefusesMismatchedReadBack(t *testing.T) {
	// A read-back whose bytes differ from the reservation must not be
	// committed: a producer that attested other bytes would let an
	// unrelated file pass as this run output.
	reserved := &sandboxProducerBinding{
		producerID: "producer-1",
		binding: extensionbridge.SourceCopyBinding{
			UserID:         "user",
			SessionID:      "session",
			ResourceID:     "/workspace/output/report.md",
			ContentVersion: sandboxProducerDigest([]byte("planned")),
			SHA256:         sandboxProducerDigest([]byte("planned")),
		},
	}
	err := commitSandboxWrite(context.Background(), reserved, []byte("different"), "user", "session")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "does not match")
}

func TestCommitSandboxWriteWithoutReservationIsUnavailable(t *testing.T) {
	err := commitSandboxWrite(context.Background(), nil, []byte("x"), "user", "session")
	require.Error(t, err)
	assert.True(t, errors.Is(err, errSandboxProducerUnavailable))
}

func TestCommitSandboxWriteAcceptsMatchingReadBack(t *testing.T) {
	// The bytes that were actually stored are what the producer acknowledges.
	fixture := &producerFixture{}
	installProducerFixture(t, fixture)
	stored := []byte("stored bytes")
	reserved := &sandboxProducerBinding{
		producerID: "producer-1",
		binding: extensionbridge.SourceCopyBinding{
			UserID:         "user",
			SessionID:      "session",
			ResourceID:     "/workspace/output/report.md",
			ContentVersion: sandboxProducerDigest(stored),
			SHA256:         sandboxProducerDigest(stored),
		},
	}
	require.NoError(t, commitSandboxWrite(context.Background(), reserved, stored, "user", "session"))
	require.Len(t, fixture.committed, 1)
	assert.Equal(t, sandboxProducerDigest(stored), fixture.committed[0].SHA256)
	assert.Equal(t, "/workspace/output/report.md", fixture.committed[0].ResourceID)
}

func TestNoteSandboxProducerOutcomeSwallowsCommitErrors(t *testing.T) {
	// A lost producer ACK must never fail the write that already
	// happened, and must never be retried as a write.
	assert.NotPanics(t, func() {
		noteSandboxProducerOutcome(context.Background(), "/workspace/output/report.md", errors.New("ack lost"))
		noteSandboxProducerOutcome(context.Background(), "/workspace/output/report.md", nil)
	})
}
