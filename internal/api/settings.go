package api

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// datesPayload is the Settings view's date options: how ambiguous slash
// dates are read, and whether years >= 2400 are Buddhist Era.
type datesPayload struct {
	DateOrder     string `json:"dateOrder"`
	BuddhistYears bool   `json:"buddhistYears"`
}

// getDatesOrder reports the date settings.
func (sv *Server) getDatesOrder(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, datesPayload{
		DateOrder:     sv.store.DateOrder(),
		BuddhistYears: sv.store.BuddhistYears(),
	})
}

// updateDatesOrder stores the date settings; they take effect immediately
// for imports and row writes.
func (sv *Server) updateDatesOrder(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Order         *string `json:"order"`
		BuddhistYears *bool   `json:"buddhistYears"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	order := sv.store.DateOrder()
	if body.Order != nil {
		order = *body.Order
	}
	beYear := sv.store.BuddhistYears()
	if body.BuddhistYears != nil {
		beYear = *body.BuddhistYears
	}
	if err := sv.store.SetDateOrder(order); mapStoreErr(w, err) {
		return
	}
	if err := sv.store.SetBuddhistYears(beYear); mapStoreErr(w, err) {
		return
	}
	sv.auditEvent(r, "set_date_order", "", "", map[string]string{"order": order, "beYear": strconv.FormatBool(beYear)})
	writeJSON(w, http.StatusOK, datesPayload{DateOrder: order, BuddhistYears: beYear})
}
