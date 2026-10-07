# Invariant coverage catalog: one row per invariant. The doc's tables and numbers are generated
# from this file, so the counts cannot drift from the rows.
#
# expr:   comb | closure | enc | no
# scale:  yes | toy | no
# path:   for yes rows (collection, Build, Abstract, per-component, federation, CheckMigration)
# reason: for toy/no rows: ARITH AGG TIME REL CLOS XITEM OTHER
# moves:  alternatives, each a '+'-joined set of extensions that together would make it "yes";
#         '' for yes rows; 'none' when no listed extension helps.
# kind:   lifecycle ordering arithmetic aggregate uniqueness referential temporal cross-item guarantee

R = []
def row(id, dom, kind, text, expr, sketch, scale, path_or_reason, moves="", val=""):
    R.append(dict(id=id, dom=dom, kind=kind, text=text, expr=expr, sketch=sketch, scale=scale,
                  pr=path_or_reason, moves=moves, val=val))

D = "Inventory and warehouse"
row("INV-01", D, "arithmetic", "On-hand quantity never goes negative; a pick beyond on-hand is recorded as a backorder.",
    "comb", "received and picked counters that only rise; backorder flag when picked > received", "toy", "ARITH", "PARAM+LIN")
row("INV-02", D, "arithmetic", "Reserved quantity never exceeds on-hand per SKU and location; reservations beyond it wait.",
    "comb", "as INV-01 with a reserved counter; waiting flag derived", "toy", "ARITH", "PARAM+LIN")
row("INV-03", D, "aggregate", "Units reserved across all SKUs in a warehouse stay within its daily pick capacity; reservations beyond it are deferred.",
    "enc", "one registry holding every SKU's reserved count, deferred flag over the sum", "toy", "AGG", "AGG+PARAM", "T4")
row("INV-04", D, "aggregate", "A SKU's available-to-sell on the storefront equals on-hand summed over its locations minus reservations.",
    "enc", "one registry holding every location's counters plus the derived total", "toy", "AGG", "AGG+PARAM")
row("INV-05", D, "uniqueness", "A bin holds at most one SKU; a put-away of a second SKU into an occupied bin is redirected.",
    "enc", "per bin, the occupant as an owner register (the lowest SKU id wins), the put-away carrying the SKU id as a parameter", "yes", "Abstract", "")
row("INV-06", D, "lifecycle", "A lot on QA hold is never shipped; a pick confirmed before the hold arrives is reversed.",
    "comb", "per lot: picked and hold facts, outcome derived", "yes", "collection")
row("INV-07", D, "temporal", "An expired lot is not allocated; an unpicked allocation that meets the expiry is released.",
    "enc", "expiry as a fact event from a scheduler; picked wins over a later expiry", "yes", "collection")
row("INV-08", D, "cross-item", "Lots of a SKU are allocated first-expiry-first-out.",
    "enc", "one registry holding the SKU's lots and their expiry ranks", "toy", "XITEM", "XREL+PARAM")
row("INV-09", D, "arithmetic", "A cycle-count adjustment equals the counted quantity minus the system quantity at count time.",
    "comb", "Sub of two counters; the count event carries the quantity as a parameter, one instance per value (abstraction refuses the arithmetic)", "toy", "ARITH", "PARAM+LIN")
row("INV-10", D, "ordering", "A stock transfer only moves forward (requested, shipped, received); a late 'shipped' never reopens a received transfer.",
    "enc", "one fact per milestone, status derived as the furthest", "yes", "collection")
row("INV-11", D, "temporal", "A serialized unit is in exactly one location: the latest scan wins.",
    "enc", "per serial: location and scan time as a last-writer-wins pair, the scan carrying both as parameters", "yes", "Abstract", "")

D = "Orders and fulfillment"
row("ORD-01", D, "lifecycle", "An order never ships unpaid.", "comb", "the README example (facts and a derived status)", "yes", "collection")
row("ORD-02", D, "lifecycle", "Cancellation wins over a shipment request, in every arrival order.", "comb", "the README example", "yes", "collection")
row("ORD-03", D, "ordering", "Order status only moves forward; a late 'packed' never moves a delivered order back.",
    "enc", "one Bool per milestone, status derived as the furthest", "yes", "collection", "", "V1")
row("ORD-04", D, "arithmetic", "Shipped quantity per order line never exceeds the ordered quantity; excess shipments are flagged.",
    "comb", "shipped counter against the ordered quantity", "toy", "ARITH", "PARAM+LIN")
