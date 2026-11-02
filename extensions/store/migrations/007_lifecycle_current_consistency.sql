-- Frozen Architecture invariant A: ext_lifecycle_current is a projection of
-- ext_lifecycle_events and must be fully reconstructible from it.
--
-- The foreign keys on this table already guarantee that last_event_id names a
-- committed event in the same tenant / knowledge base / document scope. They
-- cannot relate the state column to that event, so a direct UPDATE could record
-- a state no event ever produced while every constraint still held. That is how
-- a document could read as stale with an event log that never left active.
--
-- These triggers close that gap for every writer, including a hand-written
-- UPDATE: the row may only carry the to_state of the event it points at. A
-- missing event is left to the foreign key so each constraint keeps its own
-- failure mode.
--
-- A matching state alone is still not enough. Pointing last_event_id at an older
-- event of the same document satisfies that check while the projection silently
-- lags the log, so a second pair of triggers requires the referenced event to be
-- the latest committed one. The two rules keep separate messages because they
-- describe different corruption, and a missing event still falls through to the
-- foreign key (the sequence subquery yields NULL, so no trigger fires).
--
-- This guard is code, not data: the statements below are replayed on every
-- writable open (see store.migrate) so a database already stamped schema 7 picks
-- up a revised body instead of silently keeping the older one. That is why every
-- CREATE is preceded by DROP IF EXISTS, and why this file stays revision 7
-- instead of taking a new version number.
--
-- The guard is not retroactive: rows that are already inconsistent stay as they
-- are (fail-closed, the document stays excluded) and must be rebuilt from the
-- event log with the documented repair procedure. A migration never rewrites
-- facts silently.

DROP TRIGGER IF EXISTS ext_lifecycle_current_insert_matches_event;
DROP TRIGGER IF EXISTS ext_lifecycle_current_update_matches_event;
DROP TRIGGER IF EXISTS ext_lifecycle_current_insert_heads_latest_event;
DROP TRIGGER IF EXISTS ext_lifecycle_current_update_heads_latest_event;

CREATE TRIGGER ext_lifecycle_current_insert_matches_event
BEFORE INSERT ON ext_lifecycle_current
FOR EACH ROW
WHEN EXISTS (SELECT 1 FROM ext_lifecycle_events WHERE event_id = NEW.last_event_id)
 AND NEW.state IS NOT (SELECT to_state FROM ext_lifecycle_events WHERE event_id = NEW.last_event_id)
BEGIN
    SELECT RAISE(ABORT, 'ext_lifecycle_current.state must equal the to_state of last_event_id');
END;

CREATE TRIGGER ext_lifecycle_current_update_matches_event
BEFORE UPDATE ON ext_lifecycle_current
FOR EACH ROW
WHEN EXISTS (SELECT 1 FROM ext_lifecycle_events WHERE event_id = NEW.last_event_id)
 AND NEW.state IS NOT (SELECT to_state FROM ext_lifecycle_events WHERE event_id = NEW.last_event_id)
BEGIN
    SELECT RAISE(ABORT, 'ext_lifecycle_current.state must equal the to_state of last_event_id');
END;

CREATE TRIGGER ext_lifecycle_current_insert_heads_latest_event
BEFORE INSERT ON ext_lifecycle_current
FOR EACH ROW
WHEN EXISTS (
    SELECT 1 FROM ext_lifecycle_events newer
    WHERE newer.tenant_id = NEW.tenant_id
      AND newer.knowledge_base_id = NEW.knowledge_base_id
      AND newer.document_id = NEW.document_id
      AND newer.sequence > (SELECT sequence FROM ext_lifecycle_events WHERE event_id = NEW.last_event_id)
)
BEGIN
    SELECT RAISE(ABORT, 'ext_lifecycle_current.last_event_id must be the latest committed event of the document');
END;

CREATE TRIGGER ext_lifecycle_current_update_heads_latest_event
BEFORE UPDATE ON ext_lifecycle_current
FOR EACH ROW
WHEN EXISTS (
    SELECT 1 FROM ext_lifecycle_events newer
    WHERE newer.tenant_id = NEW.tenant_id
      AND newer.knowledge_base_id = NEW.knowledge_base_id
      AND newer.document_id = NEW.document_id
      AND newer.sequence > (SELECT sequence FROM ext_lifecycle_events WHERE event_id = NEW.last_event_id)
)
BEGIN
    SELECT RAISE(ABORT, 'ext_lifecycle_current.last_event_id must be the latest committed event of the document');
END;
