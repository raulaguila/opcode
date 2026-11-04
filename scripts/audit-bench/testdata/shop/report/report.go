package report

import (
	"fmt"

	"shop/inventory"
)

// Line prints one product of the report.
func Line(catalog *inventory.Catalog, id string) string {
	product := catalog.Find(id)
	return fmt.Sprintf("%s costs %d", product.Name, product.Cost)
}

// Unit prints the cost per unit, or nothing for an empty batch.
func Unit(cost, units int) string {
	if units == 0 {
		return ""
	}
	return fmt.Sprint(inventory.Ratio(cost, units))
}
