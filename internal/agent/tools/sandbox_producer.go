package tools

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/Tencent/WeKnora/internal/extensionbridge"
	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
)

// Private seams let existing algorithm unit tests supply explicit policy
// fixtures. Production always uses the fixed loopback HTTP client.
var reserveSandboxProducer = extensionbridge.ReserveSourceProducer
var commitSandboxProducer = extensionbridge.CommitSourceProducer
var beginSandboxProducerStep = extensionbridge.BeginSourceStep

// sandboxProducerBinding describes the whole-byte identity of one sandbox file
// write. The SHA256 is the hash of the bytes that were actually stored, not
// of the bytes the caller intended: a producer only ever acknowledges bytes
// read back from the file itself.
type sandboxProducerBinding struct {
	producerID string
	binding    extensionbridge.SourceCopyBinding
}

// errSandboxProducerUnavailable reports that no producer could be reserved
// for this write. It is not a failure of the write: a run without provenance
// tracking still writes files, it just cannot claim the result as an
// attributed copy.
var errSandboxProducerUnavailable = errors.New("sandbox producer unavailable")

// reserveSandboxWrite reserves a producer for an intended sandbox write.
//
// It returns errSandboxProducerUnavailable when the run carries no provenance
// handle, which is the normal case for a session without an authorized
// source scope. The caller keeps writing in that case: refusing the write
// would break ordinary sandbox use, and pretending the file is attributed
// would invent a source that was never proven.
func reserveSandboxWrite(ctx context.Context, path string, content []byte, userID, sessionID string) (*sandboxProducerBinding, error) {
	if _, ok := extensionbridge.SourceRunFromContext(ctx); !ok {
		return nil, errSandboxProducerUnavailable
	}
	stepCtx, err := beginSandboxProducerStep(ctx, "write")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errSandboxProducerUnavailable, err)
	}
	binding := extensionbridge.SourceCopyBinding{
		UserID:         userID,
		SessionID:      sessionID,
		ResourceID:     path,
		ContentVersion: sandboxProducerDigest(content),
		SHA256:         sandboxProducerDigest(content),
	}
	producerID, err := reserveSandboxProducer(stepCtx, extensionbridge.SourceProducer{
		OperationID: "sandbox-write:" + sandboxProducerPathKey(path),
		CopyID:      sandboxProducerCopyID(path),
		Path:        sandboxProducerPath(path),
		Binding:     binding,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", errSandboxProducerUnavailable, err)
	}
	return &sandboxProducerBinding{producerID: producerID, binding: binding}, nil
}

// commitSandboxWrite acknowledges a reserved producer with the bytes that were
// actually read back after the write.
//
// The binding must match the reservation exactly: a read-back whose digest
// differs from the reserved one is refused rather than committed, because a
// producer that attests different bytes than it reserved would let an
// unrelated file pass as this run output.
func commitSandboxWrite(ctx context.Context, reserved *sandboxProducerBinding, readBack []byte, userID, sessionID string) error {
	if reserved == nil || reserved.producerID == "" {
		return errSandboxProducerUnavailable
	}
	actual := extensionbridge.SourceCopyBinding{
		UserID:         userID,
		SessionID:      sessionID,
		ResourceID:     reserved.binding.ResourceID,
		ContentVersion: reserved.binding.ContentVersion,
		SHA256:         sandboxProducerDigest(readBack),
	}
	if actual != reserved.binding {
		return fmt.Errorf("sandbox producer read-back digest does not match the reserved write")
	}
	return commitSandboxProducer(ctx, reserved.producerID, actual, "sandbox-write-commit:"+sandboxProducerPathKey(reserved.binding.ResourceID))
}

// sandboxProducerPath is the canonical, relative form the Host accepts. The
// Host refuses absolute paths and parent traversal, so the sandbox path is
// reduced to its layout-relative tail before it is sent.
func sandboxProducerPath(path string) string {
	trimmed := path
	for _, prefix := range []string{"/workspace/output/", "/workspace/input/", "/workspace/"} {
		if len(trimmed) > len(prefix) && trimmed[:len(prefix)] == prefix {
			trimmed = trimmed[len(prefix):]
			break
		}
	}
	return trimmed
}

// sandboxProducerCopyID names the copy this write produces. It is derived from
// the path so that a retry of the same write targets the same copy.
func sandboxProducerCopyID(path string) string {
	sum := sha256.Sum256([]byte("sandbox-copy:" + sandboxProducerPath(path)))
	return "copy-" + hex.EncodeToString(sum[:])
}

// sandboxProducerPathKey is the idempotency key for the reservation.
func sandboxProducerPathKey(path string) string {
	sum := sha256.Sum256([]byte(sandboxProducerPath(path)))
	return hex.EncodeToString(sum[:])
}

// sandboxProducerDigest is the whole-byte digest of the stored content.
func sandboxProducerDigest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// producerOwnerID is the user the sandbox file belongs to. It comes from the
// execution context, never from tool arguments, so a model cannot claim a
// producer for another user's write.
func producerOwnerID(ctx context.Context) string {
	userID, _ := types.UserIDFromContext(ctx)
	return userID
}

// noteSandboxProducerOutcome records a producer failure without failing the
// write. A lost producer ACK must never replay the underlying write, so the
// only honest outcome is a log line.
func noteSandboxProducerOutcome(ctx context.Context, path string, err error) {
	if err == nil {
		return
	}
	logger.Warnf(ctx, "[Tool][SandboxProducer] producer not committed: path=%s err=%v", path, err)
}
