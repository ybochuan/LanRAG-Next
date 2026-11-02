# M1 artifact publication recovery implementation plan

> Execute inline with superpowers:executing-plans and test-driven-development; one fresh whole-slice review after implementation. Preserve the existing dirty worktree; no commits/pushes or native core changes.

**Goal:** Strengthen M1-05P's filesystem publication barriers and prove recovery across the previously untested archive/SQL/ACK crash windows without pretending to test physical power loss.

**Architecture:** Keep the existing one-parse journal and filesystem-before-SQL protocol. Sync both files, then the staging directory, rename the whole directory and sync the artifact root before permitting SQL registration/ZIP success. An existing identical archive must repeat directory barriers on retry. Filesystem errors remain fail-closed.

**Tech stack:** Existing Go1.26 os.Root, modernc SQLite and native test fixtures. No dependencies, native algorithms or migrations added.

**Spec:** Frozen Architecture7.4 and TASK_BACKLOG M1-05/M1-05P; user-approved independent extensions and original-file preservation.

## Constraints and limits

- Only parser artifact persistence/helpers, targeted tests and documentation change.
- Linux/POSIX attempts directory File.Sync and propagates errors; platform/filesystem support must be observed. Windows retains existing file Sync plus atomic rename and explicitly lacks the new directory-flush guarantee; do not break current Windows behavior or claim equivalent durability.
- No automatic adoption/deletion of stale .pending directories or existing archives without job history.
- Successful process-crash recovery is not physical device power-loss proof. Do not shut down WSL/Windows, unmount volumes, inject real disk-full conditions or destroy user data.

## Steps

- [x] Add failing behavioral tests for barrier order, staging/root barrier failures, and retry barriers on an already-published identical archive.
- [x] Implement parser/artifact_sync.go and artifact_sync_windows.go plus minimal calls in artifacts.go; gofmt and targeted tests.
- [x] Add subprocess crashes after staging sync, after rename/before root sync, after root sync/before SQL, and after SQL commit/before HTTP ACK. Reuse original production handlers, never a duplicate parser implementation. Assert no premature SQL row, original files preserved, pending remnants retained, same job reused, final FK/byte consistency and cross-document denial.
- [x] Execute full extensions build/test/vet/module/Python checks, Linux race, and actual native upload+M0 combined E2E on the prior unchanged CGO core. All exit0 except full race exit1 for the preexisting Outbox calls=3/reports=2 assertion; parser passes. Additional target-storage TMPDIR crash run exits0; default WSL /tmp is tmpfs, /mnt/d is v9fs.
- [x] One independent review: Critical0/Important0/Minor0, no fix pass or second review. Update backlog/context/README and evidence, retain physical power loss and missing corpus/OCR gates as pending.

## Review focus

Existing archive retries must not skip a previously failed parent sync; no success ACK before SQL; sync failures must not poison known job ownership; orphan staging is not an ownership assertion; directory fsync and device durability claims must remain platform-specific.
