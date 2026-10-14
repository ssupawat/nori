package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

var hexColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// nori defaults.
type Branding struct {
	Name    string `json:"name"`
	Tagline string `json:"tagline"`
	Color   string `json:"color"`
	LogoURL string `json:"logoUrl"`
}

func (b Branding) withDefaults() Branding {
	if b.Name == "" {
		b.Name = "nori"
	}
	if b.Tagline == "" {
		b.Tagline = "import a spreadsheet, get an API"
	}
	if b.Color == "" {
		b.Color = "#18181b"
	}
	return b
}

// updateBranding stores a branding override in the database. Empty fields
// keep the current value; the override wins over -brand-* flags until
// reset. GET /api/branding picks the change up immediately since handlers
// read sv.branding.
func (sv *Server) updateBranding(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name    string `json:"name"`
		Tagline string `json:"tagline"`
		Color   string `json:"color"`
		LogoURL string `json:"logoUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	body.Tagline = strings.TrimSpace(body.Tagline)
	body.Color = strings.TrimSpace(body.Color)
	body.LogoURL = strings.TrimSpace(body.LogoURL)
	if body.Color != "" && !hexColorRe.MatchString(body.Color) {
		writeError(w, http.StatusBadRequest, "color must be a hex value like #2563eb")
		return
	}
	if body.LogoURL != "" && !strings.HasPrefix(body.LogoURL, "http://") && !strings.HasPrefix(body.LogoURL, "https://") {
		writeError(w, http.StatusBadRequest, "logoUrl must be an http(s) URL")
		return
	}
	// full replacement: the form sends every field; empty name/tagline/
	// color fall back to defaults via withDefaults, and an empty logoUrl
	// means "back to the generated art mark"
	vals := map[string]string{
		"name":    body.Name,
		"tagline": body.Tagline,
		"color":   body.Color,
		"logoUrl": body.LogoURL,
	}
	if err := sv.store.SetSetting("branding", vals); err != nil {
		serverError(w, err)
		return
	}
	sv.branding = Branding{
		Name:    body.Name,
		Tagline: body.Tagline,
		Color:   body.Color,
		LogoURL: body.LogoURL,
	}
	writeJSON(w, http.StatusOK, sv.branding.withDefaults())
}

// resetBranding removes the stored override; branding falls back to the
// -brand-* flags the server was started with.
func (sv *Server) resetBranding(w http.ResponseWriter, r *http.Request) {
	if err := sv.store.DeleteSetting("branding"); err != nil {
		serverError(w, err)
		return
	}
	sv.branding = sv.flagBranding
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
