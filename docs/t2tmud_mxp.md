# t2tmud.org — MXP Tag Reference

Reference catalogue of MXP tags actually emitted by `t2tmud.org` in a live
session, captured via the Phase 1 `protolog` JSON-Lines logger.

This document is intended for Phase 2 design (mapper resilience, structural
extraction) and for future MXP work. It is descriptive, not normative — what
t2tmud sends, with examples and where the data lives in the byte stream.

## Capture context

- **Source:** `gotin.log` from sessions on 2026-04-26 (Grey Havens area and
  the wilderness plains east of it), with the expanded `<SUPPORTS>`
  advertisement (`internal/mudproto/mxp/mxp.go:265`) and the quote-aware
  tag terminator + split-tag buffering fixes
  (`internal/mudproto/mxp/mxp.go:findTagEnd`, `Protocol.pending`).
- **Filter:** `mxp.Protocol.Filter` strips every tag below from the visible
  text stream. Identifiers and attributes survive only in the `protolog`
  `tag` events (`source: "mxp"`, `event: "tag"`).
- **Mode interaction:** t2tmud uses `\x1b[7z` (LockLocked) as the persistent
  default and prefixes each interpreted tag with `\x1b[4z` (TempSecure,
  next-tag-only). Our parser tracks both correctly.
- **Negotiation prerequisite:** these tags only appear after a successful
  MXP handshake. The handshake fails unless `<SUPPORTS>` advertises every
  capability the server probes for in `<SUPPORT>` — see
  `mxp.go:sendSupports` for the full list.

## What is *not* present

The Phase 2 reminder hypothesised these. None observed on t2tmud:

- `<ROOMNAME>` / `<ROOM>` / `<RDESC>` / `<RNAME>` / `<RNUM>` — no per-room
  identifier or wrapped description. Room name and body arrive as raw
  ANSI-coloured text.
- `<A href=…>` — t2tmud uses `<SEND>` (via custom elements) for hyperlinks,
  not `<A>`.
- GMCP `Room.Info` package — server negotiates GMCP and accepts our
  `Core.Hello` / `Core.Supports.Set` but emits zero `Room.Info` events.

## Catalogue

### Handshake / capability tags

#### `<VERSION>`

Bare probe sent by server during MXP negotiation. Client must reply with a
SECURE-line `<VERSION CLIENT="…" VERSION="…">` or the server downgrades and
prints `No MXP client support; MXP disabled.`

```
\x1b[1z<VERSION>\x1b[0m
```

#### `<SUPPORT …>`

Server queries which capabilities the client implements. Body is a
space-separated list of feature names with optional dotted sub-options.

```
<SUPPORT a send expire image gauge u b i font.face font.size font.color font.back>
```

Client replies with `<SUPPORTS +A +SEND +EXPIRE …>` listing each feature it
will accept. Advertising less than the server probed for triggers a downgrade
to plain text — t2tmud silently stops emitting room-content tags.

### Element / entity definition (sent once, on MXP enable)

#### `<!el name 'definition' [att='attr-name']>` — element definition

t2tmud declares 16 custom shortcut elements at the start of every session.
Each compresses a `<SEND>` template with attribute substitution into a one
or two-letter tag name. Definitions arrive in a single burst, all on
`\x1b[4z` TempSecure.

| Element | Definition (abbreviated) | Used for |
| :--- | :--- | :--- |
| `<x>` | `<send href="&text;\|l &text;" hint="go &text;\|look &text;" expire="room_exits">` | Land exits (single direction word) |
| `<xx>` att=`dir` | same, substitutes `&dir;` | Land exits with explicit direction attribute |
| `<t>` | `<send href="travelto &text;\|…">` | Signpost / travelto destinations |
| `<w>`, `<ww>` | `<send href="swim &text;">` | Water exits |
| `<l>`, `<ll>` | `<send href="launch &text;">` | Boat-launch points |
| `<y>`, `<yy>` | `<send href="land &text;\|l &text;">` | Boat-landing points |
| `<z>` | `<send href="l" hint="look">` | Generic "look here" link |
| `<d>` | `<send href="l &text;\|open &text;\|close &text;\|unlock &text;\|lock &text;\|pick &text;\|bash &text;" expire="door_links">` | Door references in detailed descriptions |
| `<m>` | `<send href="f\|b" hint="Next page\|Previous page" expire=ml>` | `--More--` paginator |
| `<hh>`, `<hs>` att=`fn` | `<send href="help &text;">` / `&fn;` | Help-file cross-references |
| `<i30>` att=`id` | `<send href="look &id; on ground\|align &id;\|consider &id;\|kill &id;" expire="obj_links">` | NPCs / monsters (right-click menu) |
| `<i9>` att=`id` | `<send href="look &id; on ground\|get all from &id; from ground" expire="obj_links">` | Containers / items (right-click menu) |

