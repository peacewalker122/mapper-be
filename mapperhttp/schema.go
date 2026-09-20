package mapperhttp

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func (handler *Handler) handleSchema(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	if handler == nil || handler.service == nil {
		writeErrorStatus(writer, http.StatusServiceUnavailable, "service_unavailable", "mapper service is unavailable")
		return
	}

	idText := strings.TrimPrefix(request.URL.Path, "/")
	if idText == "" || strings.Contains(idText, "/") {
		writeErrorStatus(writer, http.StatusBadRequest, "invalid_schema_id", "schema id must be a positive integer")
		return
	}
	id, err := strconv.ParseUint(idText, 10, 64)
	if err != nil || id == 0 {
		writeErrorStatus(writer, http.StatusBadRequest, "invalid_schema_id", fmt.Sprintf("invalid schema id %q", idText))
		return
	}

	schema, err := handler.service.Schema(id)
	if err != nil {
		writeError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, schema)
}

func methodNotAllowed(writer http.ResponseWriter, method string) {
	writer.Header().Set("Allow", method)
	writeErrorStatus(writer, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
}
