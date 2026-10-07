package gsm

import (
	"fmt"
	"sort"
	"strings"
)

// RegimeSummary is the regime report: one summary that names the configuration a report
// describes and states its contract in four parts. Build, BuildCompositional,
// Collection.Build, Federation.Build, BuildCoordinated and CheckMigration set it
// (Report.Regime, FedReport.Regime, MigrationReport.Regime); the report's String prints it
// as a block.
//
// It is a reporting layer over what the build already computed: it adds no check and
// changes no result. Every part is derived from what was built and declared, never
// guessed:
//
//   - Regime names the configuration: one registry or a federation, how it was checked
//     (globally, per footprint component, by abstraction, as a collection template), the
//     declared Independent pairs, cycles, coordination, and the projection opt-in.
//   - Guaranteed lists only what the checks that ran certified, each line with the
//     normalization-confluence theorems it rests on (RegimeLine.Theorems, names gated in
//     that repository's coq/verify.sh).
//   - MustProvide lists what the guarantee assumes of the deployment: the obligations the
//     report computes (NotIdempotent, CausalOrderRequired, coordinated inputs) and the
//     deployment rules of the regime. gsm cannot see the transport, so a delivery rule is
//     stated conditionally ("if your transport can redeliver: ...").
//   - NotCovered lists what lies outside the mechanized model for this configuration, each
//     with a pointer (RegimeLine.Ref) to the doc section or the open gap in
//     normalization-confluence REGIME-AUDIT.md.
//
// When a claim is not certified for the configuration it is omitted from Guaranteed, or
// listed under NotCovered; the summary never implies more than is proved. As with the rest
// of a report, parse the fields, not the text.
type RegimeSummary struct {
	// Regime describes the configuration, one item per aspect.
	Regime []string
	// Guaranteed: what the checks that ran certified, each with its theorems.
	Guaranteed []RegimeLine
	// MustProvide: what the guarantee assumes of the deployment.
	MustProvide []RegimeLine
	// NotCovered: what lies outside the mechanized model for this configuration.
	NotCovered []RegimeLine
}

// RegimeLine is one item of a RegimeSummary.
type RegimeLine struct {
	// Text states the item.
	Text string
	// Theorems names the normalization-confluence theorems the item rests on. Every
	// Guaranteed line has at least one.
	Theorems []string
	// Ref points to where the item is explained: a docs section of gsm, or an open gap
	// in normalization-confluence REGIME-AUDIT.md. Empty when the theorems say enough.
	Ref string
}

// String renders the line as the report prints it: the text, then the theorems and the
// pointer in parentheses.
func (l RegimeLine) String() string {
	var cite []string
	if len(l.Theorems) > 0 {
		cite = append(cite, strings.Join(l.Theorems, ", "))
	}
	if l.Ref != "" {
		cite = append(cite, "see "+l.Ref)
	}
	if len(cite) == 0 {
		return l.Text
	}
	return l.Text + " (" + strings.Join(cite, "; ") + ")"
}

// String renders the summary as a block of four labeled parts, each item on its own
// indented line; an empty part reads "none".
func (s *RegimeSummary) String() string {
	if s == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Regime: %s\n", strings.Join(s.Regime, "; "))
	part := func(label string, lines []RegimeLine) {
		if len(lines) == 0 {
			fmt.Fprintf(&b, "%s: none\n", label)
			return
		}
		fmt.Fprintf(&b, "%s:\n", label)
		for _, l := range lines {
			fmt.Fprintf(&b, "  %s\n", l)
		}
	}
	part("Guaranteed", s.Guaranteed)
	part("You must provide", s.MustProvide)
	part("Not covered", s.NotCovered)
	return b.String()
}

