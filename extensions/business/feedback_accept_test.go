package business

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ybochuan/LanRAG-Next/extensions/provenance"
	"github.com/ybochuan/LanRAG-Next/extensions/store"
)

func testScope() []provenance.Scope {
	return []provenance.Scope{{TenantID: 7, KnowledgeBaseID: "kb"}}
}

func TestFeedbackAutoActivationSweepAndSampleReview(t *testing.T) {
	oldPercent := feedbackSamplePercent
	feedbackSamplePercent = 100
	defer func() { feedbackSamplePercent = oldPercent }()

	s, db, clock := fixture(t)
	ctx := context.Background()
	create := cmd("feedback_create", "auto-low")
	create.Payload = Payload{Text: "Use the current date", Topic: "auto-date", Risk: "low"}
	f, err := s.Execute(ctx, create)
	if err != nil || f.Status != "approved" {
		t.Fatalf("create low: %+v %v", f, err)
	}
	*clock = clock.Add(25 * time.Hour)
	if err = s.SweepFeedbackActivations(ctx, testScope()); err != nil {
		t.Fatal(err)
	}
	if err = db.View(ctx, func(tx *sql.Tx) error {
		var staging, guidance, decision string
		if err := tx.QueryRow(`SELECT status FROM ext_feedback_staging WHERE feedback_id=?`, f.ID).Scan(&staging); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT status FROM ext_feedback_guidances WHERE feedback_id=?`, f.ID).Scan(&guidance); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT decision FROM ext_feedback_sample_reviews WHERE feedback_id=?`, f.ID).Scan(&decision); err != nil {
			return err
		}
		if staging != "active" || guidance != "active" || decision != "pending" {
			t.Fatalf("after activation: staging=%s guidance=%s decision=%s", staging, guidance, decision)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := s.List(ctx, Command{Scope: cmd("", "").Scope, Action: "feedback_list", ListStatus: "active"})
	if err != nil || len(rows) != 1 || rows[0]["sample_decision"] != "pending" {
		t.Fatalf("sample is not visible in list: %#v %v", rows, err)
	}
	var revision int64
	if err = db.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT revision FROM ext_feedback_staging WHERE feedback_id=?`, f.ID).Scan(&revision)
	}); err != nil {
		t.Fatal(err)
	}
	review := cmd("feedback_sample_review", "sample-fail")
	review.ID = f.ID
	review.ExpectedRevision = revision
	review.Payload = Payload{SampleDecision: "failed", ReviewNote: "does not match the effective policy"}
	if _, err = s.Execute(ctx, review); err != nil {
		t.Fatal(err)
	}
	if err = db.View(ctx, func(tx *sql.Tx) error {
		var staging, guidance, decision string
		if err := tx.QueryRow(`SELECT status FROM ext_feedback_staging WHERE feedback_id=?`, f.ID).Scan(&staging); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT status FROM ext_feedback_guidances WHERE feedback_id=?`, f.ID).Scan(&guidance); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT decision FROM ext_feedback_sample_reviews WHERE feedback_id=?`, f.ID).Scan(&decision); err != nil {
			return err
		}
		if staging != "quarantined" || guidance != "quarantined" || decision != "failed" {
			t.Fatalf("after failed sample: staging=%s guidance=%s decision=%s", staging, guidance, decision)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	review.OperationID = "sample-fail-replay"
	if _, err = s.Execute(ctx, review); !errors.Is(err, ErrConflict) {
		t.Fatalf("sample review replay: %v", err)
	}
}

func TestFeedbackSampleReviewPassKeepsActive(t *testing.T) {
	oldPercent := feedbackSamplePercent
	feedbackSamplePercent = 100
	defer func() { feedbackSamplePercent = oldPercent }()

	s, db, clock := fixture(t)
	ctx := context.Background()
	create := cmd("feedback_create", "sample-pass")
	create.Payload = Payload{Text: "Reviewed guidance", Topic: "sample-pass-topic", Risk: "low"}
	f, err := s.Execute(ctx, create)
	if err != nil {
		t.Fatal(err)
	}
	*clock = clock.Add(25 * time.Hour)
	if err = s.SweepFeedbackActivations(ctx, testScope()); err != nil {
		t.Fatal(err)
	}
	var revision int64
	if err = db.View(ctx, func(tx *sql.Tx) error {
		return tx.QueryRow(`SELECT revision FROM ext_feedback_staging WHERE feedback_id=?`, f.ID).Scan(&revision)
	}); err != nil {
		t.Fatal(err)
	}
	review := cmd("feedback_sample_review", "sample-pass-review")
	review.ID = f.ID
	review.ExpectedRevision = revision
	review.Payload = Payload{SampleDecision: "passed", ReviewNote: "matches policy"}
	if _, err = s.Execute(ctx, review); err != nil {
		t.Fatal(err)
	}
	if err = db.View(ctx, func(tx *sql.Tx) error {
		var staging, guidance, decision string
		if err := tx.QueryRow(`SELECT status FROM ext_feedback_staging WHERE feedback_id=?`, f.ID).Scan(&staging); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT status FROM ext_feedback_guidances WHERE feedback_id=?`, f.ID).Scan(&guidance); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT decision FROM ext_feedback_sample_reviews WHERE feedback_id=?`, f.ID).Scan(&decision); err != nil {
			return err
		}
		if staging != "active" || guidance != "active" || decision != "passed" {
			t.Fatalf("after passed sample: staging=%s guidance=%s decision=%s", staging, guidance, decision)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFeedbackTopicRecoveryResetsCircuit(t *testing.T) {
	s, db, _ := fixture(t)
	ctx := context.Background()
	seed := cmd("guidance_seed", "topic-seed")
	seed.Payload = Payload{Text: "Use dated evidence", Topic: "recover-topic"}
	first, err := s.Execute(ctx, seed)
	if err != nil || first.Status != "active" {
		t.Fatalf("seed: %+v %v", first, err)
	}
	for i, op := range []string{"topic-negative-1", "topic-negative-2"} {
		n := cmd("feedback_create", op)
		n.Payload = Payload{Text: "wrong", Topic: "recover-topic", Risk: "medium", Negative: true}
		if _, err = s.Execute(ctx, n); err != nil {
			t.Fatalf("negative %d: %v", i, err)
		}
	}
	recover := cmd("feedback_topic_recover", "topic-recover-no-confirm")
	recover.Payload = Payload{Topic: "recover-topic"}
	if _, err = s.Execute(ctx, recover); !errors.Is(err, ErrConflict) {
		t.Fatalf("recovery without confirmation: %v", err)
	}
	recover.OperationID = "topic-recover"
	recover.Payload.SecondConfirmation = true
	recover.Payload.ReviewNote = "root cause corrected"
	if result, err := s.Execute(ctx, recover); err != nil || result.Status != "recovered" {
		t.Fatalf("recovery: %+v %v", result, err)
	}
	again := cmd("guidance_seed", "topic-seed-after-recovery")
	again.Payload = Payload{Text: "Use the corrected dated evidence", Topic: "recover-topic"}
	if result, err := s.Execute(ctx, again); err != nil || result.Status != "active" {
		t.Fatalf("seed after recovery: %+v %v", result, err)
	}
	if err = db.View(ctx, func(tx *sql.Tx) error {
		var oldStatus string
		if err := tx.QueryRow(`SELECT status FROM ext_feedback_guidances WHERE guidance_id=?`, first.ID).Scan(&oldStatus); err != nil {
			return err
		}
		if oldStatus != "quarantined" {
			t.Fatalf("old guidance was unexpectedly reactivated: %s", oldStatus)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFeedbackAutoActivationAuditFailureRollsBack(t *testing.T) {
	oldPercent := feedbackSamplePercent
	feedbackSamplePercent = 100
	defer func() { feedbackSamplePercent = oldPercent }()

	s, db, clock := fixture(t)
	ctx := context.Background()
	create := cmd("feedback_create", "rollback-low")
	create.Payload = Payload{Text: "Atomic activation", Topic: "rollback-topic", Risk: "low"}
	f, err := s.Execute(ctx, create)
	if err != nil || f.Status != "approved" {
		t.Fatalf("create: %+v %v", f, err)
	}
	*clock = clock.Add(25 * time.Hour)
	if err = db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`CREATE TRIGGER fail_feedback_guidance BEFORE INSERT ON ext_feedback_guidances
BEGIN SELECT RAISE(ABORT,'injected'); END`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err = s.SweepFeedbackActivations(ctx, testScope()); err == nil {
		t.Fatal("guidance insert failure was ignored")
	}
	if err = assertFeedbackActivationState(db, f.ID, "approved", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err = db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.Exec(`DROP TRIGGER fail_feedback_guidance`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err = s.SweepFeedbackActivations(ctx, testScope()); err != nil {
		t.Fatal(err)
	}
	if err = assertFeedbackActivationState(db, f.ID, "active", 1, 1); err != nil {
		t.Fatal(err)
	}
}

func TestFeedbackSweepLoadIsIdempotent(t *testing.T) {
	oldPercent := feedbackSamplePercent
	feedbackSamplePercent = 100
	defer func() { feedbackSamplePercent = oldPercent }()

	s, db, clock := fixture(t)
	ctx := context.Background()
	const total = 24
	for i := 0; i < total; i++ {
		c := cmd("feedback_create", "load-"+string(rune('a'+i)))
		c.Payload = Payload{Text: "load feedback", Topic: "load-" + string(rune('a'+i)), Risk: "low"}
		if _, err := s.Execute(ctx, c); err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
	}
	*clock = clock.Add(25 * time.Hour)

	var wg sync.WaitGroup
	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.SweepFeedbackActivations(ctx, testScope())
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil && !errors.Is(err, ErrConflict) {
			t.Fatalf("load sweep: %v", err)
		}
	}
	var guidances, samples int
	if err := db.View(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRow(`SELECT count(*) FROM ext_feedback_guidances`).Scan(&guidances); err != nil {
			return err
		}
		return tx.QueryRow(`SELECT count(*) FROM ext_feedback_sample_reviews`).Scan(&samples)
	}); err != nil {
		t.Fatal(err)
	}
	if guidances != total || samples != total {
		t.Fatalf("load rows: guidances=%d samples=%d", guidances, samples)
	}
	if err := s.SweepFeedbackActivations(ctx, testScope()); err != nil {
		t.Fatal(err)
	}
	if err := db.View(ctx, func(tx *sql.Tx) error {
		var againGuidances, againSamples int
		if err := tx.QueryRow(`SELECT count(*) FROM ext_feedback_guidances`).Scan(&againGuidances); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT count(*) FROM ext_feedback_sample_reviews`).Scan(&againSamples); err != nil {
			return err
		}
		if againGuidances != guidances || againSamples != samples {
			t.Fatalf("idempotency changed rows: %d/%d -> %d/%d", guidances, samples, againGuidances, againSamples)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestFeedbackSweepSurvivesReopen(t *testing.T) {
	oldPercent := feedbackSamplePercent
	feedbackSamplePercent = 100
	defer func() { feedbackSamplePercent = oldPercent }()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "business-reopen.db")
	db, err := store.OpenDurable(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 11, 2, 0, 0, 0, 0, time.UTC)
	s := New(db, nil, func() time.Time { return now })
	create := cmd("feedback_create", "reopen-low")
	create.Payload = Payload{Text: "Survive a process restart", Topic: "reopen-topic", Risk: "low"}
	f, err := s.Execute(ctx, create)
	if err != nil || f.Status != "approved" {
		t.Fatalf("create: %+v %v", f, err)
	}
	now = now.Add(25 * time.Hour)

	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	db, err = store.OpenDurable(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s = New(db, nil, func() time.Time { return now })
	if err = s.SweepFeedbackActivations(ctx, testScope()); err != nil {
		t.Fatal(err)
	}
	if err = assertFeedbackActivationState(db, f.ID, "active", 1, 1); err != nil {
		t.Fatal(err)
	}
	if err = s.SweepFeedbackActivations(ctx, testScope()); err != nil {
		t.Fatal(err)
	}
	if err = assertFeedbackActivationState(db, f.ID, "active", 1, 1); err != nil {
		t.Fatal(err)
	}
}

func assertFeedbackActivationState(db interface {
	View(context.Context, func(*sql.Tx) error) error
}, id, status string, guidance, sample int) error {
	return db.View(context.Background(), func(tx *sql.Tx) error {
		var gotStatus string
		if err := tx.QueryRow(`SELECT status FROM ext_feedback_staging WHERE feedback_id=?`, id).Scan(&gotStatus); err != nil {
			return err
		}
		var gotGuidance, gotSample int
		if err := tx.QueryRow(`SELECT count(*) FROM ext_feedback_guidances WHERE feedback_id=?`, id).Scan(&gotGuidance); err != nil {
			return err
		}
		if err := tx.QueryRow(`SELECT count(*) FROM ext_feedback_sample_reviews WHERE feedback_id=?`, id).Scan(&gotSample); err != nil {
			return err
		}
		if gotStatus != status || gotGuidance != guidance || gotSample != sample {
			return errors.New("feedback activation state mismatch")
		}
		return nil
	})
}

func TestFeedbackSweepSkipsHighRiskAndNegativeTopic(t *testing.T) {
	s, _, clock := fixture(t)
	ctx := context.Background()

	high := cmd("feedback_create", "auto-high")
	high.Payload = Payload{Text: "High risk needs review", Topic: "auto-high", Risk: "high"}
	h, err := s.Execute(ctx, high)
	if err != nil {
		t.Fatal(err)
	}
	approve := cmd("feedback_approve", "approve-high")
	approve.ID, approve.ExpectedRevision = h.ID, h.Revision
	if _, err = s.Execute(ctx, approve); err != nil {
		t.Fatal(err)
	}

	medium := cmd("feedback_create", "auto-medium")
	medium.Payload = Payload{Text: "Medium risk auto", Topic: "auto-medium", Risk: "medium"}
	m, err := s.Execute(ctx, medium)
	if err != nil {
		t.Fatal(err)
	}
	approve = cmd("feedback_approve", "approve-medium")
	approve.ID, approve.ExpectedRevision = m.ID, m.Revision
	if _, err = s.Execute(ctx, approve); err != nil {
		t.Fatal(err)
	}

	blocked := cmd("feedback_create", "blocked-low")
	blocked.Payload = Payload{Text: "Blocked by topic", Topic: "blocked-topic", Risk: "low"}
	if _, err = s.Execute(ctx, blocked); err != nil {
		t.Fatal(err)
	}
	for i, op := range []string{"negative-a", "negative-b"} {
		n := cmd("feedback_create", op)
		n.Payload = Payload{Text: "negative", Topic: "blocked-topic", Risk: "medium", Negative: true}
		if _, err = s.Execute(ctx, n); err != nil {
			t.Fatalf("negative %d: %v", i, err)
		}
	}
	*clock = clock.Add(73 * time.Hour)
	if err = s.SweepFeedbackActivations(ctx, testScope()); err != nil {
		t.Fatal(err)
	}
	rows, err := s.List(ctx, Command{Scope: cmd("", "").Scope, Action: "feedback_list", ListStatus: "approved"})
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, row := range rows {
		found[row["topic"].(string)] = true
	}
	if !found["auto-high"] || found["auto-medium"] || !found["blocked-topic"] {
		t.Fatalf("unexpected sweep status: %#v", rows)
	}
}
