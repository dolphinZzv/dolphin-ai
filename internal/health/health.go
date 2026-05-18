// Package health provides a health check framework with JSON exposition format.
package health

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
)

// Status represents the health status of a component.
type Status string

const (
	StatusOK       Status = "ok"
	StatusError    Status = "error"
	StatusDegraded Status = "degraded"
)

// ComponentStatus represents the health of a single component.
type ComponentStatus struct {
	Name    string `json:"name"`
	Status  Status `json:"status"`
	Message string `json:"message,omitempty"`
}

// Checker is the interface each health-checkable component implements.
type Checker interface {
	Name() string
	Check(ctx context.Context) ComponentStatus
}

// Result is the top-level health check response.
type Result struct {
	Status     Status            `json:"status"`
	Components []ComponentStatus `json:"components,omitempty"`
}

// CheckAll runs all checkers and aggregates the results.
func CheckAll(ctx context.Context, checkers []Checker) Result {
	components := make([]ComponentStatus, len(checkers))
	for i, c := range checkers {
		components[i] = c.Check(ctx)
	}

	overall := StatusOK
	for _, cs := range components {
		if cs.Status == StatusError {
			if overall == StatusOK {
				overall = StatusDegraded
			}
		}
	}

	return Result{Status: overall, Components: components}
}

// Handler returns an http.Handler that runs all given checkers and returns JSON.
func Handler(checkers ...Checker) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result := CheckAll(r.Context(), checkers)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(w).Encode(result); err != nil {
			log.Printf("health encode error: %v", err)
		}
	})
}

// ---- Built-in checkers ----

// FuncChecker adapts a function to the Checker interface.
type FuncChecker struct {
	name string
	fn   func(context.Context) error
}

func (f *FuncChecker) Name() string { return f.name }

func (f *FuncChecker) Check(ctx context.Context) ComponentStatus {
	if err := f.fn(ctx); err != nil {
		return ComponentStatus{Name: f.name, Status: StatusError, Message: err.Error()}
	}
	return ComponentStatus{Name: f.name, Status: StatusOK}
}

// NewChecker creates a Checker from a name and a check function.
// The function returns nil for healthy, or an error describing the problem.
func NewChecker(name string, fn func(context.Context) error) Checker {
	return &FuncChecker{name: name, fn: fn}
}
