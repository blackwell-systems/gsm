package oracle

import "testing"

// The proof repository's astdemo machines and its fragment cases
// (extraction/tests/cases), through CheckRules.
func TestCheckRulesVerdicts(t *testing.T) {
	good := "(doms 6 6 2)\n(inv (le (var 0) (lit 3)) (do (set 0 (lit 3))))\n(ev (do (set 0 (add (var 0) (lit 1)))))\n(ev (do (set 1 (add (var 1) (lit 1)))))\n(ev (do (set 2 (lit 1))))\n"
	for _, c := range []struct {
		name, machine, pairs string
		want                 RulesVerdict
	}{
		{"convergent", good, "pairs all", RulesCertified},
		{"convergent, declared pairs", good, "pairs 2 0 1 1 2", RulesCertified},
		{"not commuting", "(doms 6 6)\n(inv (le (var 0) (lit 3)) (do (set 0 (lit 3))))\n(ev (do (set 0 (add (var 0) (lit 1))) (set 1 (add (var 1) (lit 1)))))\n(ev (do (set 1 (lit 0))))\n", "pairs all", RulesNotCommuting},
		{"declared pair excludes the conflict", "(doms 2)\n(ev (do (set 0 (lit 1))))\n(ev (do (set 0 (lit 0))))\n", "pairs 0", RulesCertified},
		{"repair never ends", "(doms 2)\n(inv (eq (var 0) (lit 5)) (do (set 0 (sub (lit 1) (var 0)))))\n(ev (do (set 0 (lit 1))))\n", "pairs all", RulesNotTerminating},
		{"literal too large", "(doms 2)\n(ev (do (set 0 (lit 2147483648))))\n", "pairs all", RulesOutsideFragment},
		{"expression can overflow", "(doms 4)\n(ev (do (set 0 (add (lit 2147483647) (var 0)))))\n", "pairs all", RulesOutsideFragment},
		{"negative write into a Bool", "(doms 2 2 2)\n(ev (do (set 0 (sub (var 1) (var 2)))))\n", "pairs all", RulesOutsideFragment},
	} {
		got, err := CheckRules(c.machine, c.pairs)
		if err != nil || got.Verdict != c.want {
			t.Errorf("%s: CheckRules = %+v, %v; want %v", c.name, got, err, c.want)
		}
		if c.want == RulesOutsideFragment && got.Reason == "" {
			t.Errorf("%s: no reason given", c.name)
		}
	}
}

func TestCheckRulesRefusesMalformedInput(t *testing.T) {
	for name, in := range map[string][2]string{
		"no doms":           {"(ev (do (set 0 (lit 1))))", "pairs all"},
		"variable past end": {"(doms 2)\n(ev (do (set 1 (lit 1))))", "pairs all"},
		"unbalanced":        {"(doms 2", "pairs all"},
		"pair past events":  {"(doms 2)\n(ev (do (set 0 (lit 1))))", "pairs 1 0 1"},
		"bad pairs":         {"(doms 2)\n(ev (do (set 0 (lit 1))))", "pairs x"},
	} {
		if got, err := CheckRules(in[0], in[1]); err == nil || got.Verdict != RulesNoVerdict {
			t.Errorf("%s: CheckRules = %+v, %v; want no verdict and an error", name, got, err)
		}
	}
}
