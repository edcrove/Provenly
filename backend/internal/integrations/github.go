package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/edcrove/provenly/backend/internal/catalog"
	"github.com/edcrove/provenly/backend/internal/platform/apperr"
)

// GitHub connector limits.
const (
	githubPerPage  = 100
	githubMaxPages = 5 // at most 500 issues per sync, the import bound
	maxLabels      = 200
	maxToken       = 500
	// minHintedToken is the shortest token whose last four characters are shown.
	minHintedToken = 12
)

var repositoryPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}/[A-Za-z0-9_.-]{1,100}$`)

// GitHubView is a project's GitHub connection as shown: never the token, only its last four characters.
type GitHubView struct {
	Repository   string
	Labels       string
	TokenHint    string
	LastSyncedAt *time.Time
	LastError    string
	UpdatedAt    time.Time
}

// GitHubInput connects or reconfigures a project's GitHub connection; a nil Token keeps the stored one.
type GitHubInput struct {
	Repository string
	Token      *string
	Labels     string
}

func noConnection(key string) error {
	return apperr.NotFound("project %s has no GitHub connection", key)
}

func (s *Service) view(c GitHubConnection) GitHubView {
	v := GitHubView{Repository: c.Repository, Labels: c.Labels, LastSyncedAt: c.LastSyncedAt, LastError: c.LastError, UpdatedAt: c.UpdatedAt}
	if token, err := s.box.Open(c.Token); err == nil {
		// Only the last four characters, and none of a short token (they would be most of it).
		v.TokenHint = "…"
		if len(token) >= minHintedToken {
			v.TokenHint += token[len(token)-4:]
		}
	}
	return v
}

// GitHub returns a project's GitHub connection (maintainers).
func (s *Service) GitHub(ctx context.Context, projectKey string) (GitHubView, error) {
	p, err := s.project(ctx, projectKey)
	if err != nil {
		return GitHubView{}, err
	}
	c, err := s.repo.GetGitHubConnection(ctx, p.ID)
	if errors.Is(err, ErrNotFound) {
		return GitHubView{}, noConnection(projectKey)
	}
	if err != nil {
		return GitHubView{}, err
	}
	return s.view(c), nil
}

// ConnectGitHub connects a project to a repository, or changes its repository, labels or token (rotation without
// reconnecting). The token is required to connect and stored encrypted.
func (s *Service) ConnectGitHub(ctx context.Context, projectKey string, in GitHubInput) (GitHubView, error) {
	in.Repository, in.Labels = strings.TrimSpace(in.Repository), strings.TrimSpace(in.Labels)
	var v apperr.Validator
	v.Check(repositoryPattern.MatchString(in.Repository), "repository", "must be owner/name")
	v.Check(validLen(in.Labels, maxLabels), "labels", fmt.Sprintf("must be at most %d characters", maxLabels))
	v.CheckText("labels", in.Labels)
	if in.Token != nil {
		v.Check(*in.Token != "" && validLen(*in.Token, maxToken), "token", fmt.Sprintf("must be 1 to %d characters", maxToken))
		v.CheckText("token", *in.Token)
	}
	if err := v.Err(); err != nil {
		return GitHubView{}, err
	}
	p, err := s.project(ctx, projectKey)
	if err != nil {
		return GitHubView{}, err
	}
	c := GitHubConnection{ProjectID: p.ID, Repository: in.Repository, Labels: in.Labels}
	if in.Token != nil {
		c.Token = s.box.Seal(*in.Token)
	} else {
		current, err := s.repo.GetGitHubConnection(ctx, p.ID)
		if errors.Is(err, ErrNotFound) {
			return GitHubView{}, apperr.Validation(apperr.ValidationFailed, apperr.FieldError{Field: "token", Message: "is required to connect"})
		}
		if err != nil {
			return GitHubView{}, err
		}
		c.Token = current.Token
	}
	if err := s.repo.UpsertGitHubConnection(ctx, c); err != nil {
		return GitHubView{}, err
	}
	return s.GitHub(ctx, projectKey)
}

// DisconnectGitHub removes a project's GitHub connection and its token; mirrored issues stay.
func (s *Service) DisconnectGitHub(ctx context.Context, projectKey string) error {
	p, err := s.project(ctx, projectKey)
	if err != nil {
		return err
	}
	removed, err := s.repo.DeleteGitHubConnection(ctx, p.ID)
	if err == nil && !removed {
		err = noConnection(projectKey)
	}
	return err
}

type githubIssue struct {
	Number      int64           `json:"number"`
	Title       string          `json:"title"`
	Body        string          `json:"body"`
	HTMLURL     string          `json:"html_url"`
	State       string          `json:"state"`
	StateReason string          `json:"state_reason"`
	PullRequest json.RawMessage `json:"pull_request"`
}

// SyncGitHub mirrors the repository's issues (not pull requests; only those with the labels, when set) into the
// project's issues: created or updated by number, with their state. The outcome is recorded on the connection.
func (s *Service) SyncGitHub(ctx context.Context, projectKey string) (catalog.ImportResult, error) {
	p, err := s.project(ctx, projectKey)
	if err != nil {
		return catalog.ImportResult{}, err
	}
	c, err := s.repo.GetGitHubConnection(ctx, p.ID)
	if errors.Is(err, ErrNotFound) {
		return catalog.ImportResult{}, noConnection(projectKey)
	}
	if err != nil {
		return catalog.ImportResult{}, err
	}
	res, syncErr := s.syncGitHub(ctx, p.ID, c)
	var synced *time.Time
	message := ""
	if syncErr == nil {
		now := s.now()
		synced = &now
	} else {
		message = truncate(syncErr.Error(), 1000)
	}
	if err := s.repo.RecordGitHubSync(ctx, p.ID, synced, message); err != nil {
		return catalog.ImportResult{}, err
	}
	return res, syncErr
}

func (s *Service) syncGitHub(ctx context.Context, projectID int64, c GitHubConnection) (catalog.ImportResult, error) {
	token, err := s.box.Open(c.Token)
	if err != nil {
		return catalog.ImportResult{}, apperr.Conflict("the GitHub token cannot be read: %s; connect again with a new token", err)
	}
	var items []catalog.IssueInput
	for page := 1; page <= githubMaxPages; page++ {
		issues, err := s.githubIssues(ctx, c, token, page)
		if err != nil {
			return catalog.ImportResult{}, err
		}
		for _, is := range issues {
			if is.PullRequest != nil {
				continue
			}
			state := catalog.IssueOpen
			if is.State == "closed" {
				state = catalog.IssueClosed
			}
			items = append(items, catalog.IssueInput{
				ExternalID: strconv.FormatInt(is.Number, 10), Title: truncate(cmpOr(strings.TrimSpace(is.Title), "#"+strconv.FormatInt(is.Number, 10)), 300),
				Description: truncate(is.Body, 10000), URL: truncate(is.HTMLURL, 2000), State: state,
				ProviderStatus: truncate(cmpOr(is.StateReason, is.State), 50),
			})
		}
		if len(issues) < githubPerPage {
			break
		}
	}
	if len(items) == 0 {
		return catalog.ImportResult{}, nil
	}
	return s.catalog.ImportIssues(ctx, projectID, catalog.ProviderGitHub, items)
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// githubIssues reads one page of a repository's issues.
func (s *Service) githubIssues(ctx context.Context, c GitHubConnection, token string, page int) ([]githubIssue, error) {
	q := url.Values{"state": {"all"}, "per_page": {strconv.Itoa(githubPerPage)}, "page": {strconv.Itoa(page)}}
	if c.Labels != "" {
		q.Set("labels", c.Labels)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, s.cfg.GitHubAPIURL+"/repos/"+c.Repository+"/issues?"+q.Encode(), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	res, err := s.client.Do(req)
	if err != nil {
		return nil, apperr.Upstream("GitHub is unreachable: %s", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return nil, apperr.Upstream("GitHub answered %d for %s (check the repository and the token's access)", res.StatusCode, c.Repository)
	}
	var issues []githubIssue
	if err := json.NewDecoder(res.Body).Decode(&issues); err != nil {
		return nil, apperr.Upstream("GitHub answered something that is not a list of issues: %s", err)
	}
	return issues, nil
}
