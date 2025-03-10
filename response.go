package router

import "net/http"

// responseWriter is an extension of http.ResponseWriter that tracks the write status of the response.
type responseWriter struct {
	http.ResponseWriter
	written bool
	status  int
}

// writeHeader sets the HTTP status code.
// It does nothing if the response has already been written.
func (rw *responseWriter) writeHeader(code int) {
	if !rw.written {
		rw.status = code
		rw.ResponseWriter.WriteHeader(code)
		rw.written = true
	}
}

// write writes the response body.
// Writing is tracked by setting the written flag.
func (rw *responseWriter) write(b []byte) (int, error) {
	if !rw.written {
		rw.written = true
	}
	return rw.ResponseWriter.Write(b)
}
