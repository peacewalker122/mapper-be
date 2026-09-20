package executor

import (
	"fmt"

	"github.com/peacewalker122/mapper/mapper"
)

// ExecutionPlan resolves stable target IDs to target slots once, before row
// processing begins.
type ExecutionPlan struct {
	Ops          []MapOp
	TargetFields []mapper.Field
}

// MapOp is one compiled source-index to target-slot operation.
type MapOp struct {
	SourceIndex int
	TargetIndex int
	TargetID    uint64
	TargetType  mapper.FieldType
	Required    bool
}

// CompilePlan validates a mapping and compiles it into a reusable plan. The
// optional source count enables up-front source-index validation when analysis
// already knows the source width.
func CompilePlan(spec mapper.MappingSpec, schema mapper.Schema, sourceCount ...int) (*ExecutionPlan, error) {
	if err := ValidateMapping(spec, schema, sourceCount...); err != nil {
		return nil, err
	}

	fields := append([]mapper.Field(nil), schema.Fields...)
	byID := make(map[uint64]int, len(fields))
	for index, field := range fields {
		byID[field.ID] = index
	}

	ops := make([]MapOp, 0, len(spec.Mappings))
	for _, fieldMapping := range spec.Mappings {
		targetIndex := byID[fieldMapping.Target]
		target := fields[targetIndex]
		ops = append(ops, MapOp{
			SourceIndex: fieldMapping.Source,
			TargetIndex: targetIndex,
			TargetID:    target.ID,
			TargetType:  target.Type,
			Required:    target.Required,
		})
	}
	return &ExecutionPlan{Ops: ops, TargetFields: fields}, nil
}

// Compile is a short alias for CompilePlan.
func Compile(spec mapper.MappingSpec, schema mapper.Schema, sourceCount ...int) (*ExecutionPlan, error) {
	return CompilePlan(spec, schema, sourceCount...)
}

// NewPlan is a constructor-style alias for CompilePlan.
func NewPlan(spec mapper.MappingSpec, schema mapper.Schema, sourceCount ...int) (*ExecutionPlan, error) {
	return CompilePlan(spec, schema, sourceCount...)
}

// Execute maps one source row into target schema order.
func (p *ExecutionPlan) Execute(source []string) (mapper.Record, error) {
	if p == nil {
		return mapper.Record{}, fmt.Errorf("nil execution plan")
	}
	record := mapper.Record{Values: make([]mapper.Value, len(p.TargetFields))}
	for _, op := range p.Ops {
		if op.SourceIndex < 0 || op.SourceIndex >= len(source) {
			index := op.SourceIndex
			target := op.TargetID
			return record, rowError(CodeInvalidSourceIndex, fmt.Sprintf("source index %d is absent from row", index), 0, &index, &target)
		}
		raw := source[op.SourceIndex]
		if raw == "" {
			if op.Required {
				target := op.TargetID
				sourceIndex := op.SourceIndex
				return record, rowError(CodeRequiredValueMissing, "required value is empty", 0, &sourceIndex, &target)
			}
			continue
		}
		value, err := Convert(raw, op.TargetType)
		if err != nil {
			rowErr := rowErrorFrom(err, 0)
			rowErr.SourceIndex = intPointer(op.SourceIndex)
			rowErr.TargetID = uintPointer(op.TargetID)
			return record, rowErr
		}
		record.Values[op.TargetIndex] = value
	}
	return record, nil
}

// Map is a descriptive alias for Execute.
func (p *ExecutionPlan) Map(source []string) (mapper.Record, error) {
	return p.Execute(source)
}

func (p *ExecutionPlan) Apply(source []string) (mapper.Record, error) {
	return p.Execute(source)
}

func Execute(plan *ExecutionPlan, source []string) (mapper.Record, error) {
	if plan == nil {
		return mapper.Record{}, fmt.Errorf("nil execution plan")
	}
	return plan.Execute(source)
}

// ExecuteSource maps a mapper SourceRow when a caller already has one.
func (p *ExecutionPlan) ExecuteSource(source mapper.SourceRow) (mapper.Record, error) {
	return p.Execute(sourceValues(source))
}

func intPointer(value int) *int        { return &value }
func uintPointer(value uint64) *uint64 { return &value }