row("ORD-05", D, "arithmetic", "Order total = sum of (quantity × unit price) - discounts + tax.",
    "closure", "multiplication needs a closure", "toy", "ARITH", "PARAM+LIN")
row("ORD-06", D, "aggregate", "An order is complete iff every line is shipped or cancelled.",
    "enc", "two Bools per line in one registry: 100 bits at 50 lines", "toy", "AGG", "AGG")
row("ORD-07", D, "lifecycle", "A return is accepted only for a delivered order, and the refund is issued only after the return is received.",
    "comb", "per order facts, outcome derived", "yes", "collection")
row("ORD-08", D, "uniqueness", "Each order is fulfilled by exactly one fulfillment center; conflicting assignments resolve to the lowest-numbered center.",
    "enc", "min-register Int(0,100), one event per center (100 events, 4,950 pairs)", "yes", "collection")
row("ORD-09", D, "uniqueness", "No serialized unit is claimed by two orders.",
    "enc", "per unit, the claimant as an owner register, the claim carrying the order id as a parameter", "yes", "Abstract", "")
row("ORD-10", D, "temporal", "An order not shipped by its promise date is flagged late.",
    "enc", "promise-date-passed as a fact event; late = passed and not shipped", "yes", "collection")
row("ORD-11", D, "guarantee", "Every paid order ships within 48 hours.",
    "no", "a deadline on when events happen (liveness), not a property of the states events reach", "no", "OTHER", "none")
row("ORD-12", D, "lifecycle", "An order whose total exceeds the customer's credit limit is held until a manager approves an override; a frozen credit line holds every order.",
    "comb", "amounts fixed at creation, compared only; flags as 0/1 Ints", "yes", "Abstract", "", "V5")

D = "Payments, wallets and ledgers"
row("PAY-01", D, "arithmetic", "A wallet balance never goes below zero; withdrawals beyond the available funds stay pending until covered.",
    "comb", "deposited and withdrawn counters that only rise; the amount as a parameter, one instance per value (abstraction refuses the arithmetic)", "toy", "ARITH", "PARAM+LIN", "T2")
row("PAY-02", D, "arithmetic", "Every journal entry balances: total debits equal total credits.",
    "comb", "one variable per posting, sum compared", "toy", "ARITH", "PARAM+LIN")
row("PAY-03", D, "arithmetic", "Captured never exceeds authorized, and refunded never exceeds captured.",
    "comb", "three amount counters", "toy", "ARITH", "PARAM+LIN")
row("PAY-04", D, "temporal", "A payment is captured at most once; redelivered capture messages are absorbed.",
    "comb", "captured Bool; Report.NotIdempotent empty", "yes", "collection", "", "V4")
row("PAY-05", D, "ordering", "A refund that arrives before the capture is held, then applied after it.",
    "comb", "refund_requested fact; refunded derived", "yes", "collection", "", "V4")
row("PAY-06", D, "aggregate", "Payouts are frozen while a merchant has any open dispute.",
    "enc", "opened and closed counters that only rise (a net counter fails CC at its bound); lifetime counts beyond 511 do not fit", "toy", "ARITH", "DIFF|AGG", "T5")
row("PAY-07", D, "aggregate", "Customer wallet balances plus fees equal the funds held at the bank.",
    "enc", "one registry holding every wallet", "toy", "AGG", "AGG+PARAM")
row("PAY-08", D, "cross-item", "A transfer debits one wallet and credits another; no money is created or destroyed.",
    "enc", "one registry holding both wallets", "toy", "XITEM", "XREL+PARAM+LIN")
row("PAY-09", D, "arithmetic", "Card spend per calendar day stays within the daily limit; transactions beyond it are held for review.",
    "enc", "key by card and day; spend counter against the limit", "toy", "ARITH", "PARAM+LIN")
row("PAY-10", D, "uniqueness", "One charge per idempotency key.", "comb", "collection keyed by the idempotency key", "yes", "collection", "", "V4")
row("PAY-11", D, "temporal", "An authorization expires after 7 days unless captured; a capture after expiry fails.",
    "enc", "the capture carries its time; the expiry is fixed at creation; the earliest capture counts", "yes", "Abstract", "")
