//go:build !windows

package winjob

// Job exists so that packages depending on it still compile off Windows. Every
// operation fails with ErrUnsupported: the supervisor is Windows-only because a
// process started under WSL has a Linux PID that no Windows tool can act on.
type Job struct{}

// New always fails off Windows.
func New() (*Job, error) { return nil, ErrUnsupported }

// AssignPID always fails off Windows.
func (j *Job) AssignPID(int) error { return ErrUnsupported }

// Close is a no-op off Windows.
func (j *Job) Close() error { return nil }
