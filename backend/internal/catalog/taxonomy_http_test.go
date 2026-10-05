package catalog

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/platform/apperr"
	"github.com/edcrove/provenly/backend/internal/platform/authz"
)

var sampleDimension = Dimension{ID: 3, ProjectID: 1, Key: "risk", Name: "Risk", BuiltIn: true,
	Values: []DimensionValue{{ID: 4, DimensionID: 3, Key: "critical", Name: "Critical", Position: 1}}}

// dimensionCall records the last taxonomy call of the stub.
type dimensionCall struct {
	projectID       int64
	dimension, item string
	in              DimensionInput
	update          UpdateDimensionInput
}

var lastDimensionCall dimensionCall

func (s *stubAPI) Dimensions(_ context.Context, projectID int64) ([]Dimension, error) {
	lastDimensionCall = dimensionCall{projectID: projectID}
	return []Dimension{sampleDimension}, s.err
}
func (s *stubAPI) CreateDimension(_ context.Context, projectID int64, in DimensionInput) (Dimension, error) {
	lastDimensionCall = dimensionCall{projectID: projectID, in: in}
	return sampleDimension, s.err
}
func (s *stubAPI) UpdateDimension(_ context.Context, projectID int64, key string, in UpdateDimensionInput) (Dimension, error) {
	lastDimensionCall = dimensionCall{projectID: projectID, dimension: key, update: in}
	return sampleDimension, s.err
}
func (s *stubAPI) CreateDimensionValue(_ context.Context, projectID int64, key string, in DimensionInput) (Dimension, error) {
	lastDimensionCall = dimensionCall{projectID: projectID, dimension: key, in: in}
	return sampleDimension, s.err
}
func (s *stubAPI) UpdateDimensionValue(_ context.Context, projectID int64, key, value string, in UpdateDimensionInput) (Dimension, error) {
	lastDimensionCall = dimensionCall{projectID: projectID, dimension: key, item: value, update: in}
	return sampleDimension, s.err
}

func TestTaxonomyHandlers(t *testing.T) {
	cases := []struct {
		method, target, body string
		status               int
		want                 dimensionCall
	}{
		{"GET", "/api/v1/projects/TC/dimensions", "", 200, dimensionCall{projectID: 1}},
		{"POST", "/api/v1/projects/TC/dimensions", `{"key":"browser","name":"Browser"}`, 201,
			dimensionCall{projectID: 1, in: DimensionInput{Key: "browser", Name: "Browser"}}},
		{"PATCH", "/api/v1/projects/TC/dimensions/risk", `{"archived":true}`, 200,
			dimensionCall{projectID: 1, dimension: "risk", update: UpdateDimensionInput{Archived: ptr(true)}}},
		{"POST", "/api/v1/projects/TC/dimensions/risk/values", `{"key":"medium","name":"Medium"}`, 201,
			dimensionCall{projectID: 1, dimension: "risk", in: DimensionInput{Key: "medium", Name: "Medium"}}},
		{"PATCH", "/api/v1/projects/TC/dimensions/risk/values/critical", `{"name":"Blocker"}`, 200,
			dimensionCall{projectID: 1, dimension: "risk", item: "critical", update: UpdateDimensionInput{Name: ptr("Blocker")}}},
	}
	for _, c := range cases {
		rec := serve(&stubAPI{}, c.method, c.target, c.body)
		assert.Equal(t, c.status, rec.Code, "%s %s", c.method, c.target)
		assert.Contains(t, rec.Body.String(), `"key":"risk","name":"Risk","builtIn":true,"archivedAt":null,"values":[{"key":"critical","name":"Critical","archivedAt":null}]`)
		assert.Equal(t, c.want, lastDimensionCall, "%s %s", c.method, c.target)
	}
	rec := serve(&stubAPI{}, "GET", "/api/v1/projects/TC/dimensions", "")
	assert.Contains(t, rec.Body.String(), `{"items":[{"key":"risk"`)

	// Errors of the service, malformed keys and bodies.
	failing := &stubAPI{err: apperr.NotFound("missing")}
	for _, c := range cases {
		assert.Equal(t, http.StatusNotFound, serve(failing, c.method, c.target, c.body).Code, "%s %s", c.method, c.target)
	}
	bad := []struct{ method, target, body, field string }{
		{"GET", "/api/v1/projects/tc/dimensions", "", "projectKey"},
		{"POST", "/api/v1/projects/TC/dimensions", `nope`, "body"},
		{"PATCH", "/api/v1/projects/TC/dimensions/Risk", `{"name":"x"}`, "dimensionKey"},
		{"PATCH", "/api/v1/projects/TC/dimensions/risk", `nope`, "body"},
		{"POST", "/api/v1/projects/TC/dimensions/9risk/values", `{"key":"a","name":"A"}`, "dimensionKey"},
		{"POST", "/api/v1/projects/TC/dimensions/risk/values", `[]`, "body"},
		{"PATCH", "/api/v1/projects/TC/dimensions/-x/values/a", `{"name":"x"}`, "dimensionKey"},
		{"PATCH", "/api/v1/projects/TC/dimensions/risk/values/A", `{"name":"x"}`, "valueKey"},
		{"PATCH", "/api/v1/projects/TC/dimensions/risk/values/a", `{"name":1}`, "body"},
	}
	for _, b := range bad {
		rec := serve(&stubAPI{}, b.method, b.target, b.body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, "%s %s", b.method, b.target)
		if b.field != "body" {
			assert.Contains(t, rec.Body.String(), `"field":"`+b.field+`"`, "%s %s", b.method, b.target)
		}
	}
	// A body that does not parse fails before the project lookup.
	for _, b := range bad[1:] {
		if b.field == "body" {
			assert.Equal(t, http.StatusBadRequest, serve(&stubAPI{projectErr: apperr.NotFound("x")}, b.method, b.target, b.body).Code)
		}
	}
}

