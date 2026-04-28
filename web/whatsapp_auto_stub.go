//go:build !windows

package web

import "net/http"

func waAutoUnavailable(w http.ResponseWriter) {
	http.Error(w, "WhatsApp Desktop auto-sender is only available on Windows", http.StatusNotImplemented)
}

func (s *Server) whatsappAutoStart(w http.ResponseWriter, r *http.Request)     { waAutoUnavailable(w) }
func (s *Server) whatsappAutoStatus(w http.ResponseWriter, r *http.Request)    { waAutoUnavailable(w) }
func (s *Server) whatsappAutoPause(w http.ResponseWriter, r *http.Request)     { waAutoUnavailable(w) }
func (s *Server) whatsappAutoResume(w http.ResponseWriter, r *http.Request)    { waAutoUnavailable(w) }
func (s *Server) whatsappAutoStop(w http.ResponseWriter, r *http.Request)      { waAutoUnavailable(w) }
func (s *Server) whatsappAutoCampaigns(w http.ResponseWriter, r *http.Request) { waAutoUnavailable(w) }
