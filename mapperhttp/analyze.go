package mapperhttp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/peacewalker122/mapper/mapper"
)

const multipartMemory = 32 << 20

// AnalyzeRequest identifies a stored file for analysis.
type AnalyzeRequest struct {
	FileID mapper.FileID `json:"file_id"`
	ID     mapper.FileID `json:"id,omitempty"`
}

// UnmarshalJSON also accepts a bare file ID string for small clients.
func (request *AnalyzeRequest) UnmarshalJSON(data []byte) error {
	var id string
	if err := json.Unmarshal(data, &id); err == nil {
		request.FileID = mapper.FileID(id)
		return nil
	}
	type payload AnalyzeRequest
	var value payload
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	*request = AnalyzeRequest(value)
	return nil
}

func (handler *Handler) handleAnalyze(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	if handler == nil || handler.service == nil {
		writeErrorStatus(writer, http.StatusServiceUnavailable, "service_unavailable", "mapper service is unavailable")
		return
	}

	contentType, err := mediaType(request)
	if err != nil {
		writeErrorStatus(writer, http.StatusUnsupportedMediaType, "unsupported_media_type", "invalid Content-Type")
		return
	}

	var fileID mapper.FileID
	switch {
	case contentType == "multipart/form-data":
		fileID, err = handler.multipartFileID(request)
	case isJSONMediaType(contentType):
		var payload AnalyzeRequest
		err = decodeJSON(request.Body, &payload)
		if err == nil {
			fileID = payload.FileID
			if !fileID.Valid() {
				fileID = payload.ID
			}
		}
	default:
		writeErrorStatus(writer, http.StatusUnsupportedMediaType, "unsupported_media_type", "files/analyze accepts JSON or multipart/form-data")
		return
	}
	if err != nil {
		writeError(writer, err)
		return
	}
	if !fileID.Valid() {
		writeErrorStatus(writer, http.StatusBadRequest, "invalid_file_id", "file_id is required")
		return
	}

	analysis, err := handler.service.AnalyzeFile(request.Context(), fileID)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, analysis)
}

func (handler *Handler) multipartFileID(request *http.Request) (mapper.FileID, error) {
	if err := request.ParseMultipartForm(multipartMemory); err != nil {
		return "", requestError("invalid multipart form: " + err.Error())
	}
	for _, name := range []string{"file_id", "id"} {
		if value := strings.TrimSpace(request.FormValue(name)); value != "" {
			return mapper.FileID(value), nil
		}
	}

	file, header, fileErr := request.FormFile("file")
	if errors.Is(fileErr, http.ErrMissingFile) {
		file, header, fileErr = request.FormFile("upload")
	}
	if errors.Is(fileErr, http.ErrMissingFile) {
		file, header, fileErr = request.FormFile("source")
	}
	if fileErr != nil {
		if errors.Is(fileErr, http.ErrMissingFile) {
			return "", requestError("multipart form requires file_id or file upload")
		}
		return "", requestError("invalid upload: " + fileErr.Error())
	}
	if handler.writer == nil {
		_ = file.Close()
		return "", mapper.ErrFileStoreUnavailable
	}

	filename := header.Filename
	metadata := mapper.FileMetadata{
		Name:        filename,
		Filename:    filename,
		ContentType: header.Header.Get("Content-Type"),
		Extension:   filepath.Ext(filename),
	}
	saved, saveErr := handler.writer.Save(request.Context(), metadata, file)
	closeErr := file.Close()
	if saveErr == nil {
		saveErr = closeErr
	}
	if saveErr != nil {
		return "", fmt.Errorf("save uploaded file: %w", saveErr)
	}
	if !saved.ID.Valid() {
		return "", fmt.Errorf("%w: file writer returned empty file id", mapper.ErrFileStoreUnavailable)
	}
	return saved.ID, nil
}