func TestTaxonomyAuthorization(t *testing.T) {
	as := func(role authz.Role) authz.Guard { return memberOf(map[int64]authz.Role{1: role}) }
	cases := []struct {
		method, target, body string
		min                  authz.Role
		success              int
	}{
		{"GET", "/api/v1/projects/TC/dimensions", "", authz.RoleViewer, 200},
		{"POST", "/api/v1/projects/TC/dimensions", `{"key":"a","name":"A"}`, authz.RoleMaintainer, 201},
		{"PATCH", "/api/v1/projects/TC/dimensions/risk", `{"name":"R"}`, authz.RoleMaintainer, 200},
		{"POST", "/api/v1/projects/TC/dimensions/risk/values", `{"key":"a","name":"A"}`, authz.RoleMaintainer, 201},
		{"PATCH", "/api/v1/projects/TC/dimensions/risk/values/a", `{"name":"A"}`, authz.RoleMaintainer, 200},
	}
	for _, c := range cases {
		assert.Equal(t, http.StatusNotFound, serveAs(memberOf(nil), &stubAPI{}, c.method, c.target, c.body).Code, c.target)
		for r := authz.RoleViewer; r <= authz.RoleMaintainer; r++ {
			want := c.success
			if r < c.min {
				want = http.StatusForbidden
			}
			assert.Equal(t, want, serveAs(as(r), &stubAPI{}, c.method, c.target, c.body).Code, "%s %s as %s", c.method, c.target, r)
		}
	}
}

// Tags and classification travel in the test case body and filter the list.
func TestTestCaseTaxonomyOverHTTP(t *testing.T) {
	api := &stubAPI{}
	rec := serve(api, "GET", "/api/v1/test-cases/153", "")
	assert.Contains(t, rec.Body.String(), `"tags":[],"classification":{}`, "never null")

	serve(api, "POST", "/api/v1/test-cases", `{"title":"t","tags":["Smoke"],"classification":{"risk":"critical"}}`)
	assert.Equal(t, []string{"Smoke"}, api.gotCreate.Tags)
	assert.Equal(t, map[string]string{"risk": "critical"}, api.gotCreate.Classification)

	serve(api, "PATCH", "/api/v1/test-cases/153", `{"tags":[],"classification":{"risk":null,"feature":"pay"}}`)
	require.NotNil(t, api.gotUpdate.Tags)
	assert.Empty(t, *api.gotUpdate.Tags)
	assert.Equal(t, map[string]*string{"risk": nil, "feature": ptr("pay")}, api.gotUpdate.Classification)
	serve(api, "PATCH", "/api/v1/test-cases/153", `{"title":"x"}`)
	assert.Nil(t, api.gotUpdate.Tags, "absent tags are unchanged")

	serve(api, "GET", "/api/v1/test-cases?tag=smoke&classification=risk:critical,feature:pay,risk:critical", "")
	assert.Equal(t, ptr("smoke"), api.gotFilter.Tag)
	assert.Equal(t, []string{"feature:pay", "risk:critical"}, api.gotFilter.Classified, "sorted, without duplicates")
	serve(api, "GET", "/api/v1/test-cases", "")
	assert.Nil(t, api.gotFilter.Tag)
	assert.Nil(t, api.gotFilter.Classified)

	for _, q := range []string{"tag=", "tag=Smoke", "tag=-x", "classification=", "classification=risk", "classification=risk:", "classification=Risk:a",
		"classification=a:1,b:2,c:3,d:4,e:5,f:6,g:7,h:8,i:9,j:10,k:11"} {
		rec := serve(api, "GET", "/api/v1/test-cases?"+q, "")
		assert.Equal(t, http.StatusBadRequest, rec.Code, q)
	}
}
