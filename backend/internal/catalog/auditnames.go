package catalog

import "context"

// The audit log names what a change touched through these lookups (audit.Resolver): they never check access, so
// they only feed events of changes that were already allowed.

// ProjectKey returns the key of a project by its id.
func (s *Service) ProjectKey(ctx context.Context, id int64) (string, error) {
	p, err := s.ProjectByID(ctx, id)
	return p.Key, err
}

// TestCaseKey returns the key of a test case (CHK-4).
func (s *Service) TestCaseKey(ctx context.Context, id int64) (string, error) {
	tc, err := s.Get(ctx, id)
	return tc.Key(), err
}

// StepPosition returns the position of a step of a test case.
func (s *Service) StepPosition(ctx context.Context, testCaseID, stepID int64) (int32, error) {
	steps, err := s.repo.ListAllTestSteps(ctx, testCaseID)
	if err != nil {
		return 0, err
	}
	for _, st := range steps {
		if st.ID == stepID {
			return st.Position, nil
		}
	}
	return 0, ErrNotFound
}
