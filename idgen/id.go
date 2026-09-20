package idgen

import "fmt"

const MaxID uint64 = 9007199254740991

type IDGenerator interface {
	Next() (uint64, error)
}

func ValidateID(id uint64) error {
	if id == 0 || id > MaxID {
		return fmt.Errorf("id %d out of range [1,%d]", id, MaxID)
	}
	return nil
}

type SequentialIDGenerator struct {
	NextID    uint64
	exhausted bool
}

func NewSequentialIDGenerator(start ...uint64) *SequentialIDGenerator {
	next := uint64(1)
	if len(start) > 0 {
		next = start[0]
	}
	return &SequentialIDGenerator{NextID: next}
}

func (g *SequentialIDGenerator) Next() (uint64, error) {
	if g == nil {
		return 0, fmt.Errorf("nil sequential ID generator")
	}
	if g.exhausted {
		return 0, fmt.Errorf("ID generator exhausted")
	}
	if g.NextID == 0 {
		g.NextID = 1
	}
	if err := ValidateID(g.NextID); err != nil {
		return 0, err
	}
	id := g.NextID
	if id == MaxID {
		g.exhausted = true
		g.NextID = 0
	} else {
		g.NextID++
	}
	return id, nil
}

var _ IDGenerator = (*SequentialIDGenerator)(nil)
