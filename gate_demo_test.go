package gsm_test

import (
	"fmt"

	"github.com/blackwell-systems/gsm"
)

// Example_naivePayShip is a deliberately non-convergent machine (ship guarded on
// paid, which pay writes), listed in the catalog as certified to show the gate
// failing. Removed once the failure is recorded.
func Example_naivePayShip() {
	r := gsm.NewRegistry("naive_pay_ship")
	status := r.Enum("status", "pending", "paid", "shipped")
	paid := r.Bool("paid")
	r.Rule("no_ship_unpaid").
		Require(gsm.Or(gsm.IsNotLabel(status, "shipped"), gsm.Is(paid, 1))).
		RepairWith(gsm.SetLabel(status, "pending")).
		Add()
	r.On("pay").Does(append(gsm.SetLabel(status, "paid"), gsm.Raise(paid)...)).Add()
	r.On("ship").OnlyIf(gsm.Is(paid, 1)).Does(gsm.SetLabel(status, "shipped")).Add()
	_, _, err := r.Build()
	fmt.Println("rejected:", err != nil)
	// Output: rejected: true
}
