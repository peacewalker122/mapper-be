package source

import (
	"errors"

	mapper "github.com/peacewalker122/mapper/mapper"
)

// Keep source contracts shared with mapper's service layer. Aliases preserve
// one type identity, so concrete adapters can be passed directly to mapper.Service.
type FileID = mapper.FileID
type FileMetadata = mapper.FileMetadata
type SourceRow = mapper.SourceRow
type RowReader = mapper.RowReader
type SheetAnalysis = mapper.SheetAnalysis
type SourceAnalysis = mapper.SourceAnalysis
type SourceAdapter = mapper.SourceAdapter

const DefaultSampleLimit = 5

var ErrNoHeader = errors.New("source: no non-empty header row")
