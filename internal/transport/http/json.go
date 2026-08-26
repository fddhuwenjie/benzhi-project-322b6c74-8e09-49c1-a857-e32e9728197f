package http

import (
	"bytes"
	"encoding/json"
	"icecoreacclimationgate/internal/domain"
	"io"
	"net/http"
)

func decode(r *http.Request, v any) bool {
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil {
		return false
	}
	return d.Decode(&struct{}{}) == io.EOF
}
func decodeMap(r *http.Request, v *map[string]any) bool { return decode(r, v) }
func mapDecode(m map[string]any, k string, v any) bool {
	if x, ok := m[k]; ok {
		b, _ := json.Marshal(x)
		d := json.NewDecoder(bytes.NewReader(b))
		d.DisallowUnknownFields()
		return d.Decode(v) == nil
	}
	return false
}
func strVal(m map[string]any, k string) string {
	if x, ok := m[k].(string); ok {
		return x
	}
	return ""
}
func intVal(m map[string]any, k string) (int, bool) {
	if x, ok := m[k].(float64); ok {
		i := int(x)
		return i, float64(i) == x
	}
	return 0, false
}
func write(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeErr(w http.ResponseWriter, status int, code, msg string) {
	write(w, status, map[string]any{"error": map[string]string{"code": code, "message": msg}})
}
func writeDomain(w http.ResponseWriter, e error) {
	status := 400
	code := "INVALID"
	if x, ok := e.(*domain.Error); ok {
		code = x.Code
		if code == "NOT_FOUND" {
			status = 404
		}
		if code == "CONFLICT" {
			status = 409
		}
	}
	writeErr(w, status, code, e.Error())
}
