package knowledge

import "fmt"

// Prices are absolute site USD units per million tokens, before group pricing.
// Input is also the explicitly configured rate for unsplit aggregate usage.
type Prices struct {
	Input      *float64 `json:"input"`
	Image      *float64 `json:"image,omitempty"`
	BatchInput *float64 `json:"batch_input,omitempty"`
}

func (p Prices) Expression() string {
	if p.Input == nil {
		return ""
	}
	if p.Image != nil {
		return fmt.Sprintf("v2:(tier(\"knowledge\", p * %.17g + img * %.17g + c * 0)\n) / 1000000", *p.Input, *p.Image)
	}
	return fmt.Sprintf("v2:(tier(\"knowledge\", p * %.17g + c * 0)\n) / 1000000", *p.Input)
}
