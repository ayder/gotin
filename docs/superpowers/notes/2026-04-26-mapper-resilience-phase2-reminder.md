# Phase 2 Reminder — Mapper Resilience: Description Cleaning + Hash Redesign

**Created:** 2026-04-26
**Status:** **GATED — DO NOT START until Phase 1 logs exist and have been inspected.**
**Phase 1 spec:** `docs/superpowers/specs/2026-04-26-mapper-resilience-phase1-design.md`
**Branch:** continue on `mapper-resilience` (or branch off `mapper-resilience-phase2`).

## Hard gate

Phase 2 spec writing begins only after **all** of the following are true:

1. Phase 1 has shipped on `mapper-resilience` and is in use.
2. The user has captured `gotin.log` from at least one `t2tmud.org` session run
   with `-debug`. Session must include:
   - ≥10 rooms visited
   - ≥1 revisit of a previously seen room
   - ≥1 weather / time-of-day tick observed
   - ≥1 NPC, item, and other-player line observed if possible
3. The log has been inspected jointly with the user and the **inspection
   checklist** below has been answered. Anything written before this is
   guesswork and will be wrong.

## Inspection checklist (answer before drafting Phase 2 spec)

Run these against the captured `gotin.log` JSON Lines:

1. **Stable identifiers**
   - Does any `gmcp/room_info` entry contain a non-empty `vnum`?
   - Does any `mxp/tag` entry have a tag name other than `ROOMNAME` that looks
     id-like? (Candidates seen on other MUDs: `RNum`, `ROOM` with id attribute,
     `RVNum`.) Capture the exact tag name and attribute structure.
2. **Description source quality**
   - For `gmcp/room_info`: is `desc` populated and clean (no weather / NPC /
     item / player lines), or contaminated?
   - Are there MXP tags wrapping the description (`RDesc`, `ROOM`, etc.)?
3. **Dynamic-line corpus**
   - Collect representative lines for each contamination class on `t2tmud`:
     weather/time, NPC presence, item-on-ground, player presence, combat spam,
     scripted ambient flavor.
   - Note: regex shape (anchored sentence forms, capitalized name patterns,
     trailing "is here." / "lies here." / etc.).
4. **Structural boundaries**
   - Is "Obvious exits:" / "The only obvious exit" a reliable terminator for
     the static-description block?
   - Does the room name always appear on its own line directly above the
     description?
5. **Stability under revisit**
   - When the same room is revisited, does the description match byte-for-byte
     after stripping dynamic lines? Or is there residual variation
     (capitalization, punctuation)? This determines whether normalization is
     needed in the hash.

Record the answers inline in the Phase 2 spec under a "Log Findings" section so
the design decisions are traceable to evidence.

## Anticipated Phase 2 deliverables

(Subject to revision based on the log findings — do not commit to these without
evidence.)

1. **Structural extractor.** Prefer (in order):
   1. MXP description tag content if present (e.g. `<RDesc>` body).
   2. GMCP `Room.Info.desc` if present and the inspection confirms it is clean.
   3. Raw-text heuristic extractor (see #2).
2. **Raw-text heuristic.** Walk the room block, split on the room-name line and
   the exits line, drop any interior line matching the per-MUD dynamic-line
   regexes derived from the corpus.
3. **Hash redesign.**
   - Hash inputs: `(normalized full static description) | (sorted exits) |
     (room name when known)`.
   - Normalization: trim leading/trailing whitespace, collapse internal runs of
     whitespace to a single space, lowercase **only if** the inspection shows
     case is unstable.
   - Replace `desc[0:50]` truncation with full description.
4. **Per-MUD profile** in `gotin.json`:
   ```json
   "mud_profiles": {
     "t2tmud.org": {
       "description_filters": [
         { "pattern": "is here\\.$", "drop": true, "comment": "NPC/player presence" },
         "..."
       ]
     }
   }
   ```
   Match by host on connect; auto-apply.
5. **Optional — fingerprint structure.** Per-room store of multiple identity
   signals: `vnum`, `mxp_id`, `desc_hash`, `name_exits_hash`. Loop detection
   matches on any signal that is present. Useful when the MUD emits different
   identifiers at different times (e.g. vnum on first entry, only name on
   subsequent entries).

## Phase 2 acceptance criteria (template — finalize from log findings)

- With `Hash=on, Vnum=on`, walk a known cycle of ≥30 rooms on `t2tmud.org`. The
  cycle is detected on the second pass; no false collapses across the cycle.
- Per-MUD profile is regression-testable: a checked-in fixture log produces a
  deterministic graph when fed through the mapper offline.
- `Hash=off` behavior unchanged from Phase 1 (still one-room-per-dig).

## Migration / compatibility

- New defaults stay `vnum=on, hash=off`; users with broken Phase 1 maps can
  delete and re-walk. No automatic migration of existing maps.
- Old hash values in saved maps become stale once the formula changes. Plan for
  a one-shot rebuild on map load (rebuild `hashIndex` using the new formula),
  similar to existing `rebuildHashIndex` in `mapper.go:344`.

## Don't-forget list

- Phase 1 leaves `Hash=on` keying on the broken short-hash formula. When the
  formula changes in Phase 2, document that any user who had `Hash=on` will
  need to delete their map. (Or implement a per-room `hash_version` field for
  graceful upgrade.)
- Vnum cross-MUD collisions are still an open issue; consider namespacing
  `roomID = host + ":" + vnum` in Phase 2 if the user maintains maps for
  multiple MUDs.
- Add a fixture (`internal/mudproto/mxp/testdata/t2tmud_chunk.txt`) from the
  captured log so Phase 2 changes can be regression-tested without a live MUD.
