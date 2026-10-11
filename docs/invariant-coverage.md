# Invariant coverage

This page measures how much of a real system gsm covers. It catalogs 116 business invariants
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

| Measure | Count | Share |
|---|---:|---:|
| Invariants catalogued | 116 | |
| Expressible in gsm today (combinator, closure or encoding) | 110 | 95% |
| ... directly with combinators | 39 | 34% |
| ... with a closure | 4 | 3% |
| ... through an encoding | 67 | 58% |
| Not expressible | 6 | 5% |
| **Verifiable at production scale today** | **64** | **55%** |
| Verifiable only at toy scale | 45 | 39% |
| Not verifiable at any scale | 7 | 6% |
| Verifiable at scale, excluding lifecycle rules and delivery or deadline guarantees | 49 of 95 | 52% |

- **gsm can state almost every invariant, at some size.** 95% are expressible: with
  combinators, with a closure, or through an encoding (an owner or last-writer-wins register, one
  registry holding the items a rule relates, facts recorded as flags). The 6 that are not ask for
  something outside convergence by compensation: a deadline, an answer that must never be revised,
  or exactly-once delivery by the transport. One more (a two-way sync between services) is
  expressible only with coordination that gsm assumes rather than checks.
- **It verifies 55% at production scale.** Each of these is a rule about one entity at a
  time (an order, a payment, a hold, a request, a room-night), checked through a collection or by
  reusing one machine per entity, or a projection across services checked as a federation. Every
  lifecycle rule in the catalog is in this group. Events with parameters added 17: registers,
  owners, deadlines and snapshots whose values arrive on events and are only compared and copied,
  checked by abstraction over values as wide as the domain needs. Without lifecycle rules and
  delivery or deadline guarantees, 52% (49 of 95) of the harder kinds
  verify at scale.
- **No balance or amount rule verifies at scale**, cross-item rules only where a key turns them
  into one owner register (2 of 11), and of the totals and counts only those that fit two small
  counters per entity (4 of 16). Arithmetic over wide ranges, aggregates and cross-item
  constraints block most of the 45 invariants (39%) that gsm checks only at toy scale.
  Parameters can carry an amount, but abstraction refuses arithmetic on it, and expanding every
  value of an amount is a toy.
- **The next levers are the linear route and an aggregate reduction.** Each moves
  13 invariants alone; together, LIN + AGG (26); with cross-item relational constraints,
  36, which would take gsm from 55% to 86%. All the listed extensions
  together reach 93%; the remaining 8 need coordination, a liveness property, or
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

| Path | Invariants |
|---|---:|
| collection | 35 |
| Abstract | 18 |
| federation | 6 |
| Build | 4 |
| CheckMigration | 1 |

Collections and per-entity machines carry the result. Abstraction carries 18 rows: a credit-limit
hold over amounts fixed at creation, and the 17 rows whose values arrive on events as parameters
(owner and last-writer-wins registers, deadlines, versions and snapshots, per entity or per key).
Per-component checking carries none: it was never the path for a
row, because every per-entity model that verifies fits `Build` whole, and
every model too large for it is blocked by arithmetic, aggregation or the 64-bit word first.

### By kind

| Kind | Invariants | Yes at scale | Toy only | No |
|---|---:|---:|---:|---:|
| Lifecycle and status rules | 15 | 15 | 0 | 0 |
| Ordering ("only moves forward") | 5 | 4 | 1 | 0 |
| Referential and cross-service | 12 | 9 | 2 | 1 |
| Uniqueness | 11 | 9 | 2 | 0 |
| Temporal (expiry, windows, "never twice", precedence) | 25 | 21 | 4 | 0 |
| Totals, counts and aggregates | 16 | 4 | 12 | 0 |
| Balances and amounts with arithmetic | 15 | 0 | 15 | 0 |
| Cross-item (double booking, overlap) | 11 | 2 | 9 | 0 |
| Delivery, deadline and no-revision guarantees | 6 | 0 | 0 | 6 |

### By blocking reason

| Blocking reason | Invariants | Share of the blocked |
|---|---:|---:|
| arithmetic over wide ranges | 17 | 33% |
| history or time | 4 | 8% |
| cross-item constraint | 9 | 17% |
| aggregate across items | 10 | 19% |
| unbounded or relational data | 4 | 8% |
| closure prevents reductions | 0 | 0% |
| other (needs coordination 4, liveness 2, transport 1, reductions do not combine 1) | 8 | 15% |
| **Total blocked** | **52** | |

"Closure prevents reductions" is never the first blocker. Closures appear in rows that verify
(collection templates are enumerated, so closures are fine there) and in rows that need
multiplication or rounding, which are blocked by arithmetic first.

### By domain

| Domain | Invariants | Expressible | Yes at scale | Toy only | No | Main blocker |
|---|---:|---:|---:|---:|---:|---|
| Inventory and warehouse | 11 | 11 | 5 (45%) | 6 | 0 | arithmetic over wide ranges (3), aggregate across items (2) |
| Orders and fulfillment | 12 | 11 | 8 (67%) | 3 | 1 | arithmetic over wide ranges (2), aggregate across items (1) |
| Payments, wallets and ledgers | 13 | 12 | 5 (38%) | 7 | 1 | arithmetic over wide ranges (5), aggregate across items (1) |
| Bookings and reservations | 14 | 12 | 10 (71%) | 2 | 2 | cross-item constraint (2), other (2) |
| Loyalty points | 10 | 10 | 5 (50%) | 5 | 0 | history or time (2), arithmetic over wide ranges (1) |
| Subscriptions and billing | 10 | 10 | 5 (50%) | 5 | 0 | arithmetic over wide ranges (4), aggregate across items (1) |
| Approvals and workflows | 10 | 10 | 8 (80%) | 2 | 0 | aggregate across items (1), history or time (1) |
| Access control and quotas | 11 | 11 | 4 (36%) | 7 | 0 | unbounded or relational data (2), arithmetic over wide ranges (2) |
| Scheduling and capacity | 10 | 10 | 3 (30%) | 7 | 0 | cross-item constraint (5), aggregate across items (1) |
| Multi-service consistency | 15 | 13 | 11 (73%) | 1 | 3 | other (4) |

Approvals, orders and multi-service consistency score highest because their rules are mostly about
one entity's lifecycle. Access control, scheduling and inventory score lowest: quotas, rolling
windows, overlapping intervals and quantities on hand are arithmetic, time and cross-item rules.

## Ranked extensions

Each candidate, with the number of catalogued invariants it would move to "verified at production
scale", now that event parameters are implemented. **Alone** counts rows one of whose minimal sets
is that extension by itself (with parameters, which every set may now use); **on some route**
counts rows where it appears in some minimal set; **greedy gain** is what it adds when extensions are added
in the order of the table, each time picking the one that moves the most.

| Rank | Extension | Alone | On some route | Greedy gain | Theory difficulty |
|---:|---|---:|---:|---:|---|
| 1 | General linear arithmetic (`LIN`) | 13 | 15 | +13 | Medium (theorem exists; solver and saturation open) |
| 2 | Aggregate reduction (counter abstraction) (`AGG`) | 13 | 19 | +13 | High (new theorem) |
| 3 | Cross-item relational constraints (`XREL`) | 5 | 11 | +10 | High (new theorem) |
| 4 | Set and uniqueness constraints (`SET`) | 2 | 3 | +3 | Medium to high |
| 5 | Time and expiry encodings (`TIME`) | 1 | 3 | +3 | Medium (PARAM plus DIFF) |
| 6 | Difference-constraint arithmetic (`DIFF`) | 5 | 6 | +1 | Medium |
| 7 | Combining reductions (`COMB`) | 1 | 1 | +1 | Low to medium |
| 8 | A larger enumeration limit (`BITS`) | 2 | 2 | +0 | None (engineering) |
| 9 | History encoding helpers (`HIST`) | 0 | 0 | +0 | None (sugar) |

Cumulative, in the greedy order:

| Step | Add | Invariants moved by this step | Cumulative yes at scale |
|---:|---|---:|---:|
| 1 | LIN | +13 | 77 (66%) |
| 2 | AGG | +13 | 90 (78%) |
| 3 | XREL | +10 | 100 (86%) |
| 4 | SET | +3 | 103 (89%) |
| 5 | TIME | +3 | 106 (91%) |
| 6 | DIFF | +1 | 107 (92%) |
| 7 | COMB | +1 | 108 (93%) |