> **Phase 2 implication:** the element definitions tell us which classes of
> hyperlink expire together (`room_exits`, `door_links`, `obj_links`,
> `ml`). The tag `name` itself (`x`, `i30`, `d`, …) functions as the
> semantic role marker.

> **Parser caveat:** these definitions contain a NESTED `<send …>` inside
> the single-quoted body. A naive `IndexByte('>', s)` closes at the inner
> `>` and leaks the trailing `'>` to the UI — see
> `internal/mudproto/mxp/mxp.go:findTagEnd` for the quote-aware fix.

#### `<!en name value>` — entity definition

Sets named entity for later substitution. t2tmud sends an initial seed for
the HP/EP gauge entities.

```
<!en hp 70>
<!en maxhp 70>
<!en ep 70>
<!en maxep 70>
```

Also implied to be sent when stats change (re-emit). Phase 2 / status-bar UI
might consume these directly.

### Status-bar metadata

#### `<gauge entity max-entity caption='…' color=…>`

Defines a graphical gauge backed by entity values. Sent once after MXP
enable. t2tmud sends two:

```
<gauge hp maxhp caption='HP' color=red>
<gauge ep maxep caption='EP' color=green>
```

Mapper ignores; useful only for a future status-bar UI.

### Per-room / inline tags (the Phase-2 substrate)

#### `<expire [name]>` — hyperlink expiration marker

Bare `<expire>` (no class name) means "expire ALL active hyperlinks".
t2tmud emits this **at the start of every room-block render**, immediately
after the `\x1b[0;32m` colour switch and before the description text:

```
\x1b[0;32m\x1b[4z<expire>    A paved street of interlocking stone slabs. …
```

> **Phase 2 implication:** `<expire>` is a reliable room-block-start marker
> we should use as the buffer terminator, in preference to (or alongside)
> the `Obvious exits` line.

#### `<x>direction</x>` — land-exit hyperlink

Wraps each exit name in the "Obvious exits" line. The text inside is the
displayed direction; the element template builds the underlying `go` /
`look` command.

Always nested in `\x1b[1;37m` (white).

```
The only obvious exits are \x1b[1;37m\x1b[4z<x>east</x>\x1b[0m\x1b[0;32m,
\x1b[1;37m\x1b[4z<x>northwest</x>\x1b[0m\x1b[0;32m,
\x1b[1;37m\x1b[4z<x>west</x>\x1b[0m\x1b[0;32m and
\x1b[1;37m\x1b[4z<x>southwest</x>\x1b[0m\x1b[0;32m.
```

#### `<w>direction</w>` — water-exit hyperlink

Wraps swim destinations in a separate "There is water to the …" line that
appears on the line **above** the regular exits line, when the room
borders water. The element template uses `swim &text;`. Always nested in
`\x1b[1;36m` (cyan), distinguishing it visually from land exits in white.

```
    There is water to the \x1b[1;36m\x1b[4z<w>north</w>\x1b[0m\x1b[0;32m
    and \x1b[1;36m\x1b[4z<w>northeast</w>\x1b[0m\x1b[0;32m.
    The only obvious exits are \x1b[1;37m\x1b[4z<x>east</x>…
```

The `xx`/`ww`/`l`/`ll`/`y`/`yy`/`t`/`z` tags share the same shape but
target different commands (see element table). They were not exercised in
this capture but the definitions are present.

> **Phase 2 implication:** structural exit extraction. Collect text inside
> every `<x>` / `<xx>` / `<t>` / `<w>` / `<ww>` / `<l>` / `<ll>` / `<y>` /
> `<yy>` tag pair within the room block. Treat land and water exits as a
> single ordered set when hashing — the room signature should not depend
> on which "kind" of exit a given direction is, only on the direction
> itself, since two rooms reachable by `n` are the same regardless of
> whether one of them is reached by `swim`.

#### `<i30 "keyword">name</i30>` — NPC presence line

Wraps each NPC entry in the post-exits "people here" lines. The attribute
is the in-game target keyword (which `attack`, `consider`, etc. accept);
the body is the player-facing name.

```
 \x1b[4z<i30 "seagull 1">\x1b[1;33mAn injured seagull</i30>
\x1b[4z<i30 "seagull 1"></i30>\x1b[0;32m
```

