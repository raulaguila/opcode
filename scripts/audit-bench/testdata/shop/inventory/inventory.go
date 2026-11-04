package inventory

type Product struct {
	ID   string
	Name string
	Cost int
}

type Catalog struct{ products map[string]*Product }

func New(products map[string]*Product) *Catalog { return &Catalog{products: products} }

// Find returns the product, or nil when there is none.
func (c *Catalog) Find(id string) *Product { return c.products[id] }

// Ratio is cost per unit; callers have already ruled out zero units.
func Ratio(cost, units int) int { return cost / units }
