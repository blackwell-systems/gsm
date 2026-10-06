package gsm_test

import (
	"fmt"
	"math/rand"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/blackwell-systems/gsm"
)

// Tests for keyed collections (Collection, roadmap item 1a), against
// normalization-confluence coq/SymmetryCutoff.v.

type productID string

// inventoryItem holds the variables of one product, as declared by declareInventoryItem.
type inventoryItem struct {
	stock, reserved, released, backordered, discontinued gsm.Var
}

// declareInventoryItem declares one product of normalization-confluence's inventory
// example (inventory_item_un) on r, with every name prefixed: stock, reservations and
// releases are counters that the events Restock, Reserve and Release increment, and the
// invariant "backordered iff reservations exceed stock plus releases" is kept by a repair
// that recomputes the flag. A Reserve with no stock leaves the flag stale, so the repair
// fires (inventory_repair_fires). discontinue, an idempotent event, sets a flag.
func declareInventoryItem(r *gsm.Registry, prefix string) inventoryItem {
	it := inventoryItem{
		stock:        r.Int(prefix+"stock", 0, 3),
		reserved:     r.Int(prefix+"reserved", 0, 3),
		released:     r.Int(prefix+"released", 0, 3),
		backordered:  r.Bool(prefix + "backordered"),
		discontinued: r.Bool(prefix + "discontinued"),
	}
	short := func(s gsm.State) bool {
		return s.GetInt(it.reserved) > s.GetInt(it.stock)+s.GetInt(it.released)
	}
	r.Invariant(prefix+"backorder_flag").
		Watches(it.stock, it.reserved, it.released, it.backordered).
		Holds(func(s gsm.State) bool { return s.GetBool(it.backordered) == short(s) }).
		Repair(func(s gsm.State) gsm.State { return s.SetBool(it.backordered, short(s)) }).
		Add()
	inc := func(v gsm.Var) gsm.EffectFunc {
		return func(s gsm.State) gsm.State { return s.SetInt(v, s.GetInt(v)+1) }
	}
	r.Event(prefix + "restock").Writes(it.stock).Apply(inc(it.stock)).Add()
	r.Event(prefix + "reserve").Writes(it.reserved).Apply(inc(it.reserved)).Add()
	r.Event(prefix + "release").Writes(it.released).Apply(inc(it.released)).Add()
	r.Event(prefix + "discontinue").Writes(it.discontinued).
		Apply(func(s gsm.State) gsm.State { return s.SetBool(it.discontinued, true) }).Add()
	return it
}

var inventoryEvents = []string{"restock", "reserve", "release", "discontinue"}

func buildInventory(t *testing.T) (*gsm.CollectionMachine[productID], *gsm.Report, inventoryItem) {
	t.Helper()
	tmpl := gsm.NewRegistry("inventory")
	it := declareInventoryItem(tmpl, "")
	c, rep, err := gsm.NewCollection[productID]("ProductID", tmpl).Build()
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	return c, rep, it
}

// keyedEvent is one event occurrence addressed to a key.
type keyedEvent struct {
	key   productID
	event string
}

func randomDelivery(rng *rand.Rand, keys, n int) []keyedEvent {
	out := make([]keyedEvent, n)
	for i := range out {
		out[i] = keyedEvent{productID(fmt.Sprintf("p%02d", rng.Intn(keys))), inventoryEvents[rng.Intn(len(inventoryEvents))]}
	}
	return out
}

func runCollection(c *gsm.CollectionMachine[productID], d []keyedEvent) *gsm.CollectionState[productID] {
	s := c.NewState()
	for _, x := range d {
		c.Apply(s, x.key, x.event)
	}
	return s
}

// sameItems reports the first key at which a and b differ, over the keys either has.
func sameItems(a, b *gsm.CollectionState[productID]) (productID, bool) {
	keys := append(a.Keys(), b.Keys()...)
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, k := range keys {
		if a.Item(k).ID() != b.Item(k).ID() {
			return k, false
		}
	}
	return "", true
}