(Yes, the close-tag is followed by an immediate empty `<i30 …></i30>`
re-open. This appears to be a t2tmud quirk for stacked count handling — it
also surrounds count badges like `[2]`.)

> **Phase 2 implication:** these lines must be DROPPED from the description
> hash. Detecting them is now trivial — the line contains an `<i30>` tag.

#### `<i9 "keyword">name</i9>` — item / container presence line

Same shape as `<i30>` but for inanimate objects (e.g. trash cans).

```
 \x1b[4z<i9 "trash can 1">\x1b[1;35mA trash can</i9>\x1b[4z<i9 "trash can 1"></i9>
```

> **Phase 2 implication:** same as `<i30>` — drop from description hash.
> The yellow vs magenta colour distinction (`\x1b[1;33m` for NPCs,
> `\x1b[1;35m` for items) is a redundant signal on top of the tag class.

#### `<d>door</d>` — door reference

Wraps door words inside detailed descriptions (rare in standard movement;
seen in `look` output). Class `door_links` — expires together when
movement triggers a new `<expire>`.

```
The \x1b[4z<d>oak door</d> is closed.
```

### Built-in `<send>` (referenced but not seen directly)

t2tmud never emits `<SEND>` directly; it always wraps it in a custom
element (`<x>`, `<i30>`, etc.). The element definition is what expands at
the server side; the inline tag is the short alias.

## Non-tag-marked dynamic content

Several classes of room-block content carry **no MXP markers** at all and
must be detected by line patterns. They sit between the static description
and the exits line, all in the same `\x1b[0;32m` colour as the description
body. This is the corpus Phase 2's heuristic line filter must handle.

Block layout observed:

```
<expire>    <static description, multi-line> 
<nearby-sights line(s)>             ← appear before weather, when present
<weather line(s)>                   ← always present (1–2 lines)
<door-state line(s)>                ← when a closed door faces the room
    There is water to the <w>…</w>. ← only when room borders water
    The only obvious exit(s) is/are <x>…</x>.
 <NPC and item presence lines>
HP:n EP:n [STATE] >                 ← prompt = end of block
```

### Weather lines

Always 1–2 lines, immediately before the exits line (or before the water
line, if any). The two-line form pairs a sky-state sentence with a
secondary atmospheric clause.

Examples observed in this capture (night):

```
The sky is black and the stars shine down brightly.
The sky is brilliantly clear.
```

From an earlier session (twilight):

```
The sky is dark blue and a yellow glow comes from the west.
A crystal clear sky hovers over the landscape, dotted with a few small clouds.
```

**Detection heuristic (provisional):**

- `^The sky is .+\.$`
- `^A .+ sky .+\.$`

Both match other generic prose, so use ONLY in the band between the static
description and the exits line. Phase 2 should sample more weather strings
(sunrise / sunset / overcast / rain) before locking the regex.

> **Phase 2 implication:** drop from the description hash. They are
> time-of-day-driven and reset on every visit.

### Daytime indicator (implicit)

t2tmud has no separate "It is dawn" line. **Time of day is encoded in the
weather sentence** — you must read the sky state to know whether it's
night, twilight, noon, etc. Since we drop weather from the hash, we also
drop the daytime signal — which is correct, because the same room exists
across all times of day.

> **Phase 2 implication:** no special handling needed. The weather drop
> handles daytime drop transitively.

### Nearby-sights lines

