package compiler

import (
	"fmt"

	"github.com/peacewalker122/mapper/idgen"
	"github.com/peacewalker122/mapper/ir"
)

func toIRFieldType(t string) (ir.FieldType, error) {
	switch t {
	case "string":
		return ir.TypeString, nil
	case "integer":
		return ir.TypeInteger, nil
	case "decimal":
		return ir.TypeDecimal, nil
	case "boolean":
		return ir.TypeBoolean, nil
	case "datetime":
		return ir.TypeDateTime, nil
	default:
		return "", fmt.Errorf("unsupported type %q", t)
	}
}

func ResolveIDs(parsed ParsedSchema, lock *LockFile, gen idgen.IDGenerator) (ir.Schema, *LockFile, error) {
	if gen == nil {
		return ir.Schema{}, nil, fmt.Errorf("nil ID generator")
	}
	// Normalize parsed -> IR without IDs first (validate types).
	fields := make([]ir.Field, 0, len(parsed.Model.Fields))
	for _, pf := range parsed.Model.Fields {
		ft, err := toIRFieldType(pf.Type)
		if err != nil {
			return ir.Schema{}, nil, err
		}
		fields = append(fields, ir.Field{Name: pf.Name, Type: ft, Required: pf.Required})
	}

	// Prepare lock.
	var out *LockFile
	if lock == nil {
		out = &LockFile{Version: 1, Fields: map[string]LockField{}}
	} else {
		// Deep copy to avoid mutating caller.
		out = &LockFile{Version: lock.Version, Schema: lock.Schema, Fields: map[string]LockField{}}
		for k, v := range lock.Fields {
			out.Fields[k] = v
		}
		if out.Fields == nil {
			out.Fields = map[string]LockField{}
		}
	}
	if out.Version == 0 {
		out.Version = 1
	}

	// Validate existing lock IDs.
	seenIDs := map[uint64]string{}
	if out.Schema.ID != 0 {
		if err := idgen.ValidateID(out.Schema.ID); err != nil {
			return ir.Schema{}, nil, fmt.Errorf("lock schema id invalid: %w", err)
		}
		seenIDs[out.Schema.ID] = "__schema__"
	}
	for name, lf := range out.Fields {
		if err := idgen.ValidateID(lf.ID); err != nil {
			return ir.Schema{}, nil, fmt.Errorf("lock field %q id invalid: %w", name, err)
		}
		if lf.Status != FieldActive && lf.Status != FieldRemoved {
			return ir.Schema{}, nil, fmt.Errorf("lock field %q: invalid status %q", name, lf.Status)
		}
		if prev, ok := seenIDs[lf.ID]; ok {
			return ir.Schema{}, nil, fmt.Errorf("lock file: duplicate id %d for %q and %q", lf.ID, prev, name)
		}
		seenIDs[lf.ID] = name
	}

	// Resolve schema ID: reuse if lock exists and name matches, else generate.
	schemaID := out.Schema.ID
	schemaName := out.Schema.Name
	if schemaID == 0 || schemaName != parsed.Model.Name {
		// If name changed, treat as new schema? MVP: keep existing ID only if name matches,
		// otherwise generate new ID (old lock fields remain but schema identity changes).
		// To preserve stability for same name, reuse; for rename, new ID.
		if schemaName == parsed.Model.Name && schemaID != 0 {
			// reuse
		} else {
			newID, err := nextUnique(gen, seenIDs)
			if err != nil {
				return ir.Schema{}, nil, err
			}
			schemaID = newID
			seenIDs[schemaID] = "__schema__"
		}
	}
	out.Schema = LockSchema{ID: schemaID, Name: parsed.Model.Name}

	// Mark removed: any active lock field not in parsed becomes removed.
	parsedSet := map[string]ParsedField{}
	for _, pf := range parsed.Model.Fields {
		parsedSet[pf.Name] = pf
	}
	for name, lf := range out.Fields {
		if _, ok := parsedSet[name]; !ok && lf.Status == FieldActive {
			lf.Status = FieldRemoved
			out.Fields[name] = lf
		}
	}

	// Resolve field IDs in parsed order.
	resolved := make([]ir.Field, 0, len(fields))
	for _, f := range fields {
		if lf, ok := out.Fields[f.Name]; ok {
			// Reuse original ID, reactivate if removed.
			f.ID = lf.ID
			if lf.Status == FieldRemoved {
				lf.Status = FieldActive
				out.Fields[f.Name] = lf
			}
		} else {
			newID, err := nextUnique(gen, seenIDs)
			if err != nil {
				return ir.Schema{}, nil, err
			}
			f.ID = newID
			seenIDs[newID] = f.Name
			out.Fields[f.Name] = LockField{ID: newID, Status: FieldActive}
		}
		resolved = append(resolved, f)
	}

	schema := ir.Schema{Version: parsed.Version, Model: ir.Model{ID: schemaID, Name: parsed.Model.Name, Fields: resolved}}
	return schema, out, nil
}

func nextUnique(gen idgen.IDGenerator, seen map[uint64]string) (uint64, error) {
	for i := 0; i < 1000; i++ {
		id, err := gen.Next()
		if err != nil {
			return 0, err
		}
		if err := idgen.ValidateID(id); err != nil {
			continue
		}
		if _, ok := seen[id]; !ok {
			return id, nil
		}
	}
	return 0, fmt.Errorf("failed to generate unique ID after 1000 attempts")
}