func TestCollection_InventoryVerifiedByTemplate(t *testing.T) {
	c, rep, _ := buildInventory(t)
	if rep.Symmetry == nil || *rep.Symmetry != (gsm.SymmetryReduction{Over: "ProductID", Cutoff: 1}) {
		t.Fatalf("Report.Symmetry = %+v, want {ProductID 1}", rep.Symmetry)
	}
	if !rep.WFC || !rep.CC || rep.Assurance == gsm.AssuranceNone {
		t.Fatalf("template not certified:\n%s", rep)
	}
	out := rep.String()
	for _, want := range []string{
		"  Convergence: GUARANTEED\n  Verified by symmetry over ProductID (items independent; cutoff 1)\n",
		"Delivery: exactly once per ProductID for restock, reserve, release (",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
	// The collection runs the template's machine: the same report as the template alone,
	// apart from the symmetry line.
	tmpl := gsm.NewRegistry("inventory")
	declareInventoryItem(tmpl, "")
	_, alone, err := tmpl.Build()
	if err != nil {
		t.Fatal(err)
	}
	if alone.Symmetry != nil || strings.Contains(alone.String(), "Verified by symmetry") {
		t.Errorf("a plain Build reports a symmetry reduction:\n%s", alone)
	}
	if alone.StateCount != rep.StateCount || !reflect.DeepEqual(alone.NotIdempotent, rep.NotIdempotent) {
		t.Errorf("collection report differs from the template's:\n%s\nvs\n%s", rep, alone)
	}
	if c.Over() != "ProductID" || !reflect.DeepEqual(c.Item().Events(), inventoryEvents) {
		t.Errorf("Over = %q, Item().Events() = %v", c.Over(), c.Item().Events())
	}
}

// Every interleaving of one delivery across many keys reaches the same collection, and
// each key's item is the template's run of the events addressed to that key (run_proj).
func TestCollection_RandomInterleavingsAgree(t *testing.T) {
	c, _, it := buildInventory(t)
	rng := rand.New(rand.NewSource(1))
	for trial := 0; trial < 40; trial++ {
		d := randomDelivery(rng, 25, 120)
		ref := runCollection(c, d)
		for perm := 0; perm < 25; perm++ {
			p := append([]keyedEvent(nil), d...)
			rng.Shuffle(len(p), func(i, j int) { p[i], p[j] = p[j], p[i] })
			if k, ok := sameItems(ref, runCollection(c, p)); !ok {
				t.Fatalf("trial %d: two orders of one delivery disagree at %s", trial, k)
			}
		}
		perKey := map[productID]gsm.State{}
		for _, x := range d {
			s, ok := perKey[x.key]
			if !ok {
				s = c.Item().NewState()
			}
			perKey[x.key] = c.Item().Apply(s, x.event)
		}
		if ref.Len() != len(perKey) {
			t.Fatalf("collection has %d keys, the delivery addresses %d", ref.Len(), len(perKey))
		}
		for k, s := range perKey {
			got := ref.Item(k)
			if got.ID() != s.ID() {
				t.Fatalf("key %s: collection %s, template run %s", k, got, s)
			}
			short := got.GetInt(it.reserved) > got.GetInt(it.stock)+got.GetInt(it.released)
			if got.GetBool(it.backordered) != short || !c.Item().IsValid(got) {
				t.Fatalf("key %s: invalid item %s", k, got)
			}
		}
	}
}

// Events on different keys commute from every collection state the run reaches
// (cross_commute): no condition relates two items.
func TestCollection_CrossKeyEventsCommute(t *testing.T) {
	c, _, _ := buildInventory(t)
	rng := rand.New(rand.NewSource(2))
	for trial := 0; trial < 500; trial++ {
		s := runCollection(c, randomDelivery(rng, 4, rng.Intn(30)))
		k1 := productID(fmt.Sprintf("p%02d", rng.Intn(4)))
		k2 := productID(fmt.Sprintf("p%02d", rng.Intn(4)))
		if k1 == k2 {
			continue
		}
		e1, e2 := inventoryEvents[rng.Intn(len(inventoryEvents))], inventoryEvents[rng.Intn(len(inventoryEvents))]
		a, b := s.Clone(), s.Clone()
		c.Apply(a, k1, e1)
		c.Apply(a, k2, e2)
		c.Apply(b, k2, e2)
		c.Apply(b, k1, e1)
		if k, ok := sameItems(a, b); !ok {
			t.Fatalf("%s@%s and %s@%s do not commute: they differ at %s", e1, k1, e2, k2, k)
		}
	}
}

// The explicit lift of the template to two products (each product's variables, its own
// invariant, and its events) also passes Build, and runs exactly as the collection does:
// checking one item covered the two-item registry, as un_cutoff_uniform and lift_idgov say.
func TestCollection_AgreesWithExplicitTwoItemLift(t *testing.T) {
	c, _, tv := buildInventory(t)
	lift := gsm.NewRegistry("inventory_x2")
	items := []inventoryItem{declareInventoryItem(lift, "p00_"), declareInventoryItem(lift, "p01_")}
	m, rep, err := lift.Build()
	if err != nil {
		t.Fatalf("the explicit two-item lift does not build: %v\n%s", err, rep)
	}
	rng := rand.New(rand.NewSource(3))
	for trial := 0; trial < 300; trial++ {
		d := randomDelivery(rng, 2, rng.Intn(20))
		got := runCollection(c, d)
		s := m.NewState()
		for _, x := range d {
			s = m.Apply(s, string(x.key)+"_"+x.event)
		}
		for i, it := range items {
			item := got.Item(productID(fmt.Sprintf("p%02d", i)))
			want := []int{s.GetInt(it.stock), s.GetInt(it.reserved), s.GetInt(it.released)}
			have := []int{item.GetInt(tv.stock), item.GetInt(tv.reserved), item.GetInt(tv.released)}
			if !reflect.DeepEqual(want, have) || s.GetBool(it.backordered) != item.GetBool(tv.backordered) ||
				s.GetBool(it.discontinued) != item.GetBool(tv.discontinued) {
				t.Fatalf("trial %d, product %d: lift %v, collection %s", trial, i, s, item)
			}
		}
	}
}

// An aggregate across keys cannot be written through a Collection. A template rule sees
// one item's State and nothing else; the closest attempt, a closure reading a captured
// "reserved at the other products" counter, runs only while Build verifies the template,
// so what it read then is frozen into the tables, and at run time no rule runs at all.
// The collection is the lift of what the template computed: each key is capped on its
// own (itemwise_converges), whatever the other keys hold.
func TestCollection_AggregateCannotBeExpressed(t *testing.T) {
	tmpl := gsm.NewRegistry("reservation")
	reserved := tmpl.Int("reserved", 0, 3)
	elsewhere := 0 // what an aggregate would need: the reservations at every other key
	calls := 0
	tmpl.Invariant("total_at_most_one").
		Watches(reserved).
		Holds(func(s gsm.State) bool { calls++; return s.GetInt(reserved)+elsewhere <= 1 }).
		Repair(func(s gsm.State) gsm.State { calls++; return s.SetInt(reserved, s.GetInt(reserved)-1) }).
		Add()
	tmpl.Event("reserve").Writes(reserved).
		Apply(func(s gsm.State) gsm.State { calls++; return s.SetInt(reserved, s.GetInt(reserved)+1) }).Add()
	c, rep, err := gsm.NewCollection[productID]("ProductID", tmpl).Build()
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	if calls == 0 {
		t.Fatal("Build ran no rule")
	}
	built := calls
	s := c.NewState()
	for i := 0; i < 50; i++ {
		elsewhere = i // the outside data changes; the collection does not see it
		c.Apply(s, productID(fmt.Sprintf("p%d", i%5)), "reserve")
	}
	if calls != built {
		t.Errorf("Apply ran rule closures %d times; a collection runs the template's tables only", calls-built)
	}
	for _, k := range s.Keys() {
		if got := s.Item(k).GetInt(reserved); got != 1 {
			t.Errorf("key %s: reserved = %d, want the per-item cap 1", k, got)
		}
	}
}

// The aggregate written by hand, as one registry over two items (aggregate_diverges):
// reserve adds a reservation to one item, the invariant is "total reserved across the two
// items is at most 1", and repair cancels a reservation on every positive item. The same
// rule on one item builds (agg_item_un), and with a per-item invariant it builds at two
// items (itemwise_converges); the aggregate at two items does not, so Build refuses it,
// which is why a Collection admits per-item rules only.
func TestCollection_NaiveAggregateRegistryFailsBuild(t *testing.T) {
	one := gsm.NewRegistry("agg_one")
	r := one.Int("reserved", 0, 3)
	one.Invariant("at_most_one").Watches(r).
		Holds(func(s gsm.State) bool { return s.GetInt(r) <= 1 }).
		Repair(func(s gsm.State) gsm.State { return s.SetInt(r, s.GetInt(r)-1) }).Add()
	one.Event("reserve").Writes(r).Apply(func(s gsm.State) gsm.State { return s.SetInt(r, s.GetInt(r)+1) }).Add()
	if _, rep, err := one.Build(); err != nil {
		t.Fatalf("one item: %v\n%s", err, rep)
	}

	two := func(name string, aggregate bool) (*gsm.Report, error) {
		reg := gsm.NewRegistry(name)
		r0, r1 := reg.Int("reserved0", 0, 3), reg.Int("reserved1", 0, 3)
		pred := func(s gsm.State, v gsm.Var) gsm.State { return s.SetInt(v, s.GetInt(v)-1) }
		if aggregate {
			reg.Invariant("total_at_most_one").Watches(r0, r1).
				Holds(func(s gsm.State) bool { return s.GetInt(r0)+s.GetInt(r1) <= 1 }).
				Repair(func(s gsm.State) gsm.State { return pred(pred(s, r0), r1) }).Add()
		} else {
			for _, v := range []gsm.Var{r0, r1} {
				v := v
				reg.Invariant("at_most_one").Watches(v).
					Holds(func(s gsm.State) bool { return s.GetInt(v) <= 1 }).
					Repair(func(s gsm.State) gsm.State { return pred(s, v) }).Add()
			}
		}
		for i, v := range []gsm.Var{r0, r1} {
			v := v
			reg.Event(fmt.Sprintf("reserve%d", i)).Writes(v).
				Apply(func(s gsm.State) gsm.State { return s.SetInt(v, s.GetInt(v)+1) }).Add()
		}
		_, rep, err := reg.Build()
		return rep, err
	}
	if rep, err := two("itemwise_x2", false); err != nil {
		t.Fatalf("per-item invariant at two items: %v\n%s", err, rep)
	}
	rep, err := two("aggregate_x2", true)
	if err == nil {
		t.Fatalf("the aggregate registry built:\n%s", rep)
	}
	if rep == nil || rep.CCFailure == nil || rep.CCFailure.Event1 != "reserve0" || rep.CCFailure.Event2 != "reserve1" {
		t.Fatalf("want a CC failure on (reserve0, reserve1), got %v\n%s", err, rep)
	}
}

// A template that fails CC makes the collection fail, with the template's witness and no
// symmetry line.
func TestCollection_TemplateCCFailureFailsBuild(t *testing.T) {
	tmpl := gsm.NewRegistry("guarded_shipment")
	paid := tmpl.Bool("paid")
	shipped := tmpl.Bool("shipped")
	tmpl.Event("pay").Writes(paid).Apply(func(s gsm.State) gsm.State { return s.SetBool(paid, true) }).Add()
	tmpl.Event("ship").Writes(shipped).
		Guard(func(s gsm.State) bool { return s.GetBool(paid) }).
		Apply(func(s gsm.State) gsm.State { return s.SetBool(shipped, true) }).Add()

	c, rep, err := gsm.NewCollection[int]("OrderID", tmpl).Build()
	if err == nil || c != nil {
		t.Fatalf("Build accepted a template that fails CC: %v", c)
	}
	if !strings.Contains(err.Error(), `collection over OrderID: template "guarded_shipment" did not build`) ||
		!strings.Contains(err.Error(), "Compensation Commutativity") {
		t.Errorf("error = %v", err)
	}
	if rep == nil || rep.CCFailure == nil || rep.CCFailure.Event1 != "pay" || rep.CCFailure.Event2 != "ship" {
		t.Fatalf("want the template's CC witness (pay, ship), got:\n%s", rep)
	}
	if rep.CCFailure.State.GetBool(paid) || rep.CCFailure.Result1.ID() == rep.CCFailure.Result2.ID() {
		t.Errorf("witness %+v is not the divergence from the unpaid state", *rep.CCFailure)
	}
	if rep.Symmetry != nil || strings.Contains(rep.String(), "Verified by symmetry") {
		t.Errorf("a failed collection reports the symmetry reduction:\n%s", rep)
	}
}

// NotIdempotent carries over per key (idem_reduces, alo_cutoff_exact): an event the
// template lists is not idempotent at every key, and an event it does not list is
// idempotent at every key, from every state the run reaches.
func TestCollection_NotIdempotentCarriesOverPerKey(t *testing.T) {
	c, rep, _ := buildInventory(t)
	listed := map[string]bool{}
	for _, e := range rep.NotIdempotent {
		listed[e] = true
	}
	if !reflect.DeepEqual(rep.NotIdempotent, []string{"restock", "reserve", "release"}) {
		t.Fatalf("NotIdempotent = %v", rep.NotIdempotent)
	}
	rng := rand.New(rand.NewSource(4))
	keys := []productID{"p00", "p01", "p02", "p03", "p04", "p05", "p06", "p07"}
	for _, k := range keys {
		for _, e := range inventoryEvents {
			// A fresh key: the listed events change the item when delivered twice.
			once, twice := c.NewState(), c.NewState()
			c.Apply(once, k, e)
			c.Apply(twice, k, e)
			c.Apply(twice, k, e)
			if differs := once.Item(k).ID() != twice.Item(k).ID(); differs != listed[e] {
				t.Errorf("key %s, event %s: twice differs from once = %v, listed = %v", k, e, differs, listed[e])
			}
		}
	}
	for trial := 0; trial < 300; trial++ {
		s := runCollection(c, randomDelivery(rng, len(keys), rng.Intn(40)))
		k := keys[rng.Intn(len(keys))]
		once := s.Clone()
		c.Apply(once, k, "discontinue")
		twice := once.Clone()
		c.Apply(twice, k, "discontinue")
		if once.Item(k).ID() != twice.Item(k).ID() {
			t.Fatalf("discontinue, which the template does not list, is not idempotent at %s", k)
		}
	}
}

// CausalOrderRequired carries over per key (declared_cutoff): a listed pair must be
// causally ordered when both events address the same key, and commutes across keys.
func TestCollection_CausalOrderRequiredPerKey(t *testing.T) {
	tmpl := gsm.NewRegistry("tier")
	tier := tmpl.Int("tier", 0, 2)
	flag := tmpl.Bool("flagged")
	tmpl.Event("silver").Writes(tier).Apply(func(s gsm.State) gsm.State { return s.SetInt(tier, 1) }).Add()
	tmpl.Event("gold").Writes(tier).Apply(func(s gsm.State) gsm.State { return s.SetInt(tier, 2) }).Add()
	tmpl.Event("flag").Writes(flag).Apply(func(s gsm.State) gsm.State { return s.SetBool(flag, true) }).Add()
	tmpl.Independent("silver", "flag")
	tmpl.Independent("gold", "flag")

	c, rep, err := gsm.NewCollection[string]("CustomerID", tmpl).Build()
	if err != nil {
		t.Fatalf("Build: %v\n%s", err, rep)
	}
	if len(rep.CausalOrderRequired) != 1 || rep.CausalOrderRequired[0].Event1 != "silver" || rep.CausalOrderRequired[0].Event2 != "gold" {
		t.Fatalf("CausalOrderRequired = %v", rep.CausalOrderRequired)
	}
	if want := "under causal delivery of the 1 undeclared pair(s) above on the same CustomerID\n"; !strings.Contains(rep.String(), want) {
		t.Errorf("report lacks %q:\n%s", want, rep)
	}
	run := func(order ...[2]string) *gsm.CollectionState[string] {
		s := c.NewState()
		for _, x := range order {
			c.Apply(s, x[0], x[1])
		}
		return s
	}
	same1 := run([2]string{"a", "silver"}, [2]string{"a", "gold"})
	same2 := run([2]string{"a", "gold"}, [2]string{"a", "silver"})
	if same1.Item("a").GetInt(tier) == same2.Item("a").GetInt(tier) {
		t.Error("the listed pair commutes on one key; the witness says it does not")
	}
	cross1 := run([2]string{"a", "silver"}, [2]string{"b", "gold"})
	cross2 := run([2]string{"b", "gold"}, [2]string{"a", "silver"})
	for _, k := range []string{"a", "b"} {
		if cross1.Item(k).ID() != cross2.Item(k).ID() {
			t.Errorf("the listed pair on different keys does not commute at %s", k)
		}
	}
}

func TestCollection_StateAndInputs(t *testing.T) {
	c, _, it := buildInventory(t)
	s := c.NewState()
	if s.Len() != 0 || len(s.Keys()) != 0 || s.Item("new").ID() != c.Item().NewState().ID() {
		t.Fatal("a new collection is not empty, or an unseen key is not at NewState")
	}
	got := c.Apply(s, "p", "restock")
	if got.GetInt(it.stock) != 1 || s.Item("p").ID() != got.ID() || s.Len() != 1 {
		t.Fatalf("Apply returned %s, item %s", got, s.Item("p"))
	}
	cl := s.Clone()
	c.Apply(cl, "p", "restock")
	c.Apply(cl, "q", "restock")
	if s.Item("p").GetInt(it.stock) != 1 || s.Len() != 1 || cl.Len() != 2 {
		t.Error("Clone shares items with the original")
	}

	mustPanic := func(name, want string, f func()) {
		t.Helper()
		defer func() {
			r := recover()
			if r == nil || !strings.Contains(fmt.Sprint(r), want) {
				t.Errorf("%s: panic %v, want one containing %q", name, r, want)
			}
		}()
		f()
	}
	mustPanic("unknown event", `unknown event "restock_all"`, func() { c.Apply(s, "p", "restock_all") })
	if s.Item("p").GetInt(it.stock) != 1 {
		t.Error("a panicking Apply changed the state")
	}
	mustPanic("nil state", "nil CollectionState", func() { c.Apply(nil, "p", "restock") })
	other, _, _ := buildInventory(t)
	mustPanic("foreign state", "another collection machine", func() { c.Apply(other.NewState(), "p", "restock") })

	tmpl := gsm.NewRegistry("inventory")
	declareInventoryItem(tmpl, "")
	if _, _, err := gsm.NewCollection[productID]("", tmpl).Build(); err == nil || !strings.Contains(err.Error(), "empty key name") {
		t.Errorf("empty key name: %v", err)
	}
	if _, _, err := gsm.NewCollection[productID]("ProductID", nil).Build(); err == nil || !strings.Contains(err.Error(), "nil template") {
		t.Errorf("nil template: %v", err)
	}
}
