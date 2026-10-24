package httpapi

import (
	"net/http"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// Channel URLs arrive masked from the use case for every role (OLD B2):
// these handlers never see a real URL to leak.

func (a *API) handleListChannels(w http.ResponseWriter, r *http.Request) {
	scope, err := scopeFrom(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	cs, err := a.cfg.Alerts.Channels(r.Context(), scope)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := channelListDTO{Channels: make([]channelDTO, 0, len(cs))}
	for _, c := range cs {
		out.Channels = append(out.Channels, toChannel(c))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) handleCreateChannel(w http.ResponseWriter, r *http.Request) {
	scope, err := scopeFrom(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in channelDTO
	if err := decodeBody(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	c, err := a.cfg.Alerts.CreateChannel(r.Context(), scope, domain.Channel{Name: in.Name, Kind: domain.ChannelKind(in.Kind), URL: in.URL})
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toChannel(c))
}

func (a *API) handleUpdateChannel(w http.ResponseWriter, r *http.Request) {
	scope, err := scopeFrom(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	id, err := pathInt(r, "cid")
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in channelDTO
	if err := decodeBody(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	c := domain.Channel{ID: id, Name: in.Name, Kind: domain.ChannelKind(in.Kind), URL: in.URL}
	if err := a.cfg.Alerts.UpdateChannel(r.Context(), scope, c); err != nil {
		writeError(w, r, err)
		return
	}
	c.URL = c.MaskedURL()
	writeJSON(w, http.StatusOK, toChannel(c))
}

func (a *API) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	a.deleteByID(w, r, "cid", a.cfg.Alerts.DeleteChannel)
}

func (a *API) handleListSilences(w http.ResponseWriter, r *http.Request) {
	scope, err := scopeFrom(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	ss, err := a.cfg.Alerts.Silences(r.Context(), scope)
	if err != nil {
		writeError(w, r, err)
		return
	}
	out := silenceListDTO{Silences: make([]silenceDTO, 0, len(ss))}
	for _, s := range ss {
		out.Silences = append(out.Silences, toSilence(s))
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *API) handleCreateSilence(w http.ResponseWriter, r *http.Request) {
	scope, err := scopeFrom(r.Context())
	if err != nil {
		writeError(w, r, err)
		return
	}
	var in silenceDTO
	if err := decodeBody(w, r, &in); err != nil {
		writeError(w, r, err)
		return
	}
	sil, err := in.toDomain()
	if err != nil {
		writeError(w, r, err)
		return
	}
	created, err := a.cfg.Alerts.CreateSilence(r.Context(), scope, sil)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, toSilence(created))
}

func (a *API) handleDeleteSilence(w http.ResponseWriter, r *http.Request) {
	a.deleteByID(w, r, "sid", a.cfg.Alerts.DeleteSilence)
}
