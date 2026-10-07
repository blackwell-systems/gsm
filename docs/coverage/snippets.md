V1

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

V2

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

V3

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

V4

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

V5

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

V6

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

V7

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

V8

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

V9

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

V10

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

T1

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

T2

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

T3

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

T4

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

T5

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

T6

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

N1

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

N2

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

N3

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
