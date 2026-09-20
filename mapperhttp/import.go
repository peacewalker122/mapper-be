package mapperhttp

import (
	"net/http"

	"github.com/peacewalker122/mapper/mapper"
)

func (handler *Handler) handleImport(writer http.ResponseWriter, request *http.Request) {
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
	if !isJSONMediaType(contentType) {
		writeErrorStatus(writer, http.StatusUnsupportedMediaType, "unsupported_media_type", "imports/sync accepts application/json")
		return
	}

	var importRequest mapper.ImportRequest
	if err := decodeJSON(request.Body, &importRequest); err != nil {
		writeError(writer, err)
		return
	}
	result, err := handler.service.Import(request.Context(), importRequest)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, result)
}
