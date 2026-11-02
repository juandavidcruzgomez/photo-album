package api

import (
	"encoding/json"
	"net/http"
	"strconv"
)

type tagName struct {
	Name string `json:"name"`
}

func decode(r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return errBadRequest("invalid JSON body: " + err.Error())
	}
	return nil
}

func tagID(r *http.Request, name string) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil {
		return 0, errBadRequest("invalid tag id")
	}
	return id, nil
}

func (s *Server) listTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.Library.ListTags(r.Context())
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
}

func (s *Server) createTag(w http.ResponseWriter, r *http.Request) {
	var body tagName
	if err := decode(r, &body); err != nil {
		s.writeError(w, r, err)
		return
	}
	tag, err := s.Library.CreateTag(r.Context(), body.Name)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tag)
}

func (s *Server) renameTag(w http.ResponseWriter, r *http.Request) {
	id, err := tagID(r, "id")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	var body tagName
	if err := decode(r, &body); err != nil {
		s.writeError(w, r, err)
		return
	}
	tag, err := s.Library.RenameTag(r.Context(), id, body.Name)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, tag)
}

func (s *Server) deleteTag(w http.ResponseWriter, r *http.Request) {
	id, err := tagID(r, "id")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.Library.DeleteTag(r.Context(), id); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) tagPhoto(w http.ResponseWriter, r *http.Request) {
	var body tagName
	if err := decode(r, &body); err != nil {
		s.writeError(w, r, err)
		return
	}
	ref, err := s.Library.TagPhoto(r.Context(), r.PathValue("id"), body.Name)
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, ref)
}

func (s *Server) untagPhoto(w http.ResponseWriter, r *http.Request) {
	id, err := tagID(r, "tagID")
	if err != nil {
		s.writeError(w, r, err)
		return
	}
	if err := s.Library.UntagPhoto(r.Context(), r.PathValue("id"), id); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// tagPhotos tags several photos at once:
// {"photo_ids": [...], "add": ["name", ...], "remove": [tagID, ...]}
func (s *Server) tagPhotos(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PhotoIDs []string `json:"photo_ids"`
		Add      []string `json:"add"`
		Remove   []int64  `json:"remove"`
	}
	if err := decode(r, &body); err != nil {
		s.writeError(w, r, err)
		return
	}
	if len(body.PhotoIDs) == 0 {
		s.writeError(w, r, errBadRequest("photo_ids is empty"))
		return
	}
	if err := s.Library.TagPhotos(r.Context(), body.PhotoIDs, body.Add, body.Remove); err != nil {
		s.writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