Free-form English sentences describing distant landmarks visible from the
current room. **Position-deterministic but description-coupled:** a chain
of rooms that all share the same static description ("Gently rolling
plains extend into the distance…") will each emit a *different* nearby-
sight depending on the player's position relative to the landmark.

Examples from one wilderness traversal:

```
The Lune River lies north and northeast.
The Lune River lies northwest and north.
The Lune River lies northwest.
The White Towers rise up to the southeast.
The White Towers rise up to the east.
```

Same-description rooms differ only by:

- which landmark sentence is present (or none, if no landmark is visible)
- the direction(s) named in that sentence

This means **the nearby-sight line is the primary disambiguator** between
two rooms that share their static description. Treating it as dynamic and
dropping it would re-create exactly the false-collapse bug Phase 2 is
trying to fix on the wilderness corpus.

**Detection heuristic (provisional):**

- Sentence shape: `^[A-Z][^.]+ (lies|rises?|rise up to|stands?|stretches?|extends?) .* (north|south|east|west|northeast|northwest|southeast|southwest|up|down)( and (north|south|east|west|northeast|northwest|southeast|southwest|up|down))?\.$`
- Always before the weather lines.
- Always title-case proper-noun subject.

The pattern is fragile — t2tmud area writers are free to use any verb.
Pragmatic alternative: **do not try to identify nearby-sights by pattern;
instead define the description as "everything between `<expire>` and the
weather line, INCLUDING any line that does not match a known dynamic
pattern."** That captures nearby-sights correctly without enumerating
their phrasings.

> **Phase 2 implication:** **KEEP nearby-sight lines in the hash.** They
> are the only feature that distinguishes same-description rooms in the
> wilderness, where t2tmud reuses paragraphs across many tiles. Define
> the hashable static block as "post-`<expire>` body MINUS weather
> sentences MINUS door-state lines MINUS presence lines (post-exits)" —
> NOT "first paragraph only".

### Door-state lines

When a closed door faces the room, a sentence reports its state. Captured
in earlier sessions (Grey Havens lighthouse street):

```
The south door is closed.
```

Likely also `is open`, `is locked`, etc. The door word itself may or may
not be wrapped in `<d>` (the captured example was raw text, not tagged —
this needs more samples).

**Detection heuristic (provisional):**

- `^The .+ (door|gate|portcullis) is (open|closed|locked|broken)\.$`

> **Phase 2 implication:** drop from the description hash. State is
> mutable; the same room with the door open vs. closed must hash
> identically.

### NPC / item presence lines (after the exits line)

Already covered by the `<i30>` and `<i9>` tags. These lines always appear
*after* the exits line, leading with a single space. They never appear
within the static-description band. Phase 2 just needs to not include
anything after the exits-line terminator.

### Ambient / event flavour

Spontaneous one-liners that can appear inside or outside a room block:

```
The Mirkwood victory gives a swiftness to your step.
Amorphorn says in some strange tongue: glith' adthal dearly einel
    dilglith aegdor.
Amorphorn whispers in some strange tongue: ...
```

Colour `\x1b[1;32m` (bright green) for self-targeted feedback;
combat / speech is uncoloured or coloured per channel.

> **Phase 2 implication:** these lines arrive *outside* the room block —
> they appear after the prompt of one room and before the `<expire>` of
> the next. Block buffering between `<expire>` and prompt naturally
> excludes them.

## Color-vs-tag overlap

After tag stripping, ANSI colour codes still encode role:

| Code | Role |
| :--- | :--- |
| `\x1b[0;32m` | Room description body (dark green) |
| `\x1b[1;37m` | Land-exit name (white) — also wrapped in `<x>` |
| `\x1b[1;36m` | Water-exit name (cyan) — also wrapped in `<w>` |
| `\x1b[1;33m` | NPC name (yellow) — also wrapped in `<i30>` |
| `\x1b[1;35m` | Item name (magenta) — also wrapped in `<i9>` |
| `\x1b[1;32m` | Ambient / event flavour (bright green) |
| `\x1b[0m` | Reset |

For Phase 2, MXP tags are the more reliable signal (work even if the user
disables colour). Colour can serve as a fallback when MXP is off.

## Sample raw chunks

### Grey Havens — paved street with NPC

Captured 2026-04-26T17:21:42. Simple urban room with one presence line
and no nearby-sight or water exits.

```
\x1b[0;32m\x1b[4z<expire>   A paved street of interlocking stone slabs. Hundreds of seagulls\n
patrol the streets of the Havens, squawking incessantly. Besides\n
the cries of the gulls you can hear the crashing of waves, telling you\n
that the sea is not far away at all.\n
The sky is black and the stars shine down brightly.\n
The sky is brilliantly clear.\n
    The only obvious exits are \x1b[1;37m\x1b[4z<x>east</x>\x1b[0m\x1b[0;32m, \x1b[1;37m\x1b[4z<x>northwest</x>\x1b[0m\x1b[0;32m, \x1b[1;37m\x1b[4z<x>west</x>\x1b[0m\x1b[0;32m and \x1b[1;37m\x1b[4z<x>southwest</x>\x1b[0m\x1b[0;32m.\n
 \x1b[4z<i30 "seagull 1">\x1b[1;33mAn injured seagull</i30>\x1b[4z<i30 "seagull 1"></i30>\x1b[0;32m\x1b[0m\n
\x1b[0m\x1b[0mHP:70 EP:70 [\x1b[1;32mWA\x1b[0m] > \x1b[0m
```

Layout: `<expire>` start, 4 prose lines + 2 weather, exits line, one
`<i30>` NPC line, prompt.

### Lune plains — same description, different nearby sights

Captured 2026-04-26T17:42:38–17:42:49. Five visits to wilderness rooms
that ALL share the same static description but represent five distinct
map tiles. Each is disambiguated only by its nearby-sights line and
sometimes a water-exits line.

Common static body (identical across all five):

```
\x1b[0;32m\x1b[4z<expire>    Gently rolling plains extend into the distance.  The grass sways
before the wind in unbroken waves, with isolated trees, like distant
ships, completing the nautical likeness.  This tranquil plain manifests a
sense of peace all the more notable for its contrast with the war-torn 
lands that lie far beyond.  
```

Per-tile distinguishing block (between description and exits):

| Tile | Nearby-sight line(s)                              | Water-exits line                                            |
| :--- | :------------------------------------------------ | :---------------------------------------------------------- |
| 1    | `The Lune River lies north and northeast.`        | `    There is water to the <w>north</w> and <w>northeast</w>.` |
| 2    | `The Lune River lies northwest and north.`        | `    There is water to the <w>northwest</w> and <w>north</w>.` |
| 3    | `The Lune River lies northwest.`                  | `    There is water to the <w>northwest</w>.`                |
| 4    | (none — past the river)                           | (none)                                                      |
| 5    | `The White Towers rise up to the southeast.`      | (none)                                                      |

All five emit the same exits set: `<x>east</x>, <x>north</x>, <x>west</x>
and <x>south</x>`. All five share the same prompt and weather lines. The
ONLY signals separating them are the nearby-sight line and the water-exit
line — both are room-deterministic and must remain in the hashable
description for Phase 2 to map this terrain correctly.

### White Towers heath — different description

Captured 2026-04-26T17:42:59. Demonstrates a distinct room with a fully
different paragraph plus a nearby-sight line.

```
\x1b[0;32m\x1b[4z<expire>    Soaring hills lift this verdant heath skyward.  Despite their height,
the summits are green, not stony.  In the rare places where the moorland
plants fail, only a smooth chalky soil is visible beneath.  Hanging over
even the mightiest of the hills are three towers whiter yet than the chalk.
Their appearance here, alone among the wilderness and taller than earthly
forces can easily account for, confounds the senses.  The west wind
carries a faint but recognizable smell of salt.
The White Towers rise up to the east.
The sky is black and the stars shine down brightly.
The sky is brilliantly clear.
    The only obvious exits are \x1b[1;37m\x1b[4z<x>east</x>\x1b[0m...
```

## Phase 2 design hints (cross-reference)

- **Room block boundary:** `<expire>` (start) → prompt `HP:n EP:n [STATE] > `
  (end). Both reliable when MXP is on. The block buffer should accumulate
  every chunk between these markers before handing to the mapper.
- **Exit set:** text content of `<x>`, `<xx>`, `<t>`, `<w>`, `<ww>`, `<l>`,
  `<ll>`, `<y>`, `<yy>`, `<z>` tags in order of appearance, normalised to
  the mapper's short form. Treat land and water exits uniformly when
  hashing — the direction is what identifies the destination, the verb
  (`go` vs `swim`) is presentation.
- **Drop from hash (always):** any line containing `<i30>` or `<i9>` —
  presence lines, after the exits line.
- **Drop from hash (always):** weather lines — pattern `^The sky …` or
  `^A .+ sky …`. Need a richer corpus to nail down the regex.
- **Drop from hash (always):** door-state lines —
  `^The .+ (door|gate|portcullis) is (open|closed|locked|broken)\.$`.
- **KEEP in hash (critical):** nearby-sight lines. They are the ONLY
  signal that distinguishes wilderness rooms sharing identical static
  paragraphs. Strategy: hash everything between `<expire>` and the first
  weather line, MINUS lines matching dynamic patterns. Do NOT enumerate
  nearby-sight phrasings — accept anything that isn't weather, isn't a
  door-state line, and isn't a presence line.
- **No vnum, no MXP id, no GMCP Room.Info on t2tmud** — hash of cleaned
  description + sorted full exit set remains the only identity strategy.
  The `Vnum` switch is harmless but inert here.

## Logger plumbing reference

To re-capture against this MUD:

1. `gotin.json`: ensure `connections.t2t.protocols.MXP = true`.
2. Run with `-debug`.
3. Open `gotin.log` and filter:
   - `grep -E '"source":"mxp","dir":"rx","event":"tag"'` — every parsed tag
     with `parsed.name` and `parsed.body`.
   - `grep -E '"source":"mxp".*"event":"chunk"'` — every input chunk to the
     filter (pre-strip), useful for split-tag debugging.
   - `grep -E '"source":"raw"'` — pre-IAC bytes, useful for telnet-level
     issues.

Each line is a self-contained JSON object — pipe through `jq` for
exploration.
