package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"infraflow/pkg/observability"
)

func (handler *Handler) ingestAgentLogs(writer http.ResponseWriter, request *http.Request) {
	var input agentLogRequest
	if err := decodeRequest(writer, request, &input); err != nil {
		return
	}
	result, err := handler.service.IngestAgentLogs(input.AgentID, input.Events)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(writer, http.StatusAccepted, result)
}

func (handler *Handler) listTechnicalLogs(writer http.ResponseWriter, request *http.Request) {
	query, err := parseLogQuery(request)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	page, err := handler.service.Logs(query)
	if err != nil {
		writeLogQueryError(writer, err)
		return
	}
	writeJSON(writer, http.StatusOK, page)
}

func (handler *Handler) streamTechnicalLogs(writer http.ResponseWriter, request *http.Request) {
	query, err := parseLogQuery(request)
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	if cursor := request.Header.Get("Last-Event-ID"); cursor != "" {
		query.After = cursor
	}
	flusher, ok := writer.(http.Flusher)
	if !ok {
		writeError(writer, http.StatusInternalServerError, "streaming is unavailable")
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-cache, no-transform")
	writer.Header().Set("X-Accel-Buffering", "no")
	writer.WriteHeader(http.StatusOK)
	flusher.Flush()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		page, err := handler.service.Logs(query)
		if err != nil {
			if errors.Is(err, observability.ErrCursorExpired) {
				_, _ = fmt.Fprint(writer, "event: reset\ndata: {\"reason\":\"cursor_expired\"}\n\n")
				query.After = ""
				flusher.Flush()
			} else {
				_, _ = fmt.Fprint(writer, "event: error\ndata: {\"reason\":\"log_store_unavailable\"}\n\n")
				flusher.Flush()
				return
			}
		} else {
			for _, event := range page.Events {
				data, err := json.Marshal(event)
				if err != nil {
					continue
				}
				if _, err := fmt.Fprintf(writer, "id: %s\nevent: log\ndata: %s\n\n", event.ID, data); err != nil {
					return
				}
				query.After = event.ID
				flusher.Flush()
			}
		}
		if _, err := fmt.Fprint(writer, ": keep-alive\n\n"); err != nil {
			return
		}
		flusher.Flush()
		select {
		case <-request.Context().Done():
			return
		case <-ticker.C:
		}
	}
}

func parseLogQuery(request *http.Request) (observability.Query, error) {
	values := request.URL.Query()
	query := observability.Query{
		After: values.Get("after"), Before: values.Get("before"),
		Level: values.Get("level"), Service: values.Get("service"),
		Hostname: values.Get("hostname"), SiteID: values.Get("site_id"), AgentID: values.Get("agent_id"),
		RunID: values.Get("run_id"), JobID: values.Get("job_id"), TaskID: values.Get("task_id"),
		Text: values.Get("q"), Limit: 100,
	}
	if rawLimit := values.Get("limit"); rawLimit != "" {
		limit, err := strconv.Atoi(rawLimit)
		if err != nil || limit < 1 || limit > observability.MaxQueryLimit {
			return query, fmt.Errorf("limit must be between 1 and %d", observability.MaxQueryLimit)
		}
		query.Limit = limit
	}
	if rawSince := values.Get("since"); rawSince != "" {
		query.Since, _ = time.Parse(time.RFC3339Nano, rawSince)
		if query.Since.IsZero() {
			return query, fmt.Errorf("since must be RFC3339")
		}
	}
	if rawUntil := values.Get("until"); rawUntil != "" {
		query.Until, _ = time.Parse(time.RFC3339Nano, rawUntil)
		if query.Until.IsZero() {
			return query, fmt.Errorf("until must be RFC3339")
		}
	}
	if query.Level != "" {
		switch strings.ToUpper(query.Level) {
		case "DEBUG", "INFO", "WARN", "ERROR":
		default:
			return query, fmt.Errorf("level must be DEBUG, INFO, WARN, or ERROR")
		}
	}
	return query, nil
}

func writeLogQueryError(writer http.ResponseWriter, err error) {
	if errors.Is(err, observability.ErrCursorExpired) {
		writeError(writer, http.StatusGone, "log cursor is no longer retained; request the latest page")
		return
	}
	writeError(writer, http.StatusInternalServerError, "technical logs are unavailable")
}
