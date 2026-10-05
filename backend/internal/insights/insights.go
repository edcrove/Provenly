// Package insights derives a project's quality indicators from the catalog and the execution history (Notion 21
// Dashboards, Metrics & Quality Intelligence): automation, test cases not executed recently and flaky test cases. It
// owns no data; run trends, requirement coverage and issue verification come from their own modules.
package insights

import (
	"cmp"
	"context"
	"math"
	"slices"
	"time"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/execution"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
)

// Bounds of the quality query.
const (
	DefaultStaleDays = 14
	MaxStaleDays     = 365
	DefaultWindow    = 20
	MaxWindow        = 200
	// maxListed bounds the stale and flaky test cases listed (counts are always complete).
	maxListed = 20
)

// Catalog is what insights needs from the catalog module.
type Catalog interface {
	ProjectByKey(ctx context.Context, key string) (catalog.Project, error)
	Selection(ctx context.Context, projectID int64, key string, automated *bool) (catalog.Suite, []int64, error)
	Keys(ctx context.Context, ids []int64) (map[int64]string, error)
}

// History is what insights needs from the execution module.
type History interface {
	LastExecuted(ctx context.Context, testCaseIDs []int64) (map[int64]time.Time, error)
	FlakyCounts(ctx context.Context, projectID int64, window, limit int32) ([]execution.FlakyCount, error)
}

// Access authorizes reads of a project.
type Access interface {
	Require(ctx context.Context, projectID int64, minRole authz.Role, notFound error) error
}

// Query narrows the quality read.
type Query struct {
	ProjectKey string
	// StaleDays: an active test case not executed for more days than this is stale.
	StaleDays int32
	// Window is how many of the latest runs flakiness is counted over.
	Window int32
}

// StaleCase is an active test case not executed recently (LastExecutedAt nil: never).
type StaleCase struct {
	TestCaseID     int64
	Key            string
	LastExecutedAt *time.Time
}

// FlakyCase is a test case flaky in some of the latest runs.
type FlakyCase struct {
	TestCaseID int64
	Key        string
	Runs       int32
}

// Quality is a project's quality indicators.
type Quality struct {
	Active, Automated, Manual int32
	// AutomationRate is the percentage of active test cases that are automated (0 without active ones).
	AutomationRate float64
	StaleDays      int32
	// NeverExecuted and Stale count active test cases without results and with only old ones.
	NeverExecuted, Stale int32
	StaleCases           []StaleCase
	Window               int32
	Flaky                []FlakyCase
}

// Service computes quality indicators.
type Service struct {
	catalog Catalog
	history History
	access  Access
	now     func() time.Time
}

// NewService builds a Service.
func NewService(c Catalog, h History, a Access, now func() time.Time) *Service {
	return &Service{catalog: c, history: h, access: a, now: now}
}

func percent(part, total int32) float64 {
	if total == 0 {
		return 0
	}
	return math.Round(float64(part)*10000/float64(total)) / 100
}

// Quality reads a project's quality indicators (anyone who can see the project).
func (s *Service) Quality(ctx context.Context, q Query) (Quality, error) {
	q.StaleDays, q.Window = cmp.Or(q.StaleDays, DefaultStaleDays), cmp.Or(q.Window, DefaultWindow)
	var v apperr.Validator
	v.Check(catalog.ProjectKeyPattern.MatchString(q.ProjectKey), "projectKey", catalog.ProjectKeyMessage)
	v.Check(q.StaleDays >= 1 && q.StaleDays <= MaxStaleDays, "staleDays", "must be 1 to 365")
	v.Check(q.Window >= 1 && q.Window <= MaxWindow, "window", "must be 1 to 200")
	if err := v.Err(); err != nil {
		return Quality{}, err
	}
	p, err := s.catalog.ProjectByKey(ctx, q.ProjectKey)
	if err == nil {
		err = s.access.Require(ctx, p.ID, authz.RoleViewer, apperr.NotFound("project %s not found", q.ProjectKey))
	}
	if err != nil {
		return Quality{}, err
	}
	automatedFlag, manualFlag := true, false
	_, automated, err := s.catalog.Selection(ctx, p.ID, "", &automatedFlag)
	if err != nil {
		return Quality{}, err
	}
	_, manual, err := s.catalog.Selection(ctx, p.ID, "", &manualFlag)
	if err != nil {
		return Quality{}, err
	}
	active := slices.Concat(automated, manual)
	slices.Sort(active)
	out := Quality{Active: int32(len(active)), Automated: int32(len(automated)), Manual: int32(len(manual)),
		AutomationRate: percent(int32(len(automated)), int32(len(active))), StaleDays: q.StaleDays, Window: q.Window}

	last, err := s.history.LastExecuted(ctx, active)
	if err != nil {
		return Quality{}, err
	}
	cutoff := s.now().AddDate(0, 0, -int(q.StaleDays))
	var stale []StaleCase
	for _, id := range active {
		at, ok := last[id]
		switch {
		case !ok:
			out.NeverExecuted++
			stale = append(stale, StaleCase{TestCaseID: id})
		case at.Before(cutoff):
			out.Stale++
			stale = append(stale, StaleCase{TestCaseID: id, LastExecutedAt: &at})
		}
	}
	// Never executed first, then the oldest; ties by id.
	slices.SortStableFunc(stale, func(a, b StaleCase) int {
		switch {
		case a.LastExecutedAt == nil || b.LastExecutedAt == nil:
			return cmp.Compare(boolRank(a.LastExecutedAt == nil), boolRank(b.LastExecutedAt == nil))
		default:
			return a.LastExecutedAt.Compare(*b.LastExecutedAt)
		}
	})
	out.StaleCases = stale[:min(len(stale), maxListed)]

	flaky, err := s.history.FlakyCounts(ctx, p.ID, q.Window, maxListed)
	if err != nil {
		return Quality{}, err
	}
	ids := make([]int64, 0, len(out.StaleCases)+len(flaky))
	for _, c := range out.StaleCases {
		ids = append(ids, c.TestCaseID)
	}
	for _, f := range flaky {
		ids = append(ids, f.TestCaseID)
	}
	keys, err := s.catalog.Keys(ctx, ids)
	if err != nil {
		return Quality{}, err
	}
	for i := range out.StaleCases {
		out.StaleCases[i].Key = keys[out.StaleCases[i].TestCaseID]
	}
	out.Flaky = make([]FlakyCase, len(flaky))
	for i, f := range flaky {
		out.Flaky[i] = FlakyCase{TestCaseID: f.TestCaseID, Key: keys[f.TestCaseID], Runs: f.Runs}
	}
	return out, nil
}

// boolRank sorts true (never executed) first.
func boolRank(b bool) int {
	if b {
		return 0
	}
	return 1
}