The best pair is LIN + AGG (26); the best triple, LIN + AGG + XREL, moves 36. The
8 rows no listed extension moves are ORD-11, PAY-12, BKG-11, BKG-13, SUB-03, MSV-05, MSV-06, MSV-12: coordination, liveness, the transport,
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
13 rows alone: wallets, captures and refunds, ledgers, seat counts, spend limits, invoices
with a tax rate. On some route for 15 rows.

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
item. Alone it moves 13 rows (INV-03, INV-04, ORD-06, PAY-06, PAY-07, LOY-05, SUB-09, SUB-10, APR-07, ACL-04, ACL-06, ACL-10, SCH-05); on some route for 19, among them
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
items' times arriving as parameters, it moves 5 rows alone (INV-08, ACL-01, ACL-03, SCH-01, SCH-10), and is on
some route for 11 rows; after `LIN` and `AGG` it adds 10.

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
register, which event parameters now cover ([V7](#v7)). On some route for 3 rows.

- **Theory difficulty: medium to high.** Equality-only reasoning has small cutoffs, but the rows
  here also need release and re-assignment.
- **Precedent:** data independence (Wolper, 1986; Lazić and Nowak, 2000).

### 5. Time and expiry encodings (`TIME`)

Timestamps as values, with deadlines and windows (t ≤ t₀ + w), packaged: in effect `PARAM` plus
`DIFF` over a time domain. Alone it moves 1 row (LOY-04). Event parameters moved
the rows whose rules only compare times; points that expire 24 months after the last activity
(LOY-04) need the window arithmetic, so `TIME` is that row's only single extension. With
`AGG` it also covers rolling rate limits (ACL-05).

- **Theory difficulty: medium**, following `PARAM` and `DIFF`.
- **Precedent:** timed automata regions and zones (Alur and Dill, 1994; difference-bound matrices,
  Dill, 1989).

### 6. Difference-constraint arithmetic (`DIFF`)

Counters that move by constants, with guards x - y ≤ c, over unbounded integers. Alone it moves
5 rows (PAY-06, LOY-04, SUB-10, ACL-04, ACL-10): counters that need no amounts on events, only more range than
20 bits allow, or a net count (open disputes, concurrent sessions) whose two monotone counters
must cover lifetime totals.

- **Theory difficulty: medium.** A fragment between the comparison route and `lin_exact`. Its
  conditions are difference-logic formulas, decidable by negative-cycle detection without an SMT
  solver, which fits gsm's no-solver rule.
- **Precedent:** difference-bound matrices (Dill, 1989; Bengtsson and Yi, 2004).

### 7. A larger enumeration limit (`BITS`)

Raise `Build`'s 20-bit limit. Alone it moves 2 rows (SUB-10, ACL-04), the ones a bit
or two over the limit. No theory; the cost doubles with each bit (step tables of 2ⁿ entries per
event), so the gain is a few bits, or symbolic enumeration (Burch et al., 1990; McMillan, 1993).

### 8. Combining reductions (`COMB`)

Abstraction with federations, with per-component checking, and collections with per-component
checking or migration. Alone it moves 1 row (MSV-14: amounts compared across
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
domain sizes a rule needs. The 8 unmoved rows need coordination (an answer never revised,
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

<!-- gocheck: run -->
```go
// ORD-03: an order's status only moves forward. Each milestone is a fact (a Bool that only
// rises); the status is derived as the furthest milestone, so a late "packed" never moves a
// delivered order back. One order's rules; the collection covers every OrderID.
order := gsm.NewRegistry("order")
paid, packed := order.Bool("paid"), order.Bool("packed")
shipped, delivered := order.Bool("shipped"), order.Bool("delivered")
status := order.Enum("status", "placed", "paid", "packed", "shipped", "delivered")

furthest := func(s gsm.State) string {
    switch {
    case s.GetBool(delivered):
        return "delivered"
    case s.GetBool(shipped):
        return "shipped"
    case s.GetBool(packed):
        return "packed"
    case s.GetBool(paid):
        return "paid"
    }
    return "placed"
}
order.Invariant("status_is_furthest_milestone").
    Watches(paid, packed, shipped, delivered, status).
    Holds(func(s gsm.State) bool { return s.Get(status) == furthest(s) }).
    Repair(func(s gsm.State) gsm.State { return s.Set(status, furthest(s)) }).
    Add()
for name, v := range map[string]gsm.Var{"pay": paid, "pack": packed, "ship": shipped, "deliver": delivered} {
    v := v
    order.Event(name).Writes(v).Apply(func(s gsm.State) gsm.State { return s.SetBool(v, true) }).Add()
}

type OrderID string
orders, report, err := gsm.NewCollection[OrderID]("OrderID", order).Build()
if err != nil {
    panic(fmt.Sprintf("convergence not guaranteed: %v\n%s", err, report))
}
s := orders.NewState()
orders.Apply(s, "A-1001", "deliver") // the carrier's scan arrives first
late := orders.Apply(s, "A-1001", "pack") // the warehouse's message arrives late
if late.Get(status) != "delivered" || len(report.NotIdempotent) != 0 {
    panic("expected delivered, and every event safe to redeliver")
}
fmt.Println(report.Symmetry, "|", late.Get(status))
```

<a id="v2"></a>
#### V2. Stop-sell per room type and night (BKG-02), collection at 2²⁰ states

Counters that only rise, per (room type, night), up to 511 bookings each, with the stop-sell and
overbooked flags derived: exactly `Build`'s limit, checked in well under a second. The report
asks for exactly-once delivery of `book` and `cancel`. This verifies the stop-sell flags at
production scale; which booking is walked when the type is oversold (BKG-03) does not verify.

<!-- gocheck: run -->
```go
// BKG-02: stop-sell. A room type closes for a night once net bookings reach rooms plus the
// overbooking allowance (200 rooms + 5%), and reopens when cancellations bring it below.
// Bookings and cancellations are separate counters that only rise, so the two events
// commute; one counter with Inc and Dec would not (see T6). 2^20 states: the whole check.
night := gsm.NewRegistry("roomtype_night")
booked := night.Int("booked", 0, 511)
cancelled := night.Int("cancelled", 0, 511)
stopSell := night.Bool("stop_sell")
overbooked := night.Bool("overbooked")
const sellable, rooms = 210, 200

net := func(s gsm.State) int { return s.GetInt(booked) - s.GetInt(cancelled) }
night.Invariant("flags_match_counts").
    Watches(booked, cancelled, stopSell, overbooked).
    Holds(func(s gsm.State) bool {
        return s.GetBool(stopSell) == (net(s) >= sellable) && s.GetBool(overbooked) == (net(s) > rooms)
    }).
    Repair(func(s gsm.State) gsm.State {
        return s.SetBool(stopSell, net(s) >= sellable).SetBool(overbooked, net(s) > rooms)
    }).
    Add()
night.Event("book").Writes(booked).
    Apply(func(s gsm.State) gsm.State { return s.SetInt(booked, s.GetInt(booked)+1) }).Add()
night.Event("cancel").Writes(cancelled).
    Apply(func(s gsm.State) gsm.State { return s.SetInt(cancelled, s.GetInt(cancelled)+1) }).Add()

type RoomTypeNight struct {
    RoomType string
    Night    string
}
nights, report, err := gsm.NewCollection[RoomTypeNight]("RoomTypeNight", night).Build()
if err != nil {
    panic(fmt.Sprintf("convergence not guaranteed: %v\n%s", err, report))
}
key := RoomTypeNight{"KING", "2026-12-31"}
st := nights.NewState()
var last gsm.State
for i := 0; i < 210; i++ {
    last = nights.Apply(st, key, "book")
}
if !last.GetBool(stopSell) || !last.GetBool(overbooked) {
    panic("expected stop-sell and overbooked at 210 net bookings")
}
last = nights.Apply(st, key, "cancel")
fmt.Println(report.StateCount, last.GetBool(stopSell), report.NotIdempotent) // 1048576 false [book cancel]
```

<a id="v3"></a>
#### V3. Approvals: thresholds, rejection, no self-approval (APR-01, APR-02, APR-03), Build

Eight approvers, a threshold and a requester fixed at creation: 27,648 states, every pair of nine
events checked. One machine serves every request.

<!-- gocheck: run -->
```go
// APR-01, APR-02, APR-03: a purchase order over $10,000 needs two approvals, otherwise one;
// a rejection is final; no one approves their own request. The approver pool is a cost
// center's 8 approvers (one Bool each, so "distinct" is free). The amount and the requester
// are fixed when the request is created, so the threshold is stored as a Bool and the
// requester as an index into the pool (8 = not in the pool).
req := gsm.NewRegistry("purchase_request")
const pool = 8
var approved [pool]gsm.Var
for i := range approved {
    approved[i] = req.Bool(fmt.Sprintf("approved_by_%d", i))
}
requester := req.Int("requester", 0, pool)
large := req.Bool("over_10k")
rejected := req.Bool("rejected")
outcome := req.Enum("outcome", "pending", "approved", "rejected")

decide := func(s gsm.State) string {
    if s.GetBool(rejected) {
        return "rejected"
    }
    n := 0
    for i, v := range approved {
        if s.GetBool(v) && i != s.GetInt(requester) {
            n++
        }
    }
    if need := map[bool]int{true: 2, false: 1}[s.GetBool(large)]; n >= need {
        return "approved"
    }
    return "pending"
}
watched := append(approved[:], requester, large, rejected, outcome)
req.Invariant("outcome_follows_approvals").Watches(watched...).
    Holds(func(s gsm.State) bool { return s.Get(outcome) == decide(s) }).
    Repair(func(s gsm.State) gsm.State { return s.Set(outcome, decide(s)) }).Add()
for i, v := range approved {
    v := v
    req.Event(fmt.Sprintf("approve_%d", i)).Writes(v).
        Apply(func(s gsm.State) gsm.State { return s.SetBool(v, true) }).Add()
}
req.Event("reject").Writes(rejected).
    Apply(func(s gsm.State) gsm.State { return s.SetBool(rejected, true) }).Add()

m, report, err := req.Build() // 27,648 states, 9 events, every pair
if err != nil {
    panic(fmt.Sprintf("convergence not guaranteed: %v\n%s", err, report))
}
// Request PO-77: $25,000, raised by approver 3. Creation-time facts are set once.
po := m.NewState().SetInt(requester, 3).SetBool(large, true)
po = m.Apply(m.Apply(po, "approve_3"), "approve_5") // a self-approval does not count
if po.Get(outcome) != "pending" {
    panic("a self-approval counted")
}
po = m.Apply(po, "approve_1")
fmt.Println(report.StateCount, po.Get(outcome)) // 27648 approved
```

<a id="v4"></a>
#### V4. One capture per idempotency key, refunds held until capture (PAY-04, PAY-05, PAY-10), collection

Combinator rules, so both extracted oracles certify the template. No event needs deduplication.

<!-- gocheck: run -->
```go
// PAY-04, PAY-05, PAY-10: one charge per idempotency key; a capture counts once however often
// it is redelivered; a refund that arrives before the capture is held, then applied.
// Combinator rules, so both extracted oracles certify the template.
p := gsm.NewRegistry("payment")
captured := p.Bool("captured")
refundAsked := p.Bool("refund_requested")
refunded := p.Bool("refunded")

p.Rule("refund_needs_capture").Require(gsm.Or(gsm.Is(refunded, 0), gsm.Is(captured, 1))).
    RepairWith(gsm.Lower(refunded)).Add()
p.Rule("apply_held_refund").
    Require(gsm.Or(gsm.Is(refundAsked, 0), gsm.Is(captured, 0), gsm.Is(refunded, 1))).
    RepairWith(gsm.Raise(refunded)).Add()
p.On("capture").Does(gsm.Raise(captured)).Add()
p.On("refund").Does(gsm.Raise(refundAsked)).Add()

type IdempotencyKey string
payments, report, err := gsm.NewCollection[IdempotencyKey]("IdempotencyKey", p).Build()
if err != nil {
    panic(fmt.Sprintf("convergence not guaranteed: %v\n%s", err, report))
}
st := payments.NewState()
held := payments.Apply(st, "idem-9f2", "refund") // the refund arrives first: held
if held.GetBool(refunded) {
    panic("refunded before capture")
}
payments.Apply(st, "idem-9f2", "capture")
done := payments.Apply(st, "idem-9f2", "capture") // a redelivered capture changes nothing
if !done.GetBool(refunded) || len(report.NotIdempotent) != 0 {
    panic("expected the held refund applied and no event needing deduplication")
}
fmt.Println(report.Assurance, "|", report.Symmetry)
```

<a id="v5"></a>
#### V5. Credit-limit hold over amounts in cents (ORD-12), Abstract

Totals and limits up to 10⁹ cents, fixed at creation and only compared: abstraction checks 12
representative values per variable. Values that arrive on events instead are V7 to V10; a balance
that moves is refused ([T2](#t2)).

<!-- gocheck: run -->
```go
// ORD-12: an order whose total exceeds the customer's credit limit is held until a manager
// approves an override, and a frozen credit line holds every order. Totals and limits are
// amounts in cents up to $10M, fixed when the order is created; the rules only compare
// them, so Abstract checks 12 representative values per variable instead of 10^9.
o := gsm.NewRegistry("order_credit")
total := o.Int("total_cents", 0, 1_000_000_000)
limit := o.Int("limit_cents", 0, 1_000_000_000)
override := o.Int("override", 0, 1) // Abstract takes Int only: flags are 0/1 Ints
frozen := o.Int("frozen", 0, 1)
held := o.Int("held", 0, 1)

shouldHold := gsm.Or(gsm.Is(frozen, 1), gsm.And(gsm.AboveVar(total, limit), gsm.Is(override, 0)))
o.Rule("hold").Require(gsm.Or(gsm.Is(held, 1), gsm.Not(shouldHold))).RepairWith(gsm.SetTo(held, 1)).Add()
o.Rule("release").Require(gsm.Or(gsm.Is(held, 0), shouldHold)).RepairWith(gsm.SetTo(held, 0)).Add()
o.On("approve_override").Does(gsm.SetTo(override, 1)).Add()
o.On("freeze_credit_line").Does(gsm.SetTo(frozen, 1)).Add()

m, report, err := o.Abstract(0, 1).Build()
if err != nil {
    panic(fmt.Sprintf("convergence not guaranteed: %v\n%s", err, report))
}
s := m.NewState().SetInt(total, 2_500_000_00).SetInt(limit, 2_000_000_00) // $2.5M against $2M
s = m.Apply(s, "approve_override")
fmt.Println(report.Abstraction)
fmt.Println(s.GetInt(held), m.Apply(s, "freeze_credit_line").GetInt(held)) // 0 1
```

<a id="v6"></a>
#### V6. GDPR erasure reaches every service (MSV-07), federation; a reacting target is refused (MSV-15)

As a projection the erasure verifies. A target invariant that drops a queued campaign once erased
is refused by M1, so MSV-15 verifies only as one registry per customer holding both facts.

<!-- gocheck: run -->
```go
// MSV-07: a GDPR erasure recorded in the identity service reaches CRM and marketing, whatever
// order the services' own events arrive in. Identity is the authority over each target's
// erased flag; the targets' own facts (a synced profile, a queued campaign) stay local.
identity := gsm.NewRegistry("identity")
erased := identity.Bool("erased")
identity.Event("erasure_requested").Writes(erased).
    Apply(func(s gsm.State) gsm.State { return s.SetBool(erased, true) }).Add()

target := func(name, fact string) (*gsm.Registry, gsm.Var, gsm.Var) {
    r := gsm.NewRegistry(name)
    flag, local := r.Bool("erased"), r.Bool(fact)
    r.Event(fact).Writes(local).Apply(func(s gsm.State) gsm.State { return s.SetBool(local, true) }).Add()
    return r, flag, local
}
copyErased := func(flag gsm.Var) func(src, dst gsm.State) gsm.State {
    return func(src, dst gsm.State) gsm.State { return dst.SetBool(flag, src.GetBool(erased)) }
}
crm, crmErased, _ := target("crm", "profile_synced")
mkt, mktErased, queued := target("marketing", "campaign_queued")
fed := gsm.NewFederation("gdpr_erasure")
fed.Morphism(identity, crm).Shared(crmErased).Map(copyErased(crmErased)).Add()
fed.Morphism(identity, mkt).Shared(mktErased).Map(copyErased(mktErased)).Add()
fm, report, err := fed.Build()
if err != nil {
    panic(fmt.Sprintf("federation not guaranteed to converge: %v\n%s", err, report))
}
fs := fm.Apply(fm.NewState(), mkt, "campaign_queued")
fs = fm.Apply(fs, identity, "erasure_requested")
fmt.Println(fm.Of(fs, crm).GetBool(crmErased), fm.Of(fs, mkt).GetBool(mktErased)) // true true

// The limit: a target invariant that repairs its own state in response to the authority's
// fact ("drop the queued campaign once erased") is refused by M1, which requires the
// overwrite alone to keep the target valid. Such a rule goes in one registry that holds both
// facts, or is evaluated when the campaign is sent (queued and not erased).
mkt2, mkt2Erased, queued2 := target("marketing_reacting", "campaign_queued")
mkt2.Invariant("no_campaign_once_erased").Watches(mkt2Erased, queued2).
    Holds(func(s gsm.State) bool { return !(s.GetBool(mkt2Erased) && s.GetBool(queued2)) }).
    Repair(func(s gsm.State) gsm.State { return s.SetBool(queued2, false) }).Add()
fed2 := gsm.NewFederation("gdpr_erasure_reacting")
fed2.Morphism(identity, mkt2).Shared(mkt2Erased).Map(copyErased(mkt2Erased)).Add()
if _, _, err := fed2.Build(); err == nil {
    panic("expected M1 to refuse the reacting target")
} else {
    fmt.Println(err)
}
_ = queued
```

<a id="v7"></a>
#### V7. No double booking of a room-night (BKG-01), collection with Abstract: an owner register

Keyed by room-night, the claim carries the reservation id as a parameter and the lowest id holds
the night. Abstraction checks the id at 12 representative values for ids up to 2⁴⁰, and the
collection covers every room-night. No claim needs deduplication. One registry holding the
candidates instead does not fit ([T3](#t3)).

<!-- gocheck: run -->
```go
// BKG-01: no room is assigned to two reservations on the same night. Keyed by room-night, the
// rule is an owner register: each reservation's claim carries its id, and the lowest id holds
// the room-night (the others are walked). The id is a parameter, compared and copied, so
// abstraction checks it at a few representative values for ids up to 2^40, and the collection
// covers every room-night of every hotel. INV-05 (bin occupant), ORD-09 (unit claimant),
// ACL-08 (email owner), SCH-06 (meeting slot) and MSV-08 (external id) are the same register.
night := gsm.NewRegistry("room_night_owner")
holder := night.Int("holder", 0, 1<<40) // 0: free
night.On("claim").Param("reservation", 1, 1<<40).
    OnlyIf(gsm.Or(gsm.Is(holder, 0), gsm.Lt(gsm.Arg("reservation"), gsm.V(holder)))).
    Does(gsm.SetToArg(holder, "reservation")).Add()
night.Abstract(0)

type RoomNight struct {
    Room  int
    Night string
}
nights, report, err := gsm.NewCollection[RoomNight]("RoomNight", night).Build()
if err != nil {
    panic(fmt.Sprintf("%v\n%s", err, report))
}
if len(report.NotIdempotent) != 0 {
    panic("a claim is safe to redeliver")
}
s := nights.NewState()
key := RoomNight{Room: 412, Night: "2026-12-31"}
nights.ApplyWith(s, key, "claim", 88_100_217)
nights.ApplyWith(s, key, "claim", 88_100_009) // arrives later, but the lower id holds the night
fmt.Println(s.Item(key).GetInt(holder))     // 88100009
fmt.Println(report.Abstraction)
```

<a id="v8"></a>
#### V8. Free-cancellation window (BKG-06), Abstract: the cancellation carries its time

Two facts whose arrival order decides the fee are refused. The cancellation carrying its time,
compared with a deadline fixed at creation, verifies over every minute until the year 6000.

<!-- gocheck: run -->
```go
// BKG-06: a cancellation inside the free window costs nothing; after it, one night's fee.
// As two facts whose arrival order decides the fee, the rule is order-dependent, and Build
// refuses it.
naive := gsm.NewRegistry("cancellation_naive")
deadlinePassed, cancelled, fee := naive.Bool("deadline_passed"), naive.Bool("cancelled"), naive.Bool("fee")
naive.Event("deadline").Writes(deadlinePassed).
    Apply(func(s gsm.State) gsm.State { return s.SetBool(deadlinePassed, true) }).Add()
naive.Event("cancel").Writes(cancelled, fee).
    Apply(func(s gsm.State) gsm.State {
        return s.SetBool(cancelled, true).SetBool(fee, s.GetBool(deadlinePassed)) // reads the order
    }).Add()
_, rep, err := naive.Build()
if err == nil || rep.CCFailure == nil {
    panic("expected the order-dependent rule to be refused")
}
fmt.Println(err)

// Faithful: the cancellation carries its time (minutes since 1970), and the deadline is fixed
// when the booking is created. The earliest cancellation counts; the fee is derived from it.
// Times compare only, so abstraction checks them at a few representative values. PAY-11
// (capture before expiry), BKG-08 (pickup before the cutoff) and APR-08 (approval inside the
// delegation window) compare an event's time with a deadline the same way.
r := gsm.NewRegistry("cancellation")
deadline := r.Int("deadline", 0, 1<<31-1)
at := r.Int("cancelled_at", 0, 1<<31-1) // 0: not cancelled
charged := r.Int("fee", 0, 1)
r.On("cancel").Param("at", 1, 1<<31-1).
    OnlyIf(gsm.Or(gsm.Is(at, 0), gsm.Lt(gsm.Arg("at"), gsm.V(at)))).
    Does(gsm.SetToArg(at, "at")).Add()
late := gsm.And(gsm.IsNot(at, 0), gsm.AboveVar(at, deadline))
r.Rule("fee if late").Require(gsm.Or(gsm.Not(late), gsm.Is(charged, 1))).RepairWith(gsm.SetTo(charged, 1)).Add()
r.Rule("no fee otherwise").Require(gsm.Or(late, gsm.Is(charged, 0))).RepairWith(gsm.SetTo(charged, 0)).Add()
m, rep2, err := r.Abstract(0, 1).Build()
if err != nil {
    panic(fmt.Sprintf("%v\n%s", err, rep2))
}
s := m.NewState().SetInt(deadline, 29_800_000)
fmt.Println(m.ApplyWith(s, "cancel", 29_799_000).GetInt(charged), m.ApplyWith(s, "cancel", 29_801_000).GetInt(charged)) // 0 1
```

<a id="v9"></a>
#### V9. Revocation beats earlier grants (ACL-02); latest address wins (MSV-11), Abstract

Versions and times on events, each register keeping the latest; a two-parameter update with a
tie-break on the version.

<!-- gocheck: run -->
```go
// ACL-02: a revocation beats any grant issued before it, but a later re-grant restores
// access. Grants and revocations carry versions; each register keeps the latest, and access
// is derived from the two. APR-05 (edits after the signature's version are rejected) is the
// same comparison.
acl := gsm.NewRegistry("access")
granted := acl.Int("granted", 0, 1<<30)
revoked := acl.Int("revoked", 0, 1<<30)
access := acl.Int("access", 0, 1)
acl.On("grant").Param("version", 1, 1<<30).
    OnlyIf(gsm.Lt(gsm.V(granted), gsm.Arg("version"))).Does(gsm.SetToArg(granted, "version")).Add()
acl.On("revoke").Param("version", 1, 1<<30).
    OnlyIf(gsm.Lt(gsm.V(revoked), gsm.Arg("version"))).Does(gsm.SetToArg(revoked, "version")).Add()
acl.Rule("on").Require(gsm.Or(gsm.AtMostVar(granted, revoked), gsm.Is(access, 1))).RepairWith(gsm.SetTo(access, 1)).Add()
acl.Rule("off").Require(gsm.Or(gsm.AboveVar(granted, revoked), gsm.Is(access, 0))).RepairWith(gsm.SetTo(access, 0)).Add()
m, report, err := acl.Abstract(0, 1).Build()
if err != nil {
    panic(fmt.Sprintf("%v\n%s", err, report))
}
s := m.ApplyWith(m.ApplyWith(m.NewState(), "grant", 10), "revoke", 20) // revoked
t := m.ApplyWith(s, "grant", 30)                                        // re-granted
fmt.Println(s.GetInt(access), t.GetInt(access), m.ApplyWith(t, "grant", 15).GetInt(access)) // 0 1 1

// MSV-11: the latest address update wins in every service. The update carries the address
// version and its time; a later time wins, and a tie goes to the higher version. INV-11
// (latest scan wins) and SUB-06 (latest plan request wins) are the same register.
addr := gsm.NewRegistry("address")
version := addr.Int("version", 0, 1<<20)
stamp := addr.Int("stamp", 0, 1<<40)
addr.On("update").Param("v", 0, 1<<20).Param("at", 1, 1<<40).
    OnlyIf(gsm.Or(gsm.Lt(gsm.V(stamp), gsm.Arg("at")),
        gsm.And(gsm.Eq(gsm.V(stamp), gsm.Arg("at")), gsm.Lt(gsm.V(version), gsm.Arg("v"))))).
    Does(gsm.Do(gsm.Set(version, gsm.Arg("v")), gsm.Set(stamp, gsm.Arg("at")))).Add()
am, report, err := addr.Abstract().Build()
if err != nil {
    panic(fmt.Sprintf("%v\n%s", err, report))
}
a := am.ApplyWith(am.ApplyWith(am.NewState(), "update", 4, 1_700_000_000), "update", 9, 1_700_000_050)
b := am.ApplyWith(am.ApplyWith(am.NewState(), "update", 9, 1_700_000_050), "update", 4, 1_700_000_000)
fmt.Println(a.GetInt(version), b.GetInt(version)) // 9 9
```

<a id="v10"></a>
#### V10. Price shown at checkout is the price charged (MSV-09), Abstract: a snapshot

The checkout carries the price it showed. The report asks for the checkouts of one order to be
causally ordered (there is one per order), and nothing else.

<!-- gocheck: run -->
```go
// MSV-09: the price charged equals the price shown at checkout. The checkout carries the price
// it showed, and the charge is derived from it, so a later catalog change cannot reach the
// order. BKG-07 (the rate locked at booking) is the same snapshot. One checkout per order:
// two checkouts of one order with different prices would not commute, so the checkout is
// declared independent of the payment only, and the report asks for the checkouts of one order
// to be causally ordered.
order := gsm.NewRegistry("order_checkout")
price := order.Int("price", 0, 1<<30)     // in cents, as shown at checkout
charged := order.Int("charged", 0, 1<<30) // what the payment captures
paid := order.Int("paid", 0, 1)
order.On("checkout").Param("price", 1, 1<<30).Does(gsm.SetToArg(price, "price")).Add()
order.On("pay").Does(gsm.SetTo(paid, 1)).Add()
order.Rule("charge the shown price").Require(gsm.Or(gsm.Is(paid, 0), gsm.SameAs(charged, price))).
    RepairWith(gsm.Copy(charged, price)).Add()
order.Independent("checkout", "pay")
m, report, err := order.Abstract(0, 1).Build()
if err != nil {
    panic(fmt.Sprintf("%v\n%s", err, report))
}
if len(report.CausalOrderRequired) != 1 {
    panic("expected the checkouts of one order to need causal order")
}
a := m.ApplyWith(m.Apply(m.NewState(), "pay"), "checkout", 12_999)
b := m.Apply(m.ApplyWith(m.NewState(), "checkout", 12_999), "pay")
fmt.Println(a.GetInt(charged), b.GetInt(charged)) // 12999 12999
```

### Verified only at toy scale

<a id="t1"></a>
#### T1. API quota of 1,000,000 a month (ACL-04): one bit over

<!-- gocheck: run -->
```go
// ACL-04: requests per API key per month stay within 1,000,000; requests beyond are throttled.
// The counter alone is 20 bits; the throttled flag makes 21, one past Build's limit. The rule
// ties the two, so per-component checking has nothing to split, and Abstract refuses both the
// Bool and the increment.
mk := func(name string, max int) *gsm.Registry {
    r := gsm.NewRegistry(name)
    used := r.Int("used", 0, max)
    throttled := r.Bool("throttled")
    r.Invariant("throttle_at_quota").Watches(used, throttled).
        Holds(func(s gsm.State) bool { return s.GetBool(throttled) == (s.GetInt(used) >= max) }).
        Repair(func(s gsm.State) gsm.State { return s.SetBool(throttled, s.GetInt(used) >= max) }).Add()
    r.Event("request").Writes(used).
        Apply(func(s gsm.State) gsm.State { return s.SetInt(used, s.GetInt(used)+1) }).Add()
    return r
}
_, _, errBuild := mk("api_quota", 1_000_000).Build()
_, _, errAbs := mk("api_quota", 1_000_000).Abstract(1_000_000).Build()
_, _, errToy := mk("api_quota_toy", 1_000).Build() // the toy model: a quota of 1,000
if errBuild == nil || errAbs == nil || errToy != nil {
    panic("expected the production quota refused and the toy quota verified")
}
fmt.Println(errBuild)
fmt.Println(errAbs)
```

<a id="t2"></a>
#### T2. Wallet balance never negative (PAY-01): arithmetic over cents

<!-- gocheck: run -->
```go
// PAY-01: a wallet balance never goes below zero; withdrawals beyond the available funds stay
// pending until a deposit covers them. Amounts are cents up to $10M (30 bits per counter), and
// each deposit or withdrawal carries its amount as a parameter.
mk := func(name string, max, maxAmount int) *gsm.Registry {
    w := gsm.NewRegistry(name)
    deposited := w.Int("deposited", 0, max) // counters that only rise, so the events commute
    withdrawn := w.Int("withdrawn", 0, max)
    pending := w.Int("withdrawal_pending", 0, 1) // an Int, so Abstract names the arithmetic
    covered := gsm.AtMostVar(withdrawn, deposited)
    w.Rule("pending_when_short").Require(gsm.Or(gsm.Is(pending, 1), covered)).RepairWith(gsm.Raise(pending)).Add()
    w.Rule("clear_when_covered").Require(gsm.Or(gsm.Is(pending, 0), gsm.Not(covered))).RepairWith(gsm.Lower(pending)).Add()
    w.On("deposit").Param("amount", 1, maxAmount).Does(gsm.IncByArg(deposited, "amount")).Add()
    w.On("withdraw").Param("amount", 1, maxAmount).Does(gsm.IncByArg(withdrawn, "amount")).Add()
    return w
}
// At production size the model needs 30 + 30 + 1 = 61 bits, far past Build's 20, and its one
// rule ties the counters, so per-component checking has nothing to split. Abstract refuses the
// arithmetic on the amount, and Build without it checks one event per amount (at most 1024).
_, _, errAbs := mk("wallet_cents", 1_000_000_000, 1_000_000_000).Abstract(0, 1).Build()
_, _, errToy := mk("wallet_toy", 255, 5).Build() // $2.55 of headroom, amounts up to 5 cents: 17 bits
if errAbs == nil || errToy != nil {
    panic("expected the production wallet refused and the toy wallet verified")
}
fmt.Println(errAbs)
```

<a id="t3"></a>
#### T3. No double booking as one registry (BKG-01): cross-item, and past 64 bits

<!-- gocheck: run -->
```go
// BKG-01: no room is assigned to two reservations on the same night. Keyed by room-night with
// the claim carrying the reservation id, it verifies at scale (V7). Without event parameters
// the rule relates two reservations, so it cannot be written through a collection keyed by
// reservation; it needs one registry holding every reservation that could claim the room-night. Toy: one
// room-night, two reservations, the lower-numbered one wins and the other is walked.
r := gsm.NewRegistry("room_night")
wantA, wantB := r.Bool("res_a_requested"), r.Bool("res_b_requested")
holder := r.Enum("holder", "none", "res_a", "res_b")
assign := func(s gsm.State) string {
    switch {
    case s.GetBool(wantA):
        return "res_a"
    case s.GetBool(wantB):
        return "res_b"
    }
    return "none"
}
r.Invariant("one_holder").Watches(wantA, wantB, holder).
    Holds(func(s gsm.State) bool { return s.Get(holder) == assign(s) }).
    Repair(func(s gsm.State) gsm.State { return s.Set(holder, assign(s)) }).Add()
r.Event("request_a").Writes(wantA).Apply(func(s gsm.State) gsm.State { return s.SetBool(wantA, true) }).Add()
r.Event("request_b").Writes(wantB).Apply(func(s gsm.State) gsm.State { return s.SetBool(wantB, true) }).Add()
if _, rep, err := r.Build(); err != nil {
    panic(fmt.Sprintf("toy model should verify: %v\n%s", err, rep))
}

// Production: 300 rooms x 365 nights, and a holder per room-night that can name any of
// ~100,000 reservations a year (17 bits). Even one bit per room-night is past State's 64.
big := gsm.NewRegistry("hotel_year")
for i := 0; i < 65; i++ { // 65 of the 109,500 room-nights already do not fit
    v := big.Bool(fmt.Sprintf("room_night_%d_taken", i))
    big.Event(fmt.Sprintf("take_%d", i)).Writes(v).Apply(func(s gsm.State) gsm.State { return s.SetBool(v, true) }).Add()
}
_, _, err := big.Build()
if err == nil {
    panic("expected a state wider than 64 bits to be refused")
}
fmt.Println(err)
```

<a id="t4"></a>
#### T4. Warehouse pick capacity across SKUs (INV-03): an aggregate

<!-- gocheck: run -->
```go
// INV-03: units reserved across all SKUs in a warehouse stay within its daily pick capacity;
// reservations beyond it are deferred. The rule relates SKUs, so a collection cannot state it
// (and must not: aggregate_diverges). One registry holding three SKUs verifies; at 10,000
// SKUs the registry needs 140,000 bits.
const skus, capacity = 3, 10
w := gsm.NewRegistry("warehouse_day")
var reserved [skus]gsm.Var
for i := range reserved {
    reserved[i] = w.Int(fmt.Sprintf("sku_%d_reserved", i), 0, 7)
}
deferred := w.Bool("deferred")
sum := func(s gsm.State) int {
    n := 0
    for _, v := range reserved {
        n += s.GetInt(v)
    }
    return n
}
w.Invariant("defer_over_capacity").Watches(append(reserved[:], deferred)...).
    Holds(func(s gsm.State) bool { return s.GetBool(deferred) == (sum(s) > capacity) }).
    Repair(func(s gsm.State) gsm.State { return s.SetBool(deferred, sum(s) > capacity) }).Add()
for i, v := range reserved {
    v := v
    w.Event(fmt.Sprintf("reserve_sku_%d", i)).Writes(v).
        Apply(func(s gsm.State) gsm.State { return s.SetInt(v, s.GetInt(v)+1) }).Add()
}
_, report, err := w.Build()
if err != nil {
    panic(fmt.Sprintf("toy model should verify: %v\n%s", err, report))
}
fmt.Println(report.StateCount) // 1024
```

<a id="t5"></a>
#### T5. Payouts frozen while a dispute is open (PAY-06): a net counter fails at its bound

<!-- gocheck: run -->
```go
// PAY-06: payouts are frozen while a merchant has an open dispute. The natural model, one
// net counter that dispute_opened increments and dispute_closed decrements, fails CC at the
// counter's bound for any range: at the bound one order saturates and the other does not.
net := gsm.NewRegistry("disputes_net")
open := net.Int("open", -1000, 1000)
frozen := net.Bool("payouts_frozen")
net.Rule("freeze").Require(gsm.Or(gsm.Is(frozen, 1), gsm.AtMost(open, 0))).RepairWith(gsm.Raise(frozen)).Add()
net.Rule("unfreeze").Require(gsm.Or(gsm.Is(frozen, 0), gsm.Above(open, 0))).RepairWith(gsm.Lower(frozen)).Add()
net.On("dispute_opened").Does(gsm.Inc(open)).Add()
net.On("dispute_closed").Does(gsm.Dec(open)).Add()
_, rep, err := net.Build()
if err == nil || rep.CCFailure == nil {
    panic("expected the net counter to fail CC at its bound")
}
fmt.Println(rep.CCFailure.State) // {open=-1000, payouts_frozen=false}

// Two counters that only rise commute. They must count disputes over the merchant's
// lifetime, and with the flag only 511 of them fit in 20 bits: a toy for a large merchant.
mono := gsm.NewRegistry("disputes")
opened, closed := mono.Int("opened", 0, 511), mono.Int("closed", 0, 511)
fr := mono.Bool("payouts_frozen")
mono.Invariant("frozen_iff_open").Watches(opened, closed, fr).
    Holds(func(s gsm.State) bool { return s.GetBool(fr) == (s.GetInt(opened) > s.GetInt(closed)) }).
    Repair(func(s gsm.State) gsm.State { return s.SetBool(fr, s.GetInt(opened) > s.GetInt(closed)) }).Add()
for name, v := range map[string]gsm.Var{"dispute_opened": opened, "dispute_closed": closed} {
    v := v
    mono.Event(name).Writes(v).Apply(func(s gsm.State) gsm.State { return s.SetInt(v, s.GetInt(v)+1) }).Add()
}
if _, rep, err := mono.Build(); err != nil {
    panic(fmt.Sprintf("monotone counters should verify: %v\n%s", err, rep))
}
```

<a id="t6"></a>
#### T6. Price equals the live catalog price (MSV-09): a closure is not a way out

<!-- gocheck: run -->
```go
// MSV-09: the price charged equals the catalog's current price. A closure that reads the
// catalog from outside its State does not express it: a collection runs the template's
// tables, so the closure ran only while Build verified the template, and the price it read
// then is frozen in. The checkout carrying the price it showed is the faithful model (V10).
catalogPrice := map[string]int{"standard": 3}
line := gsm.NewRegistry("order_line")
priced := line.Int("price", 0, 7)
line.Event("price_line").Writes(priced).
    Apply(func(s gsm.State) gsm.State { return s.SetInt(priced, catalogPrice["standard"]) }).Add()
type LineID string
lines, report, err := gsm.NewCollection[LineID]("LineID", line).Build()
if err != nil {
    panic(fmt.Sprintf("%v\n%s", err, report))
}
catalogPrice["standard"] = 5 // the catalog changes after Build
got := lines.Apply(lines.NewState(), "L-1", "price_line").GetInt(priced)
if got != 3 {
    panic("expected the build-time price")
}
fmt.Println(got) // 3, not 5: the rule saw the catalog once, at build time
```

### Not expressible

<a id="n1"></a>
#### N1. Never tell two guests the last room is theirs (BKG-11): needs coordination

<!-- gocheck: run -->
```go
// BKG-11: never tell two guests that the last room is theirs, not even briefly. gsm converges
// by compensation: each replica applies events as they arrive and repairs, so a replica that
// sees guest B's request first confirms B, and only later, when A's request arrives, walks B.
// The final state is right everywhere; the confirmation B already received is not undone.
// No model fixes this: an answer that must never be revised needs coordination.
r := gsm.NewRegistry("last_room")
wantA, wantB := r.Bool("guest_a_requested"), r.Bool("guest_b_requested")
holder := r.Enum("holder", "none", "guest_a", "guest_b")
assign := func(s gsm.State) string {
    switch {
    case s.GetBool(wantA):
        return "guest_a"
    case s.GetBool(wantB):
        return "guest_b"
    }
    return "none"
}
r.Invariant("one_holder").Watches(wantA, wantB, holder).
    Holds(func(s gsm.State) bool { return s.Get(holder) == assign(s) }).
    Repair(func(s gsm.State) gsm.State { return s.Set(holder, assign(s)) }).Add()
r.Event("request_a").Writes(wantA).Apply(func(s gsm.State) gsm.State { return s.SetBool(wantA, true) }).Add()
r.Event("request_b").Writes(wantB).Apply(func(s gsm.State) gsm.State { return s.SetBool(wantB, true) }).Add()
m, report, err := r.Build()
if err != nil {
    panic(fmt.Sprintf("%v\n%s", err, report))
}
replica := m.Apply(m.NewState(), "request_b")
told := replica.Get(holder) // what guest B is told now
replica = m.Apply(replica, "request_a")
fmt.Println(told, "->", replica.Get(holder)) // guest_b -> guest_a: B was confirmed, then walked
```

<a id="n2"></a>
#### N2. Exactly one OrderPlaced message per order (MSV-05): a transport property

<!-- gocheck: run -->
```go
// MSV-05: every committed order publishes exactly one OrderPlaced message. That is a property
// of the outbox and the transport, not of a state. gsm can say which events must not be
// redelivered (Report.NotIdempotent) and states it as an obligation; it cannot check that the
// broker delivers once.
o := gsm.NewRegistry("outbox")
published := o.Int("order_placed_published", 0, 3)
o.On("publish_order_placed").Does(gsm.Inc(published)).Add()
_, report, err := o.Build()
if err != nil {
    panic(fmt.Sprintf("%v\n%s", err, report))
}
fmt.Println(report.NotIdempotent) // [publish_order_placed]: an obligation on you, not a check
```

<a id="n3"></a>
#### N3. Every hold resolved within 15 minutes (BKG-13): a deadline, not a state

The same registry verifies BKG-12 (conversion wins over a late expiry) at scale.

<!-- gocheck: run -->
```go
// BKG-13: every hold is resolved (converted or released) within 15 minutes. gsm proves that
// every order of the events that do arrive reaches one state; it says nothing about whether
// or when an event arrives. A hold that no event ever touches is a valid state forever.
h := gsm.NewRegistry("hold")
converted, expired := h.Bool("converted"), h.Bool("expired")
status := h.Enum("status", "held", "converted", "released")
resolve := func(s gsm.State) string {
    switch {
    case s.GetBool(converted):
        return "converted" // a conversion wins over a late expiry
    case s.GetBool(expired):
        return "released"
    }
    return "held"
}
h.Invariant("status_follows_facts").Watches(converted, expired, status).
    Holds(func(s gsm.State) bool { return s.Get(status) == resolve(s) }).
    Repair(func(s gsm.State) gsm.State { return s.Set(status, resolve(s)) }).Add()
h.Event("convert").Writes(converted).Apply(func(s gsm.State) gsm.State { return s.SetBool(converted, true) }).Add()
h.Event("expire").Writes(expired).Apply(func(s gsm.State) gsm.State { return s.SetBool(expired, true) }).Add()
m, report, err := h.Build()
if err != nil {
    panic(fmt.Sprintf("%v\n%s", err, report))
}
stuck := m.NewState()
fmt.Println(m.IsValid(stuck), stuck.Get(status)) // true held: valid, and nothing forces progress
```

## The catalog

Each row: the invariant; how it is expressed today, with the encoding sketched; whether it
verifies at production scale and by which path; the blocking reason otherwise; and the extensions
that would move it ([Ranked extensions](#ranked-extensions)). IDs link to the program that
validates the row, where there is one.

### Inventory and warehouse

| ID | Invariant | Expressible today | At production scale | Blocker | Would move with |
|---|---|---|---|---|---|
| INV-01 | On-hand quantity never goes negative; a pick beyond on-hand is recorded as a backorder. | combinator: received and picked counters that only rise; backorder flag when picked > received | toy | arithmetic over wide ranges | LIN |
| INV-02 | Reserved quantity never exceeds on-hand per SKU and location; reservations beyond it wait. | combinator: as INV-01 with a reserved counter; waiting flag derived | toy | arithmetic over wide ranges | LIN |
| INV-03 ([T4](#t4)) | Units reserved across all SKUs in a warehouse stay within its daily pick capacity; reservations beyond it are deferred. | encoding: one registry holding every SKU's reserved count, deferred flag over the sum | toy | aggregate across items | AGG |
| INV-04 | A SKU's available-to-sell on the storefront equals on-hand summed over its locations minus reservations. | encoding: one registry holding every location's counters plus the derived total | toy | aggregate across items | AGG |
| INV-05 | A bin holds at most one SKU; a put-away of a second SKU into an occupied bin is redirected. | encoding: per bin, the occupant as an owner register (the lowest SKU id wins), the put-away carrying the SKU id as a parameter | **yes** (Abstract) |  |  |
| INV-06 | A lot on QA hold is never shipped; a pick confirmed before the hold arrives is reversed. | combinator: per lot, picked and hold facts, outcome derived | **yes** (collection) |  |  |
| INV-07 | An expired lot is not allocated; an unpicked allocation that meets the expiry is released. | encoding: expiry as a fact event from a scheduler; picked wins over a later expiry | **yes** (collection) |  |  |
| INV-08 | Lots of a SKU are allocated first-expiry-first-out. | encoding: one registry holding the SKU's lots and their expiry ranks | toy | cross-item constraint | XREL |
| INV-09 | A cycle-count adjustment equals the counted quantity minus the system quantity at count time. | combinator: Sub of two counters; the count event carries the quantity as a parameter, one instance per value (abstraction refuses the arithmetic) | toy | arithmetic over wide ranges | LIN |
| INV-10 | A stock transfer only moves forward (requested, shipped, received); a late 'shipped' never reopens a received transfer. | encoding: one fact per milestone, status derived as the furthest | **yes** (collection) |  |  |
| INV-11 | A serialized unit is in exactly one location: the latest scan wins. | encoding: per serial, location and scan time as a last-writer-wins pair, the scan carrying both as parameters | **yes** (Abstract) |  |  |

### Orders and fulfillment

| ID | Invariant | Expressible today | At production scale | Blocker | Would move with |
|---|---|---|---|---|---|
| ORD-01 | An order never ships unpaid. | combinator: the README example (facts and a derived status) | **yes** (collection) |  |  |
| ORD-02 | Cancellation wins over a shipment request, in every arrival order. | combinator: the README example | **yes** (collection) |  |  |
| ORD-03 ([V1](#v1)) | Order status only moves forward; a late 'packed' never moves a delivered order back. | encoding: one Bool per milestone, status derived as the furthest | **yes** (collection) |  |  |
| ORD-04 | Shipped quantity per order line never exceeds the ordered quantity; excess shipments are flagged. | combinator: shipped counter against the ordered quantity | toy | arithmetic over wide ranges | LIN |
| ORD-05 | Order total = sum of (quantity × unit price) - discounts + tax. | closure: multiplication needs a closure | toy | arithmetic over wide ranges | LIN |
| ORD-06 | An order is complete iff every line is shipped or cancelled. | encoding: two Bools per line in one registry, 100 bits at 50 lines | toy | aggregate across items | AGG |
| ORD-07 | A return is accepted only for a delivered order, and the refund is issued only after the return is received. | combinator: per order facts, outcome derived | **yes** (collection) |  |  |
| ORD-08 | Each order is fulfilled by exactly one fulfillment center; conflicting assignments resolve to the lowest-numbered center. | encoding: min-register Int(0,100), one event per center (100 events, 4,950 pairs) | **yes** (collection) |  |  |
| ORD-09 | No serialized unit is claimed by two orders. | encoding: per unit, the claimant as an owner register, the claim carrying the order id as a parameter | **yes** (Abstract) |  |  |
| ORD-10 | An order not shipped by its promise date is flagged late. | encoding: promise-date-passed as a fact event; late = passed and not shipped | **yes** (collection) |  |  |
| ORD-11 | Every paid order ships within 48 hours. | no: a deadline on when events happen (liveness), not a property of the states events reach | no | other: liveness | none listed |
| ORD-12 ([V5](#v5)) | An order whose total exceeds the customer's credit limit is held until a manager approves an override; a frozen credit line holds every order. | combinator: amounts fixed at creation, compared only; flags as 0/1 Ints | **yes** (Abstract) |  |  |

### Payments, wallets and ledgers

| ID | Invariant | Expressible today | At production scale | Blocker | Would move with |
|---|---|---|---|---|---|
| PAY-01 ([T2](#t2)) | A wallet balance never goes below zero; withdrawals beyond the available funds stay pending until covered. | combinator: deposited and withdrawn counters that only rise; the amount as a parameter, one instance per value (abstraction refuses the arithmetic) | toy | arithmetic over wide ranges | LIN |
| PAY-02 | Every journal entry balances: total debits equal total credits. | combinator: one variable per posting, sum compared | toy | arithmetic over wide ranges | LIN |
| PAY-03 | Captured never exceeds authorized, and refunded never exceeds captured. | combinator: three amount counters | toy | arithmetic over wide ranges | LIN |
| PAY-04 ([V4](#v4)) | A payment is captured at most once; redelivered capture messages are absorbed. | combinator: captured Bool; Report.NotIdempotent empty | **yes** (collection) |  |  |
| PAY-05 ([V4](#v4)) | A refund that arrives before the capture is held, then applied after it. | combinator: refund_requested fact; refunded derived | **yes** (collection) |  |  |
| PAY-06 ([T5](#t5)) | Payouts are frozen while a merchant has any open dispute. | encoding: opened and closed counters that only rise (a net counter fails CC at its bound); lifetime counts beyond 511 do not fit | toy | arithmetic over wide ranges | DIFF or AGG |
| PAY-07 | Customer wallet balances plus fees equal the funds held at the bank. | encoding: one registry holding every wallet | toy | aggregate across items | AGG |
| PAY-08 | A transfer debits one wallet and credits another; no money is created or destroyed. | encoding: one registry holding both wallets | toy | cross-item constraint | XREL + LIN |
| PAY-09 | Card spend per calendar day stays within the daily limit; transactions beyond it are held for review. | encoding: key by card and day; spend counter against the limit | toy | arithmetic over wide ranges | LIN |
| PAY-10 ([V4](#v4)) | One charge per idempotency key. | combinator: collection keyed by the idempotency key | **yes** (collection) |  |  |
| PAY-11 | An authorization expires after 7 days unless captured; a capture after expiry fails. | encoding: the capture carries its time; the expiry is fixed at creation; the earliest capture counts | **yes** (Abstract) |  |  |
| PAY-12 | An ATM approves a withdrawal against the current balance and never revokes an approval. | no: an irrevocable answer that depends on arrival order, so it needs coordination | no | other: needs coordination | none listed |
| PAY-13 | A posted ledger entry is never edited; it is only ever corrected by a reversing entry. | combinator: per entry, posted and reversed facts | **yes** (collection) |  |  |

### Bookings and reservations

| ID | Invariant | Expressible today | At production scale | Blocker | Would move with |
|---|---|---|---|---|---|
| BKG-01 ([V7](#v7)) | No room is assigned to two reservations on the same night. | encoding: per room-night an owner register over reservation ids, the claim carrying the id as a parameter (one registry holding the candidates does not fit, T3) | **yes** (Abstract) |  |  |
| BKG-02 ([V2](#v2)) | Stop-sell: a room type closes for a night once net bookings reach rooms plus the overbooking allowance, and reopens when cancellations bring it below. | encoding: booked and cancelled counters that only rise, per (room type, night); 20 bits for up to 511 bookings | **yes** (collection) |  |  |
| BKG-03 | Confirmed room-nights per type never exceed rooms plus allowance; the excess is walked, latest booking first. | encoding: one registry holding the night's bookings and their ranks | toy | cross-item constraint | AGG + XREL |
| BKG-04 | A multi-night stay is confirmed only if every night is available (all or nothing). | encoding: one registry holding the stay and every night it spans | toy | cross-item constraint | AGG + XREL |
| BKG-05 | Check-in only on or after the arrival date; check-out only after check-in. | encoding: arrival-date-reached as a fact event; status derived | **yes** (collection) |  |  |
| BKG-06 ([V8](#v8)) | A cancellation inside the free-cancellation window costs nothing; after it, one night's fee. | encoding: the cancellation carries its time; the deadline is fixed at creation; the fee is derived | **yes** (Abstract) |  |  |
| BKG-07 | The rate charged is the rate locked at booking, even if the rate plan changes later. | encoding: the booking carries its rate as a parameter; one booking per reservation, causally ordered | **yes** (Abstract) |  |  |
| BKG-08 | Pickups from a group block stop at the cutoff date; later pickups go to general inventory. | encoding: the pickup carries its time; the cutoff is fixed at creation | **yes** (Abstract) |  |  |
| BKG-09 | A room is assignable to an arriving guest only when it is inspected clean and vacant. | combinator: per room facts, assignable derived | **yes** (collection) |  |  |
| BKG-10 | A no-show fee is charged at most once, and only after the arrival date passes without check-in; a late check-in reverses it. | encoding: arrival-passed fact; fee = passed and not checked in | **yes** (collection) |  |  |
| BKG-11 ([N1](#n1)) | Two guests are never told that the last room is theirs, not even briefly. | no: an answer that must never be revised, needs coordination | no | other: needs coordination | none listed |
| BKG-12 ([N3](#n3)) | A hold is released when it expires unless it has been converted; a conversion always wins. | encoding: expiry as a fact event from a timer | **yes** (collection) |  |  |
| BKG-13 ([N3](#n3)) | Every hold is resolved (converted or released) within 15 minutes. | no: a deadline (liveness); a hold no event touches is valid forever | no | other: liveness | none listed |
| BKG-14 | Room-nights sold through a channel stay within its allotment; sales beyond it are flagged for the revenue manager. | encoding: counters that only rise per (channel, room type, night), as BKG-02 | **yes** (collection) |  |  |

### Loyalty points

| ID | Invariant | Expressible today | At production scale | Blocker | Would move with |
|---|---|---|---|---|---|
| LOY-01 | A points balance never goes negative; redemptions beyond it stay pending. | combinator: earned and redeemed counters; the points as a parameter, one instance per value (abstraction refuses the arithmetic) | toy | arithmetic over wide ranges | LIN |
| LOY-02 | A member is Gold iff qualifying points over the last 12 months reach 50,000. | encoding: monthly buckets in one registry, rolled by month events | toy | history or time | LIN + TIME |
| LOY-03 | Points for a stay post once, only after checkout, and reverse if the stay is refunded. | combinator: per stay facts, posted derived | **yes** (collection) |  |  |
| LOY-04 | Points expire 24 months after the member's last activity. | encoding: last activity as a max-register, now as a time slot | toy | history or time | DIFF or TIME |
| LOY-05 | A member's balance equals points earned minus redeemed minus expired. | encoding: one registry holding the member's postings | toy | aggregate across items | AGG |
| LOY-06 | A reward certificate is used at most once; a redelivered redemption does not use it twice. | combinator: per certificate, used Bool | **yes** (collection) |  |  |
| LOY-07 | A tier never drops during the membership year. | encoding: key by (member, year); tier as a max-register over 4 levels | **yes** (collection) |  |  |
| LOY-08 | A points transfer to an airline partner completes on both sides or is reversed on both. | combinator: per transfer saga, debited, credited, failed facts | **yes** (collection) |  |  |
| LOY-09 | Redemptions are disabled while the member is under fraud review. | combinator: per member facts | **yes** (collection) |  |  |
| LOY-10 | The welcome bonus is earned once per person, even across re-enrollment under a new member number. | encoding: one registry holding the person's member numbers | toy | unbounded or relational data | SET |

### Subscriptions and billing

| ID | Invariant | Expressible today | At production scale | Blocker | Would move with |
|---|---|---|---|---|---|
| SUB-01 | Access for a billing period is granted iff its invoice is paid or still in grace. | encoding: key by (subscription, period); grace expiry as a fact event | **yes** (collection) |  |  |
| SUB-02 | A subscription is never invoiced twice for the same period. | combinator: key by (subscription, period); invoiced Bool | **yes** (collection) |  |  |
| SUB-03 | A mid-cycle plan change credits unused days / days in period × old price. | closure: division and multiplication of variables | toy | arithmetic over wide ranges | none listed |
| SUB-04 | Seats assigned never exceed seats purchased; assignments beyond wait for a seat. | combinator: counters that only rise; 10,000 seats need 14 bits per counter | toy | arithmetic over wide ranges | LIN |
| SUB-05 | One free trial per payment card, ever. | combinator: key by card fingerprint; trial_used Bool | **yes** (collection) |  |  |
| SUB-06 | A downgrade takes effect at period end and an upgrade at once; the latest request wins. | encoding: plan and request time as a last-writer-wins register, carried by the request | **yes** (Abstract) |  |  |
| SUB-07 | After three failed payment attempts the subscription is suspended; a successful payment reactivates it. | combinator: per invoice, failures Int(0,3), paid Bool; payment_failed needs exactly-once delivery | **yes** (collection) |  |  |
| SUB-08 | Invoice total = line items + tax - credits, with tax = rate × subtotal, rounded. | closure: multiplication by a rate and rounding | toy | arithmetic over wide ranges | LIN |
| SUB-09 | Each usage record is billed exactly once, in exactly one period. | encoding: one registry holding the period's usage records | toy | aggregate across items | AGG |
| SUB-10 | Usage beyond the plan's included units is billed as overage. | combinator: usage counter against a constant; 1,000,000 units plus the flag is 21 bits | toy | arithmetic over wide ranges | DIFF or BITS or AGG |

### Approvals and workflows

| ID | Invariant | Expressible today | At production scale | Blocker | Would move with |
|---|---|---|---|---|---|
| APR-01 ([V3](#v3)) | A purchase order over $10,000 needs two approvals; others need one. | encoding: threshold stored as a Bool at creation; one Bool per approver in the cost center's pool | **yes** (Build) |  |  |
| APR-02 ([V3](#v3)) | A rejection at any level is final; a request is approved only when every required approval is in. | combinator: rejected fact wins in the derived outcome | **yes** (Build) |  |  |
| APR-03 ([V3](#v3)) | No one approves their own request. | encoding: requester stored as an index into the approver pool | **yes** (Build) |  |  |
| APR-04 | An expense report's total per category stays within the policy limit, else it goes to exception review. | closure: sum of line amounts | toy | aggregate across items | LIN |
| APR-05 | A signed document never changes; edits that arrive after the signature are rejected. | encoding: versions on edits and on the signature, carried as parameters; edits above the signed version rejected | **yes** (Abstract) |  |  |
| APR-06 | A request pending for more than 48 hours escalates to the next level. | encoding: SLA-breached fact; escalated = breached and undecided | **yes** (collection) |  |  |
| APR-07 | A withdrawn request returns to draft, and approvals from an earlier submission round never count. | encoding: per approver, the latest round approved, against the current round; approvals must carry their round | toy | history or time | AGG |
| APR-08 | A delegation of approval authority is valid only between its start and end dates. | encoding: the approval carries its time; the window is fixed at creation | **yes** (Abstract) |  |  |
| APR-09 | Every approved request has an audit record of the approval. | encoding: federation, workflow is the authority over the audit service's approval record (a projection) | **yes** (federation) |  |  |
| APR-10 | An approver's approval counts only up to their personal approval limit. | encoding: amount and limits are fixed at creation, so each approver's eligibility is stored as a Bool | **yes** (Build) |  |  |

### Access control and quotas

| ID | Invariant | Expressible today | At production scale | Blocker | Would move with |
|---|---|---|---|---|---|
| ACL-01 | A user can access a resource iff a role they currently hold grants it. | encoding: per (user, role), grant and revoke need precedence (ACL-02); access joins the roles that grant the resource | toy | unbounded or relational data | XREL |
| ACL-02 ([V9](#v9)) | A revocation beats any grant issued before it, but a later re-grant restores access. | encoding: versions on grants and revocations, carried as parameters; access derived from the latest of each | **yes** (Abstract) |  |  |
| ACL-03 | Sessions issued before an account suspension stop working. | encoding: session issue time against the suspension time, for every session of the account | toy | unbounded or relational data | XREL |
| ACL-04 ([T1](#t1)) | Requests per API key per month stay within 1,000,000; requests beyond are throttled. | combinator: used Int(0,1000000) + throttled, 21 bits | toy | arithmetic over wide ranges | BITS or DIFF or AGG |
| ACL-05 | At most 100 requests per key in any rolling 60 seconds. | encoding: the timestamps of the last 100 requests | toy | history or time | AGG + TIME |
| ACL-06 | A tenant's stored bytes stay within the plan limit; uploads beyond it are held. | encoding: one registry holding the tenant's files | toy | aggregate across items | AGG |
| ACL-07 | An organization always keeps at least one admin; removing the last admin is undone. | encoding: one registry holding the organization's members | toy | aggregate across items | AGG + SET |
| ACL-08 | Each email address belongs to at most one account. | encoding: key by email; owner register over account ids, the claim carrying the id as a parameter | **yes** (Abstract) |  |  |
| ACL-09 | Privileged roles take effect only after MFA enrollment. | combinator: per user facts, effective role derived | **yes** (collection) |  |  |
| ACL-10 | A license allows at most 5 concurrent sessions; extra sessions are queued. | encoding: login and logout counters that only rise, over the license's lifetime | toy | arithmetic over wide ranges | DIFF or AGG |
| ACL-11 | A suspended user has no privileged access, whatever order the suspension and grants arrive in. | combinator: suspension wins in the derived access | **yes** (collection) |  |  |

### Scheduling and capacity

| ID | Invariant | Expressible today | At production scale | Blocker | Would move with |
|---|---|---|---|---|---|
| SCH-01 | An employee is never on two overlapping shifts. | encoding: one registry holding the employee's shifts and their intervals | toy | cross-item constraint | XREL |
| SCH-02 | Class enrollment never exceeds capacity; enrollments beyond it are waitlisted (by count). | encoding: enrolled and dropped counters that only rise per class (8 bits each) | **yes** (collection) |  |  |
| SCH-03 | When a seat frees up, the earliest waitlisted student gets it. | encoding: one registry holding the class's waitlist | toy | cross-item constraint | AGG + XREL |
| SCH-04 | At least 11 hours of rest between an employee's consecutive shifts. | encoding: one registry holding the employee's shifts | toy | cross-item constraint | XREL + DIFF |
| SCH-05 | Weekly hours per employee stay within 40 unless overtime is approved. | encoding: one registry holding the week's shifts | toy | aggregate across items | AGG |
| SCH-06 | A meeting room holds at most one meeting per 15-minute slot. | encoding: key by (room, slot); holder register over meeting ids, the claim carrying the id as a parameter | **yes** (Abstract) |  |  |
| SCH-07 | Every shift has at least one certified supervisor assigned. | encoding: per shift, certified-assigned and certified-unassigned counters that only rise | **yes** (collection) |  |  |
| SCH-08 | A pickup slot takes at most 50 orders; overflow rolls to the next slot. | encoding: one registry holding consecutive slots | toy | cross-item constraint | AGG + XREL |
| SCH-09 | Exactly one doctor is primary on call each day. | encoding: one registry holding the day's claimants | toy | unbounded or relational data | SET |
| SCH-10 | Equipment under maintenance is not assigned to jobs during the maintenance interval. | encoding: one registry holding the equipment's jobs and intervals | toy | cross-item constraint | XREL |

### Multi-service consistency

| ID | Invariant | Expressible today | At production scale | Blocker | Would move with |
|---|---|---|---|---|---|
| MSV-01 | The order service shows 'paid' iff the payment service has captured the payment. | combinator: federation, payments is the authority over the order's paid flag | **yes** (federation) |  |  |
| MSV-02 | Inventory holds a reservation only while the order service has the order open (no orphan reservations). | combinator: federation, orders is the authority over the reservation's active flag | **yes** (federation) |  |  |
| MSV-03 | Trip saga: if the flight, hotel or car leg fails, every confirmed leg is cancelled. | combinator: one registry per trip holding the three legs (orchestrated saga) | **yes** (collection) |  |  |
| MSV-04 | A product deleted from the catalog disappears from search and recommendations. | combinator: federation, catalog is the authority over each read model's listed flag | **yes** (federation) |  |  |
| MSV-05 ([N2](#n2)) | Every committed order publishes exactly one OrderPlaced message. | no: a property of the outbox and transport; gsm states it as an obligation (NotIdempotent) | no | other: transport | none listed |
| MSV-06 | Two-way availability sync between the hotel PMS and an OTA channel manager. | encoding: a federation cycle that is not monotone; BuildCoordinated verifies only the residual | no | other: needs coordination | none listed |
| MSV-07 ([V6](#v6)) | A GDPR erasure recorded in the identity service reaches CRM and marketing. | combinator: federation, identity is the authority over each target's erased flag | **yes** (federation) |  |  |
| MSV-08 | An external order id maps to at most one internal order across shards. | encoding: key by external id; owner register over internal ids, the claim carrying the id as a parameter | **yes** (Abstract) |  |  |
| MSV-09 ([V10](#v10)) | The price charged equals the price shown at checkout. | encoding: the checkout carries the price it showed, and the charge is derived from it (a closure reading the catalog is frozen at build time, T6); one checkout per order, causally ordered | **yes** (Abstract) |  |  |
| MSV-10 | Raising the per-guest hold limit from 5 to 10 while holds are in flight keeps every replica converging. | combinator: CheckMigration on the per-entity registry, SAFE ONLINE | **yes** (CheckMigration) |  |  |
| MSV-11 ([V9](#v9)) | The latest address update wins in every service. | encoding: address version and update time as a last-writer-wins register, carried by the update | **yes** (Abstract) |  |  |
| MSV-12 | Invoice numbers are sequential and gap-free across the company. | no: a number once issued must never change, needs coordination | no | other: needs coordination | none listed |
| MSV-13 | The PMS's room status drives the front desk's assignable flag and housekeeping's task list. | combinator: federation, PMS is the authority over both projections | **yes** (federation) |  |  |
| MSV-14 | Fulfillment releases an order only if the payment service's authorized amount covers the order total. | encoding: amounts fixed at creation, compared only, but in two registries; a federation refuses Abstract components | toy | other: reductions do not combine | COMB |
| MSV-15 ([V6](#v6)) | Marketing drops a queued campaign once the identity service records an erasure. | combinator: M1 refuses the reacting target in a federation; one registry per customer holding both facts | **yes** (collection) |  |  |

