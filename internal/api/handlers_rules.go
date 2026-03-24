package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/amir-the-h/iran-watcher/internal/model"
	"github.com/amir-the-h/iran-watcher/internal/store"
	"github.com/amir-the-h/iran-watcher/internal/stream"
)

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	filter := store.RuleFilter{}
	if a := r.URL.Query().Get("active"); a != "" {
		active, err := strconv.ParseBool(a)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid active parameter")
			return
		}
		filter.Active = &active
	}
	rules, err := s.store.ListRules(r.Context(), filter)
	if err != nil {
		s.logger.Error("api: list rules", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list rules")
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

func (s *Server) handleGetRule(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid rule ID")
		return
	}
	rule, err := s.store.GetRule(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "rule not found")
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (s *Server) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name       string          `json:"name"`
		Expression json.RawMessage `json:"expression"`
		Local      bool            `json:"local"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if len(req.Expression) == 0 {
		writeError(w, http.StatusBadRequest, "expression is required")
		return
	}

	var node model.ExprNode
	if err := json.Unmarshal(req.Expression, &node); err != nil {
		writeError(w, http.StatusBadRequest, "invalid expression JSON")
		return
	}

	resolver := s.makeResolver(r)
	ruleResolver := s.makeRuleResolver(r)
	validator := stream.NewRuleValidator(s.tier)

	now := time.Now().UTC()
	rule := &model.Rule{
		Name:      req.Name,
		Expression: req.Expression,
		Local:     req.Local,
		CreatedAt: now,
		UpdatedAt: now,
	}

	if req.Local {
		if err := validator.ValidateLocalRule(node, resolver, ruleResolver); err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		compiler := stream.NewRuleCompiler()
		compiled, err := compiler.CompileWithRules(node, resolver, ruleResolver)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		rule.CompiledValue = compiled
		rule.Active = true

		if err := s.store.SaveRule(r.Context(), rule); err != nil {
			s.logger.Error("api: save local rule", "error", err)
			writeError(w, http.StatusInternalServerError, "failed to save rule")
			return
		}
		if err := s.store.SetRuleActive(r.Context(), rule.ID, true, ""); err != nil {
			s.logger.Error("api: set local rule active", "rule_id", rule.ID, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to activate rule")
			return
		}
		rule.Active = true

		writeJSON(w, http.StatusCreated, rule)
		return
	}

	if err := validator.Validate(node, resolver, ruleResolver); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	compiler := stream.NewRuleCompiler()
	compiled, err := compiler.CompileWithRules(node, resolver, ruleResolver)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	rule.CompiledValue = compiled
	rule.Active = false

	if err := s.store.SaveRule(r.Context(), rule); err != nil {
		s.logger.Error("api: save rule", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to save rule")
		return
	}

	xRuleID, err := s.rules.AddRule(r.Context(), rule.Name, compiled)
	if err != nil {
		s.logger.Error("api: add x rule", "rule_id", rule.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to add stream rule to X")
		return
	}
	if err := s.store.SetRuleActive(r.Context(), rule.ID, true, xRuleID); err != nil {
		s.logger.Error("api: set rule active", "rule_id", rule.ID, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to activate rule")
		return
	}
	rule.XRuleID = xRuleID
	rule.Active = true

	writeJSON(w, http.StatusCreated, rule)
}

func (s *Server) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid rule ID")
		return
	}

	existing, err := s.store.GetRule(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "rule not found")
		return
	}

	var req struct {
		Name       *string         `json:"name"`
		Expression json.RawMessage `json:"expression"`
		Local      *bool           `json:"local"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Local != nil {
		existing.Local = *req.Local
	}

	resolver := s.makeResolver(r)
	ruleResolver := s.makeRuleResolver(r)

	expressionChanged := false
	if len(req.Expression) > 0 {
		var node model.ExprNode
		if err := json.Unmarshal(req.Expression, &node); err != nil {
			writeError(w, http.StatusBadRequest, "invalid expression JSON")
			return
		}

		validator := stream.NewRuleValidator(s.tier)
		if existing.Local {
			if err := validator.ValidateLocalRule(node, resolver, ruleResolver); err != nil {
				writeError(w, http.StatusUnprocessableEntity, err.Error())
				return
			}
		} else {
			if err := validator.Validate(node, resolver, ruleResolver); err != nil {
				writeError(w, http.StatusUnprocessableEntity, err.Error())
				return
			}
		}

		compiler := stream.NewRuleCompiler()
		compiled, err := compiler.CompileWithRules(node, resolver, ruleResolver)
		if err != nil {
			writeError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}

		existing.Expression = req.Expression
		existing.CompiledValue = compiled
		expressionChanged = true
	}

	if !existing.Local && existing.Active && expressionChanged {
		if existing.XRuleID != "" {
			if err := s.rules.DeleteRule(r.Context(), existing.XRuleID); err != nil {
				s.logger.Error("api: delete old x rule on update", "rule_id", id, "error", err)
				writeError(w, http.StatusInternalServerError, "failed to update stream rule on X")
				return
			}
		}
		xRuleID, err := s.rules.AddRule(r.Context(), existing.Name, existing.CompiledValue)
		if err != nil {
			s.logger.Error("api: add new x rule on update", "rule_id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to update stream rule on X")
			return
		}
		existing.XRuleID = xRuleID
	}

	if err := s.store.UpdateRule(r.Context(), existing); err != nil {
		s.logger.Error("api: update rule", "rule_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update rule")
		return
	}

	if !existing.Local && existing.Active && expressionChanged {
		if err := s.store.SetRuleActive(r.Context(), id, true, existing.XRuleID); err != nil {
			s.logger.Error("api: set rule active after update", "rule_id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to sync rule x_rule_id")
			return
		}
	}

	writeJSON(w, http.StatusOK, existing)
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid rule ID")
		return
	}

	rule, err := s.store.GetRule(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "rule not found")
		return
	}

	count, err := s.store.TweetRuleCount(r.Context(), id)
	if err != nil {
		s.logger.Error("api: tweet rule count", "rule_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to check tweet history")
		return
	}
	if count > 0 {
		writeError(w, http.StatusConflict, "rule has tweet history; deactivate it instead of deleting")
		return
	}

	if !rule.Local && rule.Active && rule.XRuleID != "" {
		if err := s.rules.DeleteRule(r.Context(), rule.XRuleID); err != nil {
			s.logger.Error("api: delete x rule", "rule_id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to delete stream rule from X")
			return
		}
	}

	if err := s.store.DeleteRule(r.Context(), id); err != nil {
		s.logger.Error("api: delete rule", "rule_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to delete rule")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleActivateRule(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid rule ID")
		return
	}

	rule, err := s.store.GetRule(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "rule not found")
		return
	}
	if rule.Active {
		writeJSON(w, http.StatusOK, rule)
		return
	}

	// Local rules: just flip the DB flag, no X API calls.
	if rule.Local {
		if err := s.store.SetRuleActive(r.Context(), id, true, ""); err != nil {
			s.logger.Error("api: activate local rule", "rule_id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to activate rule")
			return
		}
		rule.Active = true
		writeJSON(w, http.StatusOK, rule)
		return
	}

	var node model.ExprNode
	if err := json.Unmarshal(rule.Expression, &node); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to parse stored expression")
		return
	}
	resolver := s.makeResolver(r)
	ruleResolver := s.makeRuleResolver(r)
	validator := stream.NewRuleValidator(s.tier)
	if err := validator.Validate(node, resolver, ruleResolver); err != nil {
		writeError(w, http.StatusUnprocessableEntity, "rule is no longer valid: "+err.Error())
		return
	}
	compiler := stream.NewRuleCompiler()
	compiled, err := compiler.CompileWithRules(node, resolver, ruleResolver)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	xRuleID, err := s.rules.AddRule(r.Context(), rule.Name, compiled)
	if err != nil {
		s.logger.Error("api: activate rule — add x rule", "rule_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to add stream rule to X")
		return
	}
	if err := s.store.SetRuleActive(r.Context(), id, true, xRuleID); err != nil {
		s.logger.Error("api: activate rule — set active", "rule_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to activate rule")
		return
	}
	rule.Active = true
	rule.XRuleID = xRuleID

	writeJSON(w, http.StatusOK, rule)
}

func (s *Server) handleDeactivateRule(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid rule ID")
		return
	}

	rule, err := s.store.GetRule(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, "rule not found")
		return
	}
	if !rule.Active {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Local rules: just flip the DB flag, no X API calls.
	if rule.Local {
		if err := s.store.SetRuleActive(r.Context(), id, false, ""); err != nil {
			s.logger.Error("api: deactivate local rule", "rule_id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to deactivate rule")
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if rule.XRuleID != "" {
		if err := s.rules.DeleteRule(r.Context(), rule.XRuleID); err != nil {
			s.logger.Error("api: deactivate rule — delete x rule", "rule_id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "failed to remove stream rule from X")
			return
		}
	}
	if err := s.store.SetRuleActive(r.Context(), id, false, ""); err != nil {
		s.logger.Error("api: deactivate rule — set inactive", "rule_id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "failed to deactivate rule")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// parseID extracts and parses the {id} URL parameter.
func parseID(r *http.Request) (int64, error) {
	return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
}