row("PAY-12", D, "guarantee", "An ATM approves a withdrawal against the current balance and never revokes an approval.",
    "no", "an irrevocable answer that depends on arrival order, so it needs coordination", "no", "OTHER", "none")
row("PAY-13", D, "temporal", "A posted ledger entry is never edited; it is only ever corrected by a reversing entry.",
    "comb", "per entry: posted and reversed facts", "yes", "collection")

D = "Bookings and reservations"
row("BKG-01", D, "cross-item", "No room is assigned to two reservations on the same night.",
    "enc", "per room-night an owner register over reservation ids, the claim carrying the id as a parameter (one registry holding the candidates does not fit, T3)", "yes", "Abstract", "", "V7")
row("BKG-02", D, "aggregate", "Stop-sell: a room type closes for a night once net bookings reach rooms plus the overbooking allowance, and reopens when cancellations bring it below.",
    "enc", "booked and cancelled counters that only rise, per (room type, night); 20 bits for up to 511 bookings", "yes", "collection", "", "V2")
row("BKG-03", D, "cross-item", "Confirmed room-nights per type never exceed rooms plus allowance; the excess is walked, latest booking first.",
    "enc", "one registry holding the night's bookings and their ranks", "toy", "XITEM", "AGG+XREL+PARAM")
row("BKG-04", D, "cross-item", "A multi-night stay is confirmed only if every night is available (all or nothing).",
    "enc", "one registry holding the stay and every night it spans", "toy", "XITEM", "AGG+XREL")
row("BKG-05", D, "temporal", "Check-in only on or after the arrival date; check-out only after check-in.",
    "enc", "arrival-date-reached as a fact event; status derived", "yes", "collection")
row("BKG-06", D, "temporal", "A cancellation inside the free-cancellation window costs nothing; after it, one night's fee.",
    "enc", "the cancellation carries its time; the deadline is fixed at creation; the fee is derived", "yes", "Abstract", "", "V8")
row("BKG-07", D, "temporal", "The rate charged is the rate locked at booking, even if the rate plan changes later.",
    "enc", "the booking carries its rate as a parameter; one booking per reservation, causally ordered", "yes", "Abstract", "")
row("BKG-08", D, "temporal", "Pickups from a group block stop at the cutoff date; later pickups go to general inventory.",
    "enc", "the pickup carries its time; the cutoff is fixed at creation", "yes", "Abstract", "")
row("BKG-09", D, "lifecycle", "A room is assignable to an arriving guest only when it is inspected clean and vacant.",
    "comb", "per room facts, assignable derived", "yes", "collection")
row("BKG-10", D, "temporal", "A no-show fee is charged at most once, and only after the arrival date passes without check-in; a late check-in reverses it.",
    "enc", "arrival-passed fact; fee = passed and not checked in", "yes", "collection")
row("BKG-11", D, "guarantee", "Two guests are never told that the last room is theirs, not even briefly.",
    "no", "an answer that must never be revised: needs coordination", "no", "OTHER", "none", "N1")
row("BKG-12", D, "temporal", "A hold is released when it expires unless it has been converted; a conversion always wins.",
    "enc", "expiry as a fact event from a timer", "yes", "collection", "", "N3")
row("BKG-13", D, "guarantee", "Every hold is resolved (converted or released) within 15 minutes.",
    "no", "a deadline (liveness); a hold no event touches is valid forever", "no", "OTHER", "none", "N3")
row("BKG-14", D, "aggregate", "Room-nights sold through a channel stay within its allotment; sales beyond it are flagged for the revenue manager.",
    "enc", "counters that only rise per (channel, room type, night), as BKG-02", "yes", "collection")

D = "Loyalty points"
row("LOY-01", D, "arithmetic", "A points balance never goes negative; redemptions beyond it stay pending.",
    "comb", "earned and redeemed counters; the points as a parameter, one instance per value (abstraction refuses the arithmetic)", "toy", "ARITH", "PARAM+LIN")
row("LOY-02", D, "temporal", "A member is Gold iff qualifying points over the last 12 months reach 50,000.",
    "enc", "monthly buckets in one registry, rolled by month events", "toy", "TIME", "PARAM+LIN+TIME")
row("LOY-03", D, "lifecycle", "Points for a stay post once, only after checkout, and reverse if the stay is refunded.",
    "comb", "per stay facts, posted derived", "yes", "collection")
row("LOY-04", D, "temporal", "Points expire 24 months after the member's last activity.",
    "enc", "last activity as a max-register, now as a time slot", "toy", "TIME", "PARAM+DIFF|TIME")