// Theorems returns every theorem the summary cites, sorted and without repeats.
func (s *RegimeSummary) Theorems() []string {
	if s == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, part := range [][]RegimeLine{s.Guaranteed, s.MustProvide, s.NotCovered} {
		for _, l := range part {
			for _, t := range l.Theorems {
				if !seen[t] {
					seen[t] = true
					out = append(out, t)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

// indentBlock prefixes every line of text with prefix.
func indentBlock(text, prefix string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(text, "\n"), "\n") {
		b.WriteString(prefix + line + "\n")
	}
	return b.String()
}

// Pointers used by more than one summary.
const (
	refMigration      = "docs/deployment.md#changing-a-running-system"
	refProjection     = "docs/deployment.md#projection-deployments"
	refCycles         = "docs/deployment.md#cycles-ghosts-and-reset-epochs"
	refCoordination   = "docs/federation.md#escape-hatch-3-coordinate-the-obstruction"
	refBuildComp      = "docs/verification.md#buildcompositional"
	refAbsArithmetic  = "docs/ROADMAP.md#1b-abstraction-check-relationships-not-values"
	refAudit          = "normalization-confluence REGIME-AUDIT.md"
	refGap20Delivery  = refAudit + " gap 20, residue (b)"
	refGap20Projected = refAudit + " gap 20, residue (a)"
	refGap21Multi     = refAudit + " gap 21, residue (a)"
	refGap21Cycles    = refAudit + " gap 21, residue (b)"
)

// setRegime sets r.Regime when the build returned a machine (the report is certified),
// and clears it otherwise. Only the public entry points call it, after every field the
// summary reads is final; a federation component's report keeps a nil Regime, since the
// federation's summary covers it.
func (r *Report) setRegime(built bool) {
	if r == nil {
		return
	}
	r.Regime = nil
	if built && r.certified() {
		r.Regime = r.regimeSummary()
	}
}

// certified reports whether the report describes a machine gsm certified: WFC and CC
// passed and an assurance level was recorded, with no failure or refusal.
func (r *Report) certified() bool {
	return r.WFC && r.CC && r.Assurance != AssuranceNone && r.OracleDisagreement == "" &&
		r.DomainViolation == "" && r.FootprintViolation == "" && r.AbstractionRefused == ""
}

// regimeSummary derives the summary of a certified single-registry report.
func (r *Report) regimeSummary() *RegimeSummary {
	s := &RegimeSummary{}
	coll := r.Symmetry != nil
	declared := r.PairsUndeclared > 0
	causal := declared && len(r.CausalOrderRequired) > 0
	tested := r.Compositional != nil && !r.Compositional.FootprintsExact
	exactComp := r.Compositional != nil && r.Compositional.FootprintsExact
	// allCommute: every pair of events commutes where Build checked it, by the path's own
	// transfer. Under abstraction gsm states no transfer for the undeclared pairs it checks,
	// so a declared-only abstraction is not counted.
	allCommute := !declared || (!causal && r.Abstraction == nil)

	// Regime.
	if coll {
		s.Regime = append(s.Regime, fmt.Sprintf("keyed collection over %s (template verified on one item)", r.Symmetry.Over))
	} else {
		s.Regime = append(s.Regime, "single registry")
	}
	switch {
	case r.Abstraction != nil:
		s.Regime = append(s.Regime, fmt.Sprintf("verified by abstraction over %s", strings.Join(r.Abstraction.Over, ", ")))
	case tested:
		s.Regime = append(s.Regime, fmt.Sprintf("checked per footprint component (%d), closure footprints tested",
			r.Compositional.Components))
	case exactComp:
		s.Regime = append(s.Regime, fmt.Sprintf("checked per footprint component (%d)", r.Compositional.Components))
	default:
		s.Regime = append(s.Regime, "checked globally")
	}
	if declared {
		s.Regime = append(s.Regime, fmt.Sprintf("declared Independent pairs (%d undeclared pair(s), %d need causal order)",
			r.PairsUndeclared, len(r.CausalOrderRequired)))
	} else {
		s.Regime = append(s.Regime, "every event pair independent")
	}

	// Guaranteed: the convergence line, by the path that certified it.
	order := "every order of the same events, each applied once, reaches one state"
	if declared {
		order = "sequences that differ only in the order of declared-independent events, each applied once, reach one state"
	}
	var conv RegimeLine
	switch {
	case r.Abstraction != nil:
		conv.Text = order + ", for every value in the declared ranges, NewState included"
		if declared {
			conv.Theorems = []string{"gsm_abs_sound"}
		} else {
			conv.Theorems = []string{"gsm_abs_sound_all"}
		}
	case tested:
		conv.Text = "within each footprint component, on the tables the oracle certified: " + order
		conv.Theorems = []string{"check_tables_converges"}
	case exactComp:
		conv.Text = order + ", from a valid state or NewState"
		if declared {
			conv.Theorems = []string{"cc1_cross", "cc1_same_iff", "run_tequiv", "check_tables_converges"}
		} else {
			conv.Theorems = []string{"compositional_exact", "check_tables_converges"}
		}
	default:
		conv.Text = order + ", from a valid state or NewState"
		if declared {
			conv.Theorems = []string{"check_tables_converges"}
		} else {
			conv.Theorems = []string{"check_tables_converges_all"}
		}
		if r.Assurance == AssuranceOracleTablesAndRules {
			conv.Theorems = append(conv.Theorems, "checkBuild_converges")
		}
	}
	if coll {
		conv.Text = "at every key, for any number of keys: " + conv.Text
		conv.Theorems = append([]string{"run_proj"}, conv.Theorems...)
	}
	s.Guaranteed = append(s.Guaranteed, conv)
	if coll {
		s.Guaranteed = append(s.Guaranteed, RegimeLine{
			Text:     "events on different keys commute, at every state",
			Theorems: []string{"cross_commute"},
		})
	}
	// Declared-only mode in which every undeclared pair commutes too: any order converges.
	// Not claimed under abstraction or tested footprints, where gsm states no transfer for
	// the undeclared pairs it checks.
	if declared && allCommute && !tested {
		thms := []string{"run_tequiv", "perm_tequiv_total"}
		if exactComp {
			thms = append([]string{"cc1_cross", "cc1_same_iff"}, thms...)
		}
		s.Guaranteed = append(s.Guaranteed, RegimeLine{
			Text:     "every undeclared pair commutes too (checked by gsm's Go code, not the oracle), so every order of the same events reaches one state",
			Theorems: thms,
		})
	}
	// At-least-once delivery: every event idempotent where checked and every pair
	// commuting, so duplicates are absorbed.
	if len(r.NotIdempotent) == 0 && allCommute && !tested {
		thms := []string{"alo_exact"}
		switch {
		case coll:
			thms = append(thms, "alo_cutoff")
		case r.Abstraction != nil:
			thms = append(thms, "idem_valid_abs")
		}
		s.Guaranteed = append(s.Guaranteed, RegimeLine{
			Text:     "at-least-once delivery: every event is idempotent where Build checked it, so a redelivered duplicate is absorbed and every at-least-once delivery reaches the exactly-once result",
			Theorems: thms,
		})
	}

	// You must provide.
	perKey, sameKey := "", ""
	if coll {
		perKey = " per " + r.Symmetry.Over
		sameKey = " when both address the same " + r.Symmetry.Over
	}
	if len(r.NotIdempotent) > 0 {
		l := RegimeLine{Text: fmt.Sprintf("if your transport can redeliver: deduplicate %s%s before Apply "+
			"(an event id and a dedupe set); the other events absorb duplicates", strings.Join(r.NotIdempotent, ", "), perKey)}
		switch {
		case tested:
			l.Text = fmt.Sprintf("if your transport can redeliver: deduplicate %s%s before Apply (an event id and a dedupe set)",
				strings.Join(r.NotIdempotent, ", "), perKey)
		case allCommute:
			l.Theorems = []string{"alo_exact"}
		default:
			l.Theorems = []string{"dalo_unlisted_converge"}
		}
		s.MustProvide = append(s.MustProvide, l)
	}
	if causal {
		pairs := make([]string, len(r.CausalOrderRequired))
		for i, f := range r.CausalOrderRequired {
			pairs[i] = fmt.Sprintf("(%s, %s)", f.Event1, f.Event2)
		}
		s.MustProvide = append(s.MustProvide, RegimeLine{
			Text: fmt.Sprintf("causal order for the undeclared pair(s) %s%s: each pair reaches every replica in the same order",
				strings.Join(pairs, ", "), sameKey),
			Theorems: []string{"run_tequiv", "causal_tequiv"},
		})
	}
	if !allCommute {
		s.MustProvide = append(s.MustProvide,
			RegimeLine{
				Text: "if your transport can redeliver: a retry keeps its causal place (it does not overtake an undeclared " +
					"partner, or arrive after an event that causally follows its original); if the transport cannot promise " +
					"that, deduplicate every event",
				Theorems: []string{"fl_retry_order_needed", "late_duplicate_diverges"},
			})
	}
	if tested {
		s.MustProvide = append(s.MustProvide, RegimeLine{
			Text: "every closure reads and writes only its declared footprint (an event's Writes, an invariant's Watches) and is " +
				"deterministic, as TrustClosureFootprints acknowledges",
		})
	}

	// Not covered.
	if tested {
		s.NotCovered = append(s.NotCovered, RegimeLine{
			Text: "the step from the components to the whole machine: closure footprints are tested by perturbation, not " +
				"proved, and a joint dependence on three or more outside variables escapes the test",
			Ref: refBuildComp,
		})
	}
	if r.Abstraction != nil {
		s.NotCovered = append(s.NotCovered, RegimeLine{
			Text: "rules that add or subtract (abstraction's arithmetic route): not implemented, and Abstract refuses them",
			Ref:  refAbsArithmetic,
		})
	}
	switch {
	case coll:
		s.NotCovered = append(s.NotCovered, RegimeLine{
			Text: "changing the template's rules while events are in flight: CheckMigration takes single registries",
			Ref:  refMigration,
		})
	case declared:
		s.NotCovered = append(s.NotCovered, RegimeLine{
			Text: "changing these rules while events are in flight: CheckMigration refuses a registry with Independent pairs; " +
				"declared independence and causal or at-least-once delivery across the switch are open",
			Ref: refMigration + "; " + refGap20Delivery,
		})
	case r.Abstraction != nil:
		s.NotCovered = append(s.NotCovered, RegimeLine{
			Text: "changing these rules while events are in flight: CheckMigration refuses a registry declared with Abstract",
			Ref:  refMigration,
		})
	}
	return s
}

// fedShape records what Federation.Build saw that FedReport's exported fields do not:
// the input to FedReport.Regime.
type fedShape struct {
	cyclic            bool
	multiSource       []string // multi-source targets (with a resolver), in component order
	requireProjection bool
	certified         int // sub-federations embedded with EmbedCertified
}

// setRegime sets r.Regime when Build returned a machine, and clears it otherwise.
func (r *FedReport) setRegime(built bool) {
	if r == nil {
		return
	}
	r.Regime = nil
	if built && r.Assurance != "" {
		r.Regime = r.regimeSummary()
	}
}

// regimeSummary derives the summary of a federation Build accepted.
func (r *FedReport) regimeSummary() *RegimeSummary {
	s := &RegimeSummary{}
	sh := r.shape
	var coordinated []CoordinationPoint
	var declaredOn, notIdem []string
	for _, c := range r.Components {
		if c == nil {
			continue
		}
		coordinated = append(coordinated, c.Coordinated...)
		if c.PairsUndeclared > 0 {
			declaredOn = append(declaredOn, c.Name)
		}
		for _, e := range c.NotIdempotent {
			notIdem = append(notIdem, fmt.Sprintf("%s on %s", e, c.Name))
		}
	}

	// Regime.
	if sh.cyclic {
		s.Regime = append(s.Regime, "federation with monotone cycles (AllowMonotoneCycles)")
	} else {
		s.Regime = append(s.Regime, "acyclic federation")
	}
	s.Regime = append(s.Regime, fmt.Sprintf("%d component(s), %d morphism(s)", len(r.Components), r.Edges))
	if len(sh.multiSource) > 0 {
		s.Regime = append(s.Regime, fmt.Sprintf("multi-source target(s) with a resolver: %s", strings.Join(sh.multiSource, ", ")))
	}
	if len(coordinated) > 0 {
		pts := make([]string, len(coordinated))
		for i, cp := range coordinated {
			pts[i] = fmt.Sprintf("%s (authority %s)", cp, cp.Authority)
		}
		s.Regime = append(s.Regime, "coordinated edges removed: "+strings.Join(pts, ", "))
	}
	if len(declaredOn) > 0 {
		s.Regime = append(s.Regime, "declared Independent pairs on "+strings.Join(declaredOn, ", "))
	}
	if sh.certified > 0 {
		s.Regime = append(s.Regime, fmt.Sprintf("%d certified embed(s)", sh.certified))
	}
	// Projection guarantees are claimed only without coordination: a coordinated input is an
	// external write, outside the projection model that XU certifies for the network's events.
	projection := r.ProjectionSafe && len(coordinated) == 0
	switch {
	case sh.requireProjection && len(coordinated) > 0:
		s.Regime = append(s.Regime, "projection deployment (RequireProjectionSafe), XU on the residual network")
	case sh.requireProjection:
		s.Regime = append(s.Regime, "projection deployment (RequireProjectionSafe)")
	case r.ProjectionSafe && len(coordinated) > 0:
		s.Regime = append(s.Regime, "projection merging certified (XU) on the residual network")
	case r.ProjectionSafe:
		s.Regime = append(s.Regime, "projection merging certified (XU)")
	default:
		s.Regime = append(s.Regime, "projection merging not certified")
	}

	// Guaranteed.
	subject := "the FedMachine (the network's repair after every event)"
	if len(coordinated) > 0 {
		subject = "the residual network (the coordinated edges removed) as a FedMachine"
	}
	if sh.cyclic {
		s.Guaranteed = append(s.Guaranteed,
			RegimeLine{
				Text:     subject + ": the federated normal form is the least fixed point, reached in any order",
				Theorems: []string{"chaotic_reaches_lfp"},
			},
			RegimeLine{
				Text:     subject + ": every interleaving of independent events, each applied once, reaches one federated state",
				Theorems: []string{"cyc_check_gc_lfp"},
			})
	} else {
		s.Guaranteed = append(s.Guaranteed, RegimeLine{
			Text:     subject + ": every interleaving of independent events, each applied once, from a FedMachine state, reaches one federated state",
			Theorems: []string{"fed_events_commute", "static_c1_c2_gc"},
		})
	}
	if projection {
		s.Guaranteed = append(s.Guaranteed,
			RegimeLine{
				Text: "projection deployment (each node runs its component and merges its sources' projections): every " +
					"interleaving of local events and merges reaches the FedMachine run of its events once propagation completes",
				Theorems: []string{"dist_interleavings_converge"},
			},
			RegimeLine{
				Text: "projection deployment over channels that delay, reorder or duplicate projections, merged with " +
					"MergeProjectionAfter under the projection rules below: every run converges once the channels drain",
				Theorems: []string{"vsettle_xu_c2"},
			})
	}

	// You must provide.
	if projection {
		s.MustProvide = append(s.MustProvide,
			RegimeLine{Text: "run the FedMachine itself (for example over a shared, totally ordered log), or a projection deployment that follows the projection rules below"},
			RegimeLine{
				Text:     "projection rules: a send after every source change",
				Theorems: []string{"no_final_send_counterexample"},
			},
			RegimeLine{
				Text: "projection rules, over channels that can reorder or redeliver projections: merge with MergeProjectionAfter " +
					"and stamp versions in send order on the snapshot sent (a retry resends the same snapshot and version); " +
					"plain MergeProjection needs per-edge FIFO channels without redelivery",
				Theorems: []string{"version_order_counterexample", "plain_stale_counterexample"},
			})
	} else {
		s.MustProvide = append(s.MustProvide, RegimeLine{
			Text: "run the FedMachine itself (for example one FedMachine over a shared, totally ordered log): merging " +
				"projections on separate nodes is not certified for this federation",
		})
	}
	redeliver := RegimeLine{Text: "if your transport can redeliver events: deduplicate every event, unless you have checked " +
		"that its federated step (the event, then the network's repair) is idempotent; per-registry NotIdempotent does not check that"}
	if len(notIdem) > 0 {
		redeliver.Text += " (it lists " + strings.Join(notIdem, ", ") + ")"
	}
	if projection {
		redeliver.Theorems = []string{"dist_alo_exact"}
	}
	s.MustProvide = append(s.MustProvide, redeliver)
	for _, c := range r.Components {
		if c == nil || len(c.CausalOrderRequired) == 0 {
			continue
		}
		pairs := make([]string, len(c.CausalOrderRequired))
		for i, f := range c.CausalOrderRequired {
			pairs[i] = fmt.Sprintf("(%s, %s)", f.Event1, f.Event2)
		}
		s.MustProvide = append(s.MustProvide, RegimeLine{
			Text: fmt.Sprintf("causal order on %s for the undeclared pair(s) %s: each pair reaches every replica in the same order",
				c.Name, strings.Join(pairs, ", ")),
		})
	}
	for _, cp := range coordinated {
		s.MustProvide = append(s.MustProvide, RegimeLine{
			Text: fmt.Sprintf("coordinate %s: serialize writes to its shared variables (a single writer, a lock, a consensus "+
				"round) and Normalize %s after each; %s is the authority root of the cycles it breaks, and the normal form "+
				"depends on that choice", cp, cp.Dst, cp.Authority),
			Theorems: []string{"root_choice_matters"},
		})
	}

	// Not covered.
	if sh.cyclic {
		s.NotCovered = append(s.NotCovered, RegimeLine{
			Text: "a projection deployment of this cyclic network: nodes can settle on a ghost, a larger fixed point than the " +
				"FedMachine's; reset epochs and the no-reset conditions are mechanized, not implemented",
			Theorems: []string{"dist_cyc_ghost", "vchan_cyc_ghost"},
			Ref:      refCycles + "; " + refGap21Cycles,
		})
	}
	if len(sh.multiSource) > 0 {
		s.NotCovered = append(s.NotCovered, RegimeLine{
			Text: fmt.Sprintf("a projection deployment with the multi-source target(s) %s: SharedProjection sends one "+
				"edge's image, not the resolver's merge", strings.Join(sh.multiSource, ", ")),
			Ref: refProjection + "; " + refGap21Multi,
		})
	}
	if !r.ProjectionSafe && !sh.cyclic && len(sh.multiSource) == 0 {
		s.NotCovered = append(s.NotCovered, RegimeLine{
			Text: "a projection deployment of this federation: XU is not certified; FedReport.ProjectionLine and " +
				"ProjectionWitnesses say where",
			Ref: refProjection,
		})
	}
	if r.ProjectionSafe && len(coordinated) > 0 {
		s.NotCovered = append(s.NotCovered, RegimeLine{
			Text: "a projection deployment with coordinated inputs: XU holds on the residual network, but a coordinated write " +
				"is not one of the network's events, which is what the projection theorem covers",
			Ref: refProjection,
		})
	}
	if len(coordinated) > 0 {
		s.NotCovered = append(s.NotCovered, RegimeLine{
			Text: "the coordination itself: serializing the coordinated inputs, and their order relative to the network's events",
			Ref:  refCoordination,
		})
	}
	s.NotCovered = append(s.NotCovered, RegimeLine{
		Text: "changing the federation's rules or topology while events are in flight: CheckMigration takes single registries; " +
			"the theory covers a FedMachine switch, which gsm does not implement, and a switch with projections in flight is open",
		Ref: refMigration + "; " + refGap20Projected,
	})
	return s
}

// regimeSummary derives the summary of a migration report.
func (r *MigrationReport) regimeSummary() *RegimeSummary {
	s := &RegimeSummary{}
	s.Regime = []string{
		fmt.Sprintf("change of a single registry, %s -> %s", r.From, r.To),
		"gsm's runtime (each Apply repairs before it returns)",
		"free delivery, each event applied once",
		fmt.Sprintf("checked from %d start(s)", len(r.Starts)),
	}
	barrierThms := func() []string {
		var out []string
		for _, t := range r.Theorems {
			if t != "det_live_exact" {
				out = append(out, t)
			}
		}
		return out
	}
	switch r.Outcome {
	case MigrationSafeOnline:
		s.Guaranteed = []RegimeLine{
			{
				Text: fmt.Sprintf("a switch at any time, with events of %s in flight: every run converges, the same events "+
					"from a checked start reaching one state", r.From),
				Theorems: []string{"det_live_exact", "run_tequiv", "perm_tequiv_total"},
			},
			{
				Text:     "a switch behind a barrier converges too",
				Theorems: []string{"det_live_implies_barrier"},
			},
		}
	case MigrationSafeBehindBarrier:
		s.Guaranteed = []RegimeLine{{
			Text: fmt.Sprintf("a switch after draining, every event submitted under %s applied first: every run converges, "+
				"the same events from a checked start reaching one state; a switch with events in flight can diverge, as "+
				"LiveWitness shows", r.From),
			Theorems: barrierThms(),
		}}
	}
	if r.Outcome == MigrationSafeOnline || r.Outcome == MigrationSafeBehindBarrier {
		s.MustProvide = []RegimeLine{
			{Text: "the system runs from a checked start (the zero state of " + r.From + ", or the states passed with MigrationFrom)"},
			{Text: "migrate every replica's state with the same migration, then Normalize it under " + r.To},
			{Text: "apply an event of " + r.From + " that arrives after the switch as its translation"},
			{Text: "apply each event once, by one configuration"},
		}
		if r.Outcome == MigrationSafeBehindBarrier {
			s.MustProvide = append(s.MustProvide, RegimeLine{
				Text: "drain before switching: every replica applies every event submitted under " + r.From + ", then switches",
			})
		}
	}
	if r.Outcome == MigrationUnknown {
		s.NotCovered = append(s.NotCovered, RegimeLine{
			Text: "this change: the search stopped at its size limit before deciding the barrier, as SearchStopped says",
			Ref:  refMigration,
		})
	}
	s.NotCovered = append(s.NotCovered,
		RegimeLine{Text: "a switch between an event and its repair, which is not gsm's runtime", Ref: refMigration},
		RegimeLine{Text: "declared Independent pairs, causal or at-least-once delivery across the switch", Ref: refGap20Delivery},
		RegimeLine{
			Text: "federations, collections, and projection deployments with propagation in flight",
			Ref:  refMigration + "; " + refGap20Projected,
		})
	return s
}
