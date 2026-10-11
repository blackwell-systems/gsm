# Invariant coverage

This page measures how much of a real system gsm covers. It catalogs {{N}} business invariants
of the kind a product engineer writes down (inventory, orders, payments, hotel bookings, loyalty,
subscriptions, approvals, access control, scheduling, and consistency across services), and
classifies each on two axes: can gsm **express** it today, and can gsm **verify** it at
production scale today. It then ranks the extensions that would move the most invariants to
"verified at production scale".

It describes gsm v0.16.0 (the regime report, `CheckMigration` and events with parameters).
Nineteen of the classifications are demonstrated by programs on this page that the doc test runs. The page is generated from a data file
([`docs/coverage/`](coverage/README.md)): edit the catalog or the template there and rerun the
generator, never this page by hand.

## Contents

- [Headline](#headline)
- [Method](#method)
- [What the classification rests on](#what-the-classification-rests-on)
- [Results](#results)
- [Ranked extensions](#ranked-extensions)
- [Validated samples](#validated-samples)
- [The catalog](#the-catalog)

## Headline

{{SUMMARY}}

- **gsm can state almost every invariant, at some size.** {{EXPR_P}} are expressible: with
  combinators, with a closure, or through an encoding (an owner or last-writer-wins register, one
  registry holding the items a rule relates, facts recorded as flags). The {{EXPRNO_N}} that are not ask for
  something outside convergence by compensation: a deadline, an answer that must never be revised,
  or exactly-once delivery by the transport. One more (a two-way sync between services) is
  expressible only with coordination that gsm assumes rather than checks.
- **It verifies {{YES_P}} at production scale.** Each of these is a rule about one entity at a
  time (an order, a payment, a hold, a request, a room-night), checked through a collection or by
  reusing one machine per entity, or a projection across services checked as a federation. Every
  lifecycle rule in the catalog is in this group. Events with parameters added 17: registers,
  owners, deadlines and snapshots whose values arrive on events and are only compared and copied,
  checked by abstraction over values as wide as the domain needs. Without lifecycle rules and
  delivery or deadline guarantees, {{HARD_P}} ({{HARD_YES}} of {{HARD_N}}) of the harder kinds
  verify at scale.
- **No balance or amount rule verifies at scale**, cross-item rules only where a key turns them
  into one owner register (2 of 11), and of the totals and counts only those that fit two small
  counters per entity (4 of 16). Arithmetic over wide ranges, aggregates and cross-item
  constraints block most of the {{TOY_N}} invariants ({{TOY_P}}) that gsm checks only at toy scale.
  Parameters can carry an amount, but abstraction refuses arithmetic on it, and expanding every
  value of an amount is a toy.
- **The next levers are the linear route and an aggregate reduction.** Each moves
  {{ALONE_LIN}} invariants alone; together, {{BESTPAIR}}; with cross-item relational constraints,
  {{BESTTRIPLE_N}}, which would take gsm from {{YES_P}} to {{TOP3_P}}. All the listed extensions
  together reach {{CEIL_P}}; the remaining {{NONE_N}} need coordination, a liveness property, or
  nonlinear arithmetic.

## Method

**The catalog.** Invariants are stated as a product engineer would state them, ten domains,
including the kinds known to be hard for coordination-free systems: totals and aggregates,
balances with arithmetic, uniqueness, referential integrity, temporal rules (expiry, "never
twice", precedence), ordering ("state only moves forward") and cross-item rules (no double
booking). The selection is one author's; the per-kind table below lets a reader reweight it.

**Production sizes.** "At production scale" means the domain sizes below, not a shrunk model.

| Domain | Sizes assumed |
|---|---|
| Inventory and warehouse | 10⁴ to 10⁵ SKUs, 10 to 100 locations, up to 10,000 units per SKU and location |
| Orders and fulfillment | 10⁶ orders a year, up to 50 lines and 1,000 units a line, 10 to 100 fulfillment centers |
| Payments, wallets and ledgers | Amounts in cents up to $10M (2³⁰), balances wider |
| Bookings and reservations | 50 to 1,000 rooms a property, up to about 250 per room type, a 365-night window, 10⁵ reservations a year |
| Loyalty points | 10⁶ members, balances up to 10⁷ points, 4 tiers |
| Subscriptions and billing | 10⁶ accounts, about 10 plans, up to 10,000 seats, up to 10⁶ usage units a period |
| Approvals and workflows | Approver pools of up to 16 per cost center; amounts in cents |
| Access control and quotas | 10⁵ to 10⁶ users, about 20 roles, quotas up to 10⁶ requests a month |
| Scheduling and capacity | 10² to 10³ staff, 15-minute slots over a year (35,040) |
| Multi-service consistency | 3 to 10 services, state per entity |

**Expressible today**, one of:

- **combinator**: written with gsm's combinator vocabulary (`Rule`, `On`, `DeclInvariant`,
  comparisons, `Add`, `Sub`, `And`, `Or`, `Not`), possibly through a collection or a federation;
- **closure**: needs a Go closure (multiplication, rounding, a loop over many variables);
- **encoding**: needs a modeling device, sketched in the catalog row: a fact per milestone with the
  status derived, two counters that only rise instead of one that rises and falls, one event per
  value an event would carry, one registry holding every item a rule relates, a deadline recorded
  as a fact event from a timer;
- **no**: no faithful statement exists at any size.

An encoding counts only when it keeps the business meaning. Turning "decline a withdrawal the
balance does not cover" into "hold it pending until a deposit covers it" counts, because many
products work that way and the row says so. An answer the business requires to be final the
moment it is given does not count: those rows are "no".

**Verifiable at production scale today**, one of:

- **yes**, naming the path that verifies the faithful model at the sizes above: `Build` (at most
  2²⁰ states), a collection (a template within `Build`'s limit or declared with `Abstract`),
  `Abstract` (at most 2²⁰ representative states), per-component checking (every component within
  20 bits), a federation (every component registry within `Build`'s limit), or `CheckMigration`
  (at most 2²⁰ reachable states on each side);
- **toy**: verifies only with the domains shrunk well below the sizes above (a handful of rooms,
  amounts up to a few hundred, one event per time slot over a few slots);
- **no**: not expressible, or what gsm can check does not establish the invariant.

When in doubt a row is marked toy or no.

**Blocking reason.** For a row that is not "yes", the first wall the faithful model hits:
arithmetic over wide ranges; an aggregate across items; history or time (which of two things
happened first in real time, a window, a snapshot); unbounded or relational data (uniqueness over
a set, joins); a closure that prevents a reduction; a cross-item constraint; or other (needs
coordination, a liveness property, a transport property, reductions that do not combine).

**Would move with.** For each blocked row, the smallest sets of extensions (from
[Ranked extensions](#ranked-extensions)) that would make the faithful model verifiable at
production scale, written `LIN + AGG`; alternatives are joined with "or". Event parameters
(`PARAM`) were the first extension on this list and are implemented, so they no longer appear in
the sets. An extension "moves"
a row alone when one of the row's sets is that extension by itself. The counts assume each
extension ships with its theorem as sketched there, and that the rest of the row's model stays
within the limits. They are estimates of reach, not implementations.

**Validation.** Nineteen programs below build the models behind representative rows (ten that
verify at scale, six that verify only as toys, three that cannot be expressed) and fail if gsm's
behavior changes. Each is a `run` block the doc test executes, and the oracle job's
example-machine gate checks every machine they make with the extracted checkers
(`.github/oracle/machines.txt`).

## What the classification rests on

These facts about gsm decide most rows. Each is checked by a program on this page or stated in the
docs it links.

- **One registry's state is one 64-bit word.** A registry cannot hold more than 64 bits of state
  at all ([T3](#t3)), and `Build` enumerates at most 20 bits whole. A rule that relates many items
  (every SKU of a warehouse, every reservation that could claim a room-night) needs one registry
  holding them, so it is expressible only at toy scale, unless a key turns it into one register
  per item ([V7](#v7)).
- **Events carry values.** An event declares integer parameters
  ([Getting started](getting-started.md#events-with-parameters)). Without `Abstract` each value is
  an event of its own, at most 1024 per event, which is fine for a few values and a toy for money
  or time. With `Abstract`, a parameter that is only compared and copied is checked at a few
  representative values, whatever its range ([Theory §11.12](theory.md#1112-event-parameters)).
- **Abstraction compares and copies.** `Abstract` refuses arithmetic (on variables and parameters
  alike), `Bool` and `Enum` variables and closures, and checks |representatives|ⁿ states, so it
  suits a few wide values fixed at creation or carried by an event and compared or copied
  ([V5](#v5), [V7](#v7) to [V10](#v10)), not a balance that moves ([T2](#t2)).
- **A count that rises and falls must be two counters.** One bounded counter that one event
  increments and another decrements fails CC at its bound, whatever the range ([T5](#t5)). Two
  counters that only rise commute, but each must cover lifetime totals, which doubles the bits.
- **A federation target cannot react to its authority.** M1 requires the overwrite alone to keep
  the target valid, so a target invariant that repairs its own state from a shared fact is refused
  ([V6](#v6)). Cross-service rules verify as projections, or in one registry holding both facts.
- **A closure sees outside data once.** A collection runs the template's tables, so a closure that
  reads a catalog or a rate reads it at build time ([T6](#t6)). Outside data has to enter as state,
  or on an event ([V10](#v10)).
- **Convergence hides arrival order.** A rule whose outcome depends on which of two facts arrived
  first is refused ([V8](#v8)); "which happened first in real time" has to be a value, carried by
  the event as a parameter and compared with a deadline in the state.
- **One machine serves every entity.** A `Machine` is a function of a `State`; an application
  keeps one state per order, payment or request, and `Build`'s result holds for each. A rule about
  one entity therefore verifies at scale when that entity's model passes a path, whether or not it
  goes through `NewCollection`. The same holds for a `FedMachine` with one `FedState` per entity,
  and for `CheckMigration` run on the per-entity registry (it does not take a `Collection`).
- **Facts fixed at creation can be state.** A value fixed when the entity is created (an order's
  total, the requester of an approval) can be set in the state before the first event, as
  [V3](#v3) and [V5](#v5) do, since every replica starts from the same creation record. A value
  that changes later has to arrive through an event.

## Results

### Where gsm verifies at scale today

{{PATHS}}

Collections and per-entity machines carry the result. Abstraction carries 18 rows: a credit-limit
hold over amounts fixed at creation, and the 17 rows whose values arrive on events as parameters
(owner and last-writer-wins registers, deadlines, versions and snapshots, per entity or per key).
Per-component checking carries none: it was never the path for a
row, because every per-entity model that verifies fits `Build` whole, and
every model too large for it is blocked by arithmetic, aggregation or the 64-bit word first.

### By kind

{{BYKIND}}

### By blocking reason

{{BYREASON}}

"Closure prevents reductions" is never the first blocker. Closures appear in rows that verify
(collection templates are enumerated, so closures are fine there) and in rows that need
multiplication or rounding, which are blocked by arithmetic first.

### By domain

{{BYDOMAIN}}

Approvals, orders and multi-service consistency score highest because their rules are mostly about
one entity's lifecycle. Access control, scheduling and inventory score lowest: quotas, rolling
windows, overlapping intervals and quantities on hand are arithmetic, time and cross-item rules.

## Ranked extensions

Each candidate, with the number of catalogued invariants it would move to "verified at production
scale", now that event parameters are implemented. **Alone** counts rows one of whose minimal sets
is that extension by itself (with parameters, which every set may now use); **on some route**
counts rows where it appears in some minimal set; **greedy gain** is what it adds when extensions are added
in the order of the table, each time picking the one that moves the most.

{{EXTTABLE}}

Cumulative, in the greedy order:

{{GREEDY}}

The best pair is {{BESTPAIR}}; the best triple, {{BESTTRIPLE}}, moves {{BESTTRIPLE_N}}. The
{{NONE_N}} rows no listed extension moves are {{IDS_NONE}}: coordination, liveness, the transport,
and one nonlinear proration formula.

Event parameters (`PARAM`), first on this list before they were implemented, moved 17 rows: every
last-writer-wins register, every "earliest claim wins" owner (a room-night, a bin, an email
address, a meeting slot), every rule that compares an event's time to a deadline fixed at
creation, and every snapshot ("the rate locked at booking"), because comparing a parameter and
copying it stays in abstraction's comparison fragment ([V7](#v7) to [V10](#v10)). They remain on
the route of most rows the extensions below would move, since an amount has to arrive on an event
before arithmetic can help.

### 1. General linear arithmetic (`LIN`)

Rules that add, subtract and multiply by literals over unbounded integers, decided by formulas
instead of enumeration. An amount now reaches the state on an event (a parameter), so it moves
{{ALONE_LIN}} rows alone: wallets, captures and refunds, ledgers, seat counts, spend limits, invoices
with a tax rate. On some route for {{PART_LIN}} rows.

- **Theory difficulty: medium, mostly done.** `lin_exact`, `phi_cc1_exact` and `lin_frag_linear`
  in `AbstractionCutoff.v` generate each condition as a quantifier-free linear-arithmetic formula
  whose validity is the condition. Open: gsm's `Int` writes saturate, which the formulas over the
  integers do not model; the generator needs a differential test against the Coq construction;
  and gsm takes no solver dependency.
- **Precedent:** Presburger arithmetic (Presburger, 1929; Cooper, 1972), SMT solvers, and
  solver-backed verifiers such as Ivy and Apalache.

### 2. Aggregate reduction (`AGG`)

A total or count over items, tracked as a variable that each item's events change by their
contribution, checked with one or two representative items and the aggregate rather than every
item. Alone it moves {{ALONE_AGG}} rows ({{IDS_AGG}}); on some route for {{PART_AGG}}, among them
warehouse capacity, storefront availability, ledger conservation, storage quotas and weekly hours.

- **Theory difficulty: high.** There is no positive theorem yet: normalization-confluence has
  the counterexample (`aggregate_diverges`: an aggregate converges on one item and diverges on
  two), so the cutoff for aggregates is not 1 and the reduction needs its own conditions. Counts
  compared to thresholds ("at least one admin", "open disputes above zero") are the tractable
  first step.
- **Precedent:** counter abstraction (Pnueli, Xu and Zuck, 2002), threshold automata and their
  reductions (Konnov, Veith and Widder, 2017).

### 3. Cross-item relational constraints (`XREL`)

Rules over a pair, or a small fixed number, of items: two shifts of one employee overlap, two
legs of a transfer, the nights of one stay, the next slot that takes the overflow. With the
items' times arriving as parameters, it moves {{ALONE_XREL}} rows alone ({{IDS_XREL}}), and is on
some route for {{PART_XREL}} rows; after `LIN` and `AGG` it adds {{GAIN_XREL}}.

- **Theory difficulty: high.** Repair across items breaks the per-key independence the collection
  theorems rest on (`cross_commute`). `SymmetryCutoff.v` already shows a cutoff of 2 for CC1 in
  its delayed-repair form (`cc1_cutoff`), which suggests the shape of a pairwise result.
- **Precedent:** cutoffs for systems with pairwise interaction (Emerson and Kahlon, 2000),
  invisible invariants (Pnueli, Ruah and Zuck, 2001), environment abstraction (Clarke, Talupur and
  Veith, 2006).

### 4. Set and uniqueness constraints (`SET`)

All-different and exactly-one over a set whose members change: one primary on call per day when
claims are released, at least one admin, one welcome bonus per person across member numbers.
Uniqueness over an immutable key does not need it: re-keyed by the unique value, it is an owner
register, which event parameters now cover ([V7](#v7)). On some route for {{PART_SET}} rows.

- **Theory difficulty: medium to high.** Equality-only reasoning has small cutoffs, but the rows
  here also need release and re-assignment.
- **Precedent:** data independence (Wolper, 1986; Lazić and Nowak, 2000).

### 5. Time and expiry encodings (`TIME`)

Timestamps as values, with deadlines and windows (t ≤ t₀ + w), packaged: in effect `PARAM` plus
`DIFF` over a time domain. Alone it moves {{ALONE_TIME}} row ({{IDS_TIME}}). Event parameters moved
the rows whose rules only compare times; points that expire 24 months after the last activity
(LOY-04) need the window arithmetic, so `TIME` is that row's only single extension. With
`AGG` it also covers rolling rate limits (ACL-05).

- **Theory difficulty: medium**, following `PARAM` and `DIFF`.
- **Precedent:** timed automata regions and zones (Alur and Dill, 1994; difference-bound matrices,
  Dill, 1989).

### 6. Difference-constraint arithmetic (`DIFF`)

Counters that move by constants, with guards x - y ≤ c, over unbounded integers. Alone it moves
{{ALONE_DIFF}} rows ({{IDS_DIFF}}): counters that need no amounts on events, only more range than
20 bits allow, or a net count (open disputes, concurrent sessions) whose two monotone counters
must cover lifetime totals.

- **Theory difficulty: medium.** A fragment between the comparison route and `lin_exact`. Its
  conditions are difference-logic formulas, decidable by negative-cycle detection without an SMT
  solver, which fits gsm's no-solver rule.
- **Precedent:** difference-bound matrices (Dill, 1989; Bengtsson and Yi, 2004).

### 7. A larger enumeration limit (`BITS`)

Raise `Build`'s 20-bit limit. Alone it moves {{ALONE_BITS}} rows ({{IDS_BITS}}), the ones a bit
or two over the limit. No theory; the cost doubles with each bit (step tables of 2ⁿ entries per
event), so the gain is a few bits, or symbolic enumeration (Burch et al., 1990; McMillan, 1993).

### 8. Combining reductions (`COMB`)

Abstraction with federations, with per-component checking, and collections with per-component
checking or migration. Alone it moves {{ALONE_COMB}} row ({{IDS_COMB}}: amounts compared across
two services). The catalog needs little of it because one machine per entity already composes
with every path, and every per-entity model that verifies fits `Build` whole.

- **Theory difficulty: low to medium.** `AbstractionCutoff.v` composes symmetry and abstraction
  (`sym_abs`) and `SymmetryCutoff.v` reduces C1 and C2 to one item (`c1_cutoff`, `c2_cutoff`);
  abstraction with C1, C2 and M1 needs stating.

### 9. History encoding helpers (`HIST`)

Sugar for facts that only rise, milestones with a derived status, max-registers and rounds. It
moves no row: every history encoding that verifies today already verifies ([V1](#v1)), and the
history rows that do not are blocked by real-time precedence, which needs values on events
(`PARAM`, `TIME`), not helpers. Worth doing for ergonomics; no theory.

### Not on the list

Partial-order reduction ([Roadmap 1d](ROADMAP.md#1d-partial-order-reduction-check-fewer-event-orders-not-just-fewer-states))
moves no row: it lowers the cost of checks over reachable states (migration, cycles), not the
domain sizes a rule needs. The {{NONE_N}} unmoved rows need coordination (an answer never revised,
gap-free numbering, a two-way sync that is not monotone), a liveness property (a deadline on
when events happen), or a transport property (exactly-once publication), all outside convergence
by compensation, plus one nonlinear formula (proration).

## Validated samples

Each program builds the model behind its rows and panics if gsm's behavior differs from the
classification. The doc test runs them.

### Verified at production scale

<a id="v1"></a>
#### V1. Order status only moves forward (ORD-03), collection

One `Bool` per milestone, the status derived as the furthest one. A late "packed" leaves a
delivered order delivered, every event is safe to redeliver, and the collection's report covers
every `OrderID`.

[[SNIP V1]]

<a id="v2"></a>
#### V2. Stop-sell per room type and night (BKG-02), collection at 2²⁰ states

Counters that only rise, per (room type, night), up to 511 bookings each, with the stop-sell and
overbooked flags derived: exactly `Build`'s limit, checked in well under a second. The report
asks for exactly-once delivery of `book` and `cancel`. This verifies the stop-sell flags at
production scale; which booking is walked when the type is oversold (BKG-03) does not verify.

[[SNIP V2]]

<a id="v3"></a>
#### V3. Approvals: thresholds, rejection, no self-approval (APR-01, APR-02, APR-03), Build

Eight approvers, a threshold and a requester fixed at creation: 27,648 states, every pair of nine
events checked. One machine serves every request.

[[SNIP V3]]

<a id="v4"></a>
#### V4. One capture per idempotency key, refunds held until capture (PAY-04, PAY-05, PAY-10), collection

Combinator rules, so both extracted oracles certify the template. No event needs deduplication.

[[SNIP V4]]

<a id="v5"></a>
#### V5. Credit-limit hold over amounts in cents (ORD-12), Abstract

Totals and limits up to 10⁹ cents, fixed at creation and only compared: abstraction checks 12
representative values per variable. Values that arrive on events instead are V7 to V10; a balance
that moves is refused ([T2](#t2)).

[[SNIP V5]]

<a id="v6"></a>
#### V6. GDPR erasure reaches every service (MSV-07), federation; a reacting target is refused (MSV-15)

As a projection the erasure verifies. A target invariant that drops a queued campaign once erased
is refused by M1, so MSV-15 verifies only as one registry per customer holding both facts.

[[SNIP V6]]

<a id="v7"></a>
#### V7. No double booking of a room-night (BKG-01), collection with Abstract: an owner register

Keyed by room-night, the claim carries the reservation id as a parameter and the lowest id holds
the night. Abstraction checks the id at 12 representative values for ids up to 2⁴⁰, and the
collection covers every room-night. No claim needs deduplication. One registry holding the
candidates instead does not fit ([T3](#t3)).

[[SNIP V7]]

<a id="v8"></a>
#### V8. Free-cancellation window (BKG-06), Abstract: the cancellation carries its time

Two facts whose arrival order decides the fee are refused. The cancellation carrying its time,
compared with a deadline fixed at creation, verifies over every minute until the year 6000.

[[SNIP V8]]

<a id="v9"></a>
#### V9. Revocation beats earlier grants (ACL-02); latest address wins (MSV-11), Abstract

Versions and times on events, each register keeping the latest; a two-parameter update with a
tie-break on the version.

[[SNIP V9]]

<a id="v10"></a>
#### V10. Price shown at checkout is the price charged (MSV-09), Abstract: a snapshot

The checkout carries the price it showed. The report asks for the checkouts of one order to be
causally ordered (there is one per order), and nothing else.

[[SNIP V10]]

### Verified only at toy scale

<a id="t1"></a>
#### T1. API quota of 1,000,000 a month (ACL-04): one bit over

[[SNIP T1]]

<a id="t2"></a>
#### T2. Wallet balance never negative (PAY-01): arithmetic over cents

[[SNIP T2]]

<a id="t3"></a>
#### T3. No double booking as one registry (BKG-01): cross-item, and past 64 bits

[[SNIP T3]]

<a id="t4"></a>
#### T4. Warehouse pick capacity across SKUs (INV-03): an aggregate

[[SNIP T4]]

<a id="t5"></a>
#### T5. Payouts frozen while a dispute is open (PAY-06): a net counter fails at its bound

[[SNIP T5]]

<a id="t6"></a>
#### T6. Price equals the live catalog price (MSV-09): a closure is not a way out

[[SNIP T6]]

### Not expressible

<a id="n1"></a>
#### N1. Never tell two guests the last room is theirs (BKG-11): needs coordination

[[SNIP N1]]

<a id="n2"></a>
#### N2. Exactly one OrderPlaced message per order (MSV-05): a transport property

[[SNIP N2]]

<a id="n3"></a>
#### N3. Every hold resolved within 15 minutes (BKG-13): a deadline, not a state

The same registry verifies BKG-12 (conversion wins over a late expiry) at scale.

[[SNIP N3]]

## The catalog

Each row: the invariant; how it is expressed today, with the encoding sketched; whether it
verifies at production scale and by which path; the blocking reason otherwise; and the extensions
that would move it ([Ranked extensions](#ranked-extensions)). IDs link to the program that
validates the row, where there is one.

{{CATALOG}}