row("LOY-05", D, "aggregate", "A member's balance equals points earned minus redeemed minus expired.",
    "enc", "one registry holding the member's postings", "toy", "AGG", "AGG+PARAM")
row("LOY-06", D, "temporal", "A reward certificate is used at most once; a redelivered redemption does not use it twice.",
    "comb", "per certificate: used Bool", "yes", "collection")
row("LOY-07", D, "ordering", "A tier never drops during the membership year.",
    "enc", "key by (member, year); tier as a max-register over 4 levels", "yes", "collection")
row("LOY-08", D, "referential", "A points transfer to an airline partner completes on both sides or is reversed on both.",
    "comb", "per transfer saga: debited, credited, failed facts", "yes", "collection")
row("LOY-09", D, "lifecycle", "Redemptions are disabled while the member is under fraud review.",
    "comb", "per member facts", "yes", "collection")
row("LOY-10", D, "uniqueness", "The welcome bonus is earned once per person, even across re-enrollment under a new member number.",
    "enc", "one registry holding the person's member numbers", "toy", "REL", "SET+PARAM")

D = "Subscriptions and billing"
row("SUB-01", D, "temporal", "Access for a billing period is granted iff its invoice is paid or still in grace.",
    "enc", "key by (subscription, period); grace expiry as a fact event", "yes", "collection")
row("SUB-02", D, "uniqueness", "A subscription is never invoiced twice for the same period.",
    "comb", "key by (subscription, period); invoiced Bool", "yes", "collection")
row("SUB-03", D, "arithmetic", "A mid-cycle plan change credits unused days / days in period × old price.",
    "closure", "division and multiplication of variables", "toy", "ARITH", "none")
row("SUB-04", D, "arithmetic", "Seats assigned never exceed seats purchased; assignments beyond wait for a seat.",
    "comb", "counters that only rise; 10,000 seats need 14 bits per counter", "toy", "ARITH", "PARAM+LIN")
row("SUB-05", D, "uniqueness", "One free trial per payment card, ever.",
    "comb", "key by card fingerprint; trial_used Bool", "yes", "collection")
row("SUB-06", D, "temporal", "A downgrade takes effect at period end and an upgrade at once; the latest request wins.",
    "enc", "plan and request time as a last-writer-wins register, carried by the request", "yes", "Abstract", "")
row("SUB-07", D, "lifecycle", "After three failed payment attempts the subscription is suspended; a successful payment reactivates it.",
    "comb", "per invoice: failures Int(0,3), paid Bool; payment_failed needs exactly-once delivery", "yes", "collection")
row("SUB-08", D, "arithmetic", "Invoice total = line items + tax - credits, with tax = rate × subtotal, rounded.",
    "closure", "multiplication by a rate and rounding", "toy", "ARITH", "PARAM+LIN")
row("SUB-09", D, "aggregate", "Each usage record is billed exactly once, in exactly one period.",
    "enc", "one registry holding the period's usage records", "toy", "AGG", "AGG+PARAM")
row("SUB-10", D, "arithmetic", "Usage beyond the plan's included units is billed as overage.",
    "comb", "usage counter against a constant; 1,000,000 units plus the flag is 21 bits", "toy", "ARITH", "DIFF|BITS|AGG")

D = "Approvals and workflows"
row("APR-01", D, "lifecycle", "A purchase order over $10,000 needs two approvals; others need one.",
    "enc", "threshold stored as a Bool at creation; one Bool per approver in the cost center's pool", "yes", "Build", "", "V3")
row("APR-02", D, "lifecycle", "A rejection at any level is final; a request is approved only when every required approval is in.",
    "comb", "rejected fact wins in the derived outcome", "yes", "Build", "", "V3")
row("APR-03", D, "uniqueness", "No one approves their own request.",
    "enc", "requester stored as an index into the approver pool", "yes", "Build", "", "V3")
row("APR-04", D, "aggregate", "An expense report's total per category stays within the policy limit, else it goes to exception review.",
    "closure", "sum of line amounts", "toy", "AGG", "PARAM+LIN")
row("APR-05", D, "temporal", "A signed document never changes; edits that arrive after the signature are rejected.",
    "enc", "versions on edits and on the signature, carried as parameters; edits above the signed version rejected", "yes", "Abstract", "")
