package desktop

import (
	"errors"
	"net/http"
	"slices"

	"github.com/raulaguila/opcode/internal/config"
	"github.com/raulaguila/opcode/internal/permission"
	"github.com/raulaguila/opcode/internal/review"
)

// Change is one turn that edited files, with the diff its undo (or, once
// undone, its redo) would apply.
type Change struct {
	ID      string   `json:"id"`
	At      int64    `json:"at"`
	Undone  bool     `json:"undone"`
	Files   []string `json:"files"`
	Diff    string   `json:"diff"`
	Version string   `json:"version"`
}

func reviewStore() *review.Store {
	cfg := config.Get()
	return review.New(cfg.WorkingDir, cfg.Data.Directory)
}

// changes lists a conversation's turns that edited files, newest first.
func (s *Server) changes(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	store := reviewStore()
	turns, err := store.List(id)
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	list := []Change{}
	for _, turn := range slices.Backward(turns) {
		preview, err := store.Preview(id, turn.ID, turn.Undone)
		if err != nil {
			continue
		}
		change := Change{ID: turn.ID, At: turn.CreatedAt, Undone: turn.Undone, Diff: preview.Diff, Version: preview.Revision}
		for _, file := range turn.Changes {
			change.Files = append(change.Files, relative(file.Path))
		}
		list = append(list, change)
	}
	reply(w, http.StatusOK, list)
}

// review undoes an applied turn or redoes an undone one. Each file goes
// through the window's approval card; the request answers once all are done.
// The version is the one the window showed, so a turn that changed since
// is refused rather than applied unseen.
func (s *Server) review(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Redo    bool   `json:"redo"`
		Version string `json:"version"`
	}
	if err := decode(r, &request); err != nil {
		fail(w, http.StatusBadRequest, err)
		return
	}
	current, ok := s.manageable(w, r)
	if !ok {
		return
	}
	store := reviewStore()
	preview, err := store.Preview(current.ID, r.PathValue("turn"), request.Redo)
	if errors.Is(err, review.ErrNothingToReview) {
		fail(w, http.StatusNotFound, err)
		return
	}
	if err != nil {
		fail(w, http.StatusInternalServerError, err)
		return
	}
	if request.Version != "" {
		preview.Revision = request.Version
	}
	changed := func() error { return s.app.Workflows.Mutated(r.Context(), current.ID) }
	if s.app.Workflows == nil {
		changed = nil
	}
	switch err := store.Apply(r.Context(), current.ID, preview, s.app.Permissions, changed); {
	case errors.Is(err, permission.ErrorPermissionDenied):
		fail(w, http.StatusForbidden, errors.New(config.Text("nada foi alterado: aprovação recusada", "nothing changed: approval denied")))
	case err != nil:
		fail(w, http.StatusConflict, err)
	default:
		reply(w, http.StatusOK, map[string]any{"redo": request.Redo, "files": len(preview.Changes)})
	}
}
