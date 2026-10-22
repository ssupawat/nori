package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"nori/internal/auth"
)

func (sv *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	users, err := sv.auth.ListUsers()
	if err != nil {
		serverError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (sv *Server) createUser(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"` // optional: generated when absent
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	generated := ""
	if body.Password == "" {
		pw, err := auth.NewRandomPassword()
		if err != nil {
			serverError(w, err)
			return
		}
		body.Password, generated = pw, pw
	}
	err := sv.auth.CreateUser(body.Username, body.Password, auth.Role(body.Role))
	switch {
	case err == nil:
	case errors.Is(err, auth.ErrUserExists):
		writeError(w, http.StatusConflict, err.Error())
		return
	case errors.Is(err, auth.ErrInvalidRole):
		writeError(w, http.StatusBadRequest, `role must be "admin", "editor" or "viewer"`)
		return
	case errors.Is(err, auth.ErrInvalidInput):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	default:
		serverError(w, err)
		return
	}
	sv.auditEvent(r, "create_user", "users", body.Username, nil)
	resp := map[string]string{"username": body.Username, "role": body.Role}
	if generated != "" {
		// the one response that ever carries the password
		resp["password"] = generated
	}
	writeJSON(w, http.StatusCreated, resp)
}

// changeOwnPassword lets any signed-in user rotate their own password
// (so a random first password can be replaced without an admin). The
// current password is required; other sessions of the user are revoked,
// the calling session stays alive.
func (sv *Server) changeOwnPassword(w http.ResponseWriter, r *http.Request) {
	var body struct {
		CurrentPassword string `json:"currentPassword"`
		Password        string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Password == "" {
		writeError(w, http.StatusBadRequest, `body must be {"currentPassword": "...", "password": "new password"}`)
		return
	}
	user := currentUser(r)
	if err := sv.auth.ChangeOwnPassword(user.Username, body.CurrentPassword, body.Password, bearerToken(r)); err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			// 400, not 401: the session is valid, the answer is wrong - the
			// client reserves 401 for expired sessions
			writeError(w, http.StatusBadRequest, "current password is wrong")
		case errors.Is(err, auth.ErrNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, auth.ErrInvalidInput):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			serverError(w, err)
		}
		return
	}
	sv.auditEvent(r, "set_password", "users", user.Username, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (sv *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if name == currentUser(r).Username {
		writeError(w, http.StatusBadRequest, "cannot delete your own account")
		return
	}
	err := sv.auth.DeleteUser(name)
	switch {
	case err == nil:
	case errors.Is(err, auth.ErrNotFound):
		writeError(w, http.StatusNotFound, err.Error())
		return
	case errors.Is(err, auth.ErrLastAdmin):
		writeError(w, http.StatusBadRequest, err.Error())
		return
	default:
		serverError(w, err)
		return
	}
	sv.auditEvent(r, "delete_user", "users", name, nil)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// updateUser changes a user's password and/or role. Revoking that user's
// sessions is part of a password change, so a rotated credential stops
// working everywhere; a role change applies to live sessions at once.
func (sv *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	var body struct {
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || (body.Password == "" && body.Role == "") {
		writeError(w, http.StatusBadRequest, `body must be {"password": "new password"} and/or {"role": "admin|editor|viewer|builder"}`)
		return
	}
	if body.Role != "" && name == currentUser(r).Username {
		writeError(w, http.StatusBadRequest, "cannot change your own role")
		return
	}
	userErr := func(err error) {
		switch {
		case errors.Is(err, auth.ErrNotFound):
			writeError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, auth.ErrInvalidRole):
			writeError(w, http.StatusBadRequest, `role must be "admin", "editor", "viewer" or "builder"`)
		case errors.Is(err, auth.ErrLastAdmin):
			writeError(w, http.StatusBadRequest, "cannot demote the last admin")
		case errors.Is(err, auth.ErrInvalidInput):
			writeError(w, http.StatusBadRequest, err.Error())
		default:
			serverError(w, err)
		}
	}
	if body.Role != "" {
		if err := sv.auth.SetRole(name, auth.Role(body.Role)); err != nil {
			userErr(err)
			return
		}
		sv.auditEvent(r, "set_role", "users", name, map[string]string{"role": body.Role})
	}
	if body.Password != "" {
		if err := sv.auth.SetPassword(name, body.Password); err != nil {
			userErr(err)
			return
		}
		sv.auditEvent(r, "set_password", "users", name, nil)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// LLMsTxt serves a machine-readable API description for LLM agents
// (llms.txt convention). Requires login like everything else; table and
