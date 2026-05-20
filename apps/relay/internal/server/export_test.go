package server

import "net/http"

// HandlerForTest exposes the HTTP handler chain so external tests can
// exercise routes without binding a TCP port. It is only compiled in test
// builds.
func HandlerForTest(s *Server) http.Handler { return s.routes() }
