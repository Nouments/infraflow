// Package planning adapts the deterministic planner to the application port.
package planning

import (
	"infraflow/internal/application/planner"
	"infraflow/internal/domain"
)

type Builder struct{}

func (Builder) Build(infrastructure domain.Infrastructure) domain.Plan {
	return planner.Build(infrastructure)
}