row("APR-06", D, "temporal", "A request pending for more than 48 hours escalates to the next level.",
    "enc", "SLA-breached fact; escalated = breached and undecided", "yes", "collection")
row("APR-07", D, "ordering", "A withdrawn request returns to draft, and approvals from an earlier submission round never count.",
    "enc", "per approver, the latest round approved, against the current round; approvals must carry their round", "toy", "TIME", "PARAM+AGG")
row("APR-08", D, "temporal", "A delegation of approval authority is valid only between its start and end dates.",
    "enc", "the approval carries its time; the window is fixed at creation", "yes", "Abstract", "")
row("APR-09", D, "referential", "Every approved request has an audit record of the approval.",
    "enc", "federation: workflow is the authority over the audit service's approval record (a projection)", "yes", "federation")
row("APR-10", D, "lifecycle", "An approver's approval counts only up to their personal approval limit.",
    "enc", "amount and limits are fixed at creation, so each approver's eligibility is stored as a Bool", "yes", "Build")

D = "Access control and quotas"
row("ACL-01", D, "referential", "A user can access a resource iff a role they currently hold grants it.",
    "enc", "per (user, role): grant and revoke need precedence (ACL-02); access joins the roles that grant the resource", "toy", "REL", "PARAM+XREL")
row("ACL-02", D, "temporal", "A revocation beats any grant issued before it, but a later re-grant restores access.",
    "enc", "versions on grants and revocations, carried as parameters; access derived from the latest of each", "yes", "Abstract", "", "V9")
row("ACL-03", D, "temporal", "Sessions issued before an account suspension stop working.",
    "enc", "session issue time against the suspension time, for every session of the account", "toy", "REL", "PARAM+XREL")
row("ACL-04", D, "arithmetic", "Requests per API key per month stay within 1,000,000; requests beyond are throttled.",
    "comb", "used Int(0,1000000) + throttled: 21 bits", "toy", "ARITH", "BITS|DIFF|AGG", "T1")
row("ACL-05", D, "temporal", "At most 100 requests per key in any rolling 60 seconds.",
    "enc", "the timestamps of the last 100 requests", "toy", "TIME", "AGG+TIME")
row("ACL-06", D, "aggregate", "A tenant's stored bytes stay within the plan limit; uploads beyond it are held.",
    "enc", "one registry holding the tenant's files", "toy", "AGG", "AGG+PARAM")
row("ACL-07", D, "aggregate", "An organization always keeps at least one admin; removing the last admin is undone.",
    "enc", "one registry holding the organization's members", "toy", "AGG", "AGG+SET")
row("ACL-08", D, "uniqueness", "Each email address belongs to at most one account.",
    "enc", "key by email; owner register over account ids, the claim carrying the id as a parameter", "yes", "Abstract", "")
row("ACL-09", D, "lifecycle", "Privileged roles take effect only after MFA enrollment.",
    "comb", "per user facts, effective role derived", "yes", "collection")
row("ACL-10", D, "aggregate", "A license allows at most 5 concurrent sessions; extra sessions are queued.",
    "enc", "login and logout counters that only rise, over the license's lifetime", "toy", "ARITH", "DIFF|AGG")
row("ACL-11", D, "lifecycle", "A suspended user has no privileged access, whatever order the suspension and grants arrive in.",
    "comb", "suspension wins in the derived access", "yes", "collection")

D = "Scheduling and capacity"
row("SCH-01", D, "cross-item", "An employee is never on two overlapping shifts.",
    "enc", "one registry holding the employee's shifts and their intervals", "toy", "XITEM", "XREL+PARAM")
row("SCH-02", D, "aggregate", "Class enrollment never exceeds capacity; enrollments beyond it are waitlisted (by count).",
    "enc", "enrolled and dropped counters that only rise per class (8 bits each)", "yes", "collection")
row("SCH-03", D, "cross-item", "When a seat frees up, the earliest waitlisted student gets it.",
    "enc", "one registry holding the class's waitlist", "toy", "XITEM", "AGG+XREL+PARAM")
row("SCH-04", D, "cross-item", "At least 11 hours of rest between an employee's consecutive shifts.",
    "enc", "one registry holding the employee's shifts", "toy", "XITEM", "XREL+PARAM+DIFF")
row("SCH-05", D, "aggregate", "Weekly hours per employee stay within 40 unless overtime is approved.",
    "enc", "one registry holding the week's shifts", "toy", "AGG", "AGG+PARAM")
