// Package winjob wraps a Windows Job Object so that every model process dies
// with the supervisor that created it. Without it, killing the supervisor from
// Task Manager leaves llama-server.exe running, holding GPU memory that no
// longer has an owner.
package winjob

import "errors"

// ErrUnsupported reports that job objects are unavailable on this platform.
// The supervisor must refuse to start rather than fall back to unsupervised
// process handling: a Linux PID means nothing to a Windows taskkill.
var ErrUnsupported = errors.New("job objects require Windows")
