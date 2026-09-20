package mapper

// Value is one converted target value. Runtime values are nil, string, int64,
// float64, bool, or time.Time.
type Value any

// Record stores values in target schema field order.
type Record struct {
	Values []Value `json:"values"`
}
