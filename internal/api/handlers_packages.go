// SPDX-License-Identifier: MIT
package api

import (
	"net/http"
	"strings"

	"github.com/StakoCP/stako/internal/crypto"
	"github.com/StakoCP/stako/internal/store"
)

func (s *Server) handleListPackages(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	var ownerFilter *string
	if ac.User.Role == "reseller" {
		ownerFilter = &ac.User.ID
	} else if ac.User.Role == "user" || ac.User.Role == "readonly" {
		if ac.User.PackageID == nil {
			WriteJSON(w, http.StatusOK, map[string]any{"items": []store.Package{}, "total": 0})
			return
		}
		p, err := s.store.GetPackageByID(r.Context(), *ac.User.PackageID)
		if err != nil {
			WriteJSON(w, http.StatusOK, map[string]any{"items": []store.Package{}, "total": 0})
			return
		}
		WriteJSON(w, http.StatusOK, map[string]any{"items": []store.Package{*p}, "total": 1})
		return
	}
	items, err := s.store.ListPackages(r.Context(), ownerFilter)
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items, "total": len(items)})
}

func (s *Server) handleCreatePackage(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	if ac.User.Role != "admin" && ac.User.Role != "reseller" {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var p store.Package
	if err := DecodeJSON(r, &p); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" {
		WriteProblem(w, http.StatusUnprocessableEntity, "validation", "Name required", "",
			FieldError{Field: "name", Message: "Package name is required"})
		return
	}
	p.ID, _ = crypto.RandomID("p")
	if ac.User.Role == "reseller" {
		p.OwnerID = &ac.User.ID
	} else {
		p.OwnerID = nil
	}
	if p.PHPVersions == nil {
		p.PHPVersions = []string{"8.3"}
	}
	if err := s.store.CreatePackage(r.Context(), &p); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "package.create", "package", p.ID, "success", map[string]any{"name": p.Name})
	WriteJSON(w, http.StatusCreated, p)
}

func (s *Server) handleGetPackage(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	p, err := s.store.GetPackageByID(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if ac.User.Role == "user" || ac.User.Role == "readonly" {
		if ac.User.PackageID == nil || *ac.User.PackageID != p.ID {
			WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
			return
		}
	}
	WriteJSON(w, http.StatusOK, p)
}

func (s *Server) handleUpdatePackage(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	p, err := s.store.GetPackageByID(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if ac.User.Role != "admin" && (ac.User.Role != "reseller" || p.OwnerID == nil || *p.OwnerID != ac.User.ID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	var upd store.Package
	if err := DecodeJSON(r, &upd); err != nil {
		WriteProblem(w, http.StatusBadRequest, "validation", "Invalid request", err.Error())
		return
	}
	upd.ID = p.ID
	upd.OwnerID = p.OwnerID
	if strings.TrimSpace(upd.Name) == "" {
		upd.Name = p.Name
	}
	if upd.PHPVersions == nil {
		upd.PHPVersions = p.PHPVersions
	}
	if err := s.store.UpdatePackage(r.Context(), &upd); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "package.update", "package", upd.ID, "success", nil)
	WriteJSON(w, http.StatusOK, upd)
}

func (s *Server) handleDeletePackage(w http.ResponseWriter, r *http.Request) {
	ac := GetAuth(r)
	p, err := s.store.GetPackageByID(r.Context(), r.PathValue("id"))
	if err != nil {
		handleStoreErr(w, err)
		return
	}
	if ac.User.Role != "admin" && (ac.User.Role != "reseller" || p.OwnerID == nil || *p.OwnerID != ac.User.ID) {
		WriteProblem(w, http.StatusForbidden, "forbidden", "Forbidden", "")
		return
	}
	if err := s.store.DeletePackage(r.Context(), p.ID); err != nil {
		handleStoreErr(w, err)
		return
	}
	s.audit(r, ac.User, "package.delete", "package", p.ID, "success", nil)
	w.WriteHeader(http.StatusNoContent)
}