row("SCH-06", D, "cross-item", "A meeting room holds at most one meeting per 15-minute slot.",
    "enc", "key by (room, slot); holder register over meeting ids, the claim carrying the id as a parameter", "yes", "Abstract", "")
row("SCH-07", D, "aggregate", "Every shift has at least one certified supervisor assigned.",
    "enc", "per shift: certified-assigned and certified-unassigned counters that only rise", "yes", "collection")
row("SCH-08", D, "cross-item", "A pickup slot takes at most 50 orders; overflow rolls to the next slot.",
    "enc", "one registry holding consecutive slots", "toy", "XITEM", "AGG+XREL")
row("SCH-09", D, "uniqueness", "Exactly one doctor is primary on call each day.",
    "enc", "one registry holding the day's claimants", "toy", "REL", "SET+PARAM")
row("SCH-10", D, "cross-item", "Equipment under maintenance is not assigned to jobs during the maintenance interval.",
    "enc", "one registry holding the equipment's jobs and intervals", "toy", "XITEM", "XREL+PARAM")

D = "Multi-service consistency"
row("MSV-01", D, "referential", "The order service shows 'paid' iff the payment service has captured the payment.",
    "comb", "federation: payments is the authority over the order's paid flag", "yes", "federation")
row("MSV-02", D, "referential", "Inventory holds a reservation only while the order service has the order open (no orphan reservations).",
    "comb", "federation: orders is the authority over the reservation's active flag", "yes", "federation")
row("MSV-03", D, "referential", "Trip saga: if the flight, hotel or car leg fails, every confirmed leg is cancelled.",
    "comb", "one registry per trip holding the three legs (orchestrated saga)", "yes", "collection")
row("MSV-04", D, "referential", "A product deleted from the catalog disappears from search and recommendations.",
    "comb", "federation: catalog is the authority over each read model's listed flag", "yes", "federation")
row("MSV-05", D, "guarantee", "Every committed order publishes exactly one OrderPlaced message.",
    "no", "a property of the outbox and transport; gsm states it as an obligation (NotIdempotent)", "no", "OTHER", "none", "N2")
row("MSV-06", D, "referential", "Two-way availability sync between the hotel PMS and an OTA channel manager.",
    "enc", "a federation cycle that is not monotone; BuildCoordinated verifies only the residual", "no", "OTHER", "none")
row("MSV-07", D, "referential", "A GDPR erasure recorded in the identity service reaches CRM and marketing.",
    "comb", "federation: identity is the authority over each target's erased flag", "yes", "federation", "", "V6")
row("MSV-08", D, "uniqueness", "An external order id maps to at most one internal order across shards.",
    "enc", "key by external id; owner register over internal ids, the claim carrying the id as a parameter", "yes", "Abstract", "")
row("MSV-09", D, "temporal", "The price charged equals the price shown at checkout.",
    "enc", "the checkout carries the price it showed, and the charge is derived from it (a closure reading the catalog is frozen at build time, T6); one checkout per order, causally ordered", "yes", "Abstract", "", "V10")
row("MSV-10", D, "lifecycle", "Raising the per-guest hold limit from 5 to 10 while holds are in flight keeps every replica converging.",
    "comb", "CheckMigration on the per-entity registry: SAFE ONLINE", "yes", "CheckMigration")
row("MSV-11", D, "temporal", "The latest address update wins in every service.",
    "enc", "address version and update time as a last-writer-wins register, carried by the update", "yes", "Abstract", "", "V9")
row("MSV-12", D, "guarantee", "Invoice numbers are sequential and gap-free across the company.",
    "no", "a number once issued must never change: needs coordination", "no", "OTHER", "none")
row("MSV-13", D, "referential", "The PMS's room status drives the front desk's assignable flag and housekeeping's task list.",
    "comb", "federation: PMS is the authority over both projections", "yes", "federation")
row("MSV-14", D, "referential", "Fulfillment releases an order only if the payment service's authorized amount covers the order total.",
    "enc", "amounts fixed at creation, compared only, but in two registries; a federation refuses Abstract components", "toy", "OTHER", "COMB")
row("MSV-15", D, "referential", "Marketing drops a queued campaign once the identity service records an erasure.",
    "comb", "M1 refuses the reacting target in a federation; one registry per customer holding both facts", "yes", "collection", "", "V6")
