//go:build contract

package contract

import (
	"net/http"
	"strings"
	"testing"
)

// TestListPagination: the project lists answer the page envelope and validate page and pageSize; requirements and
// issues count over every page; the test case list searches with ?q= (DEC-78).
func TestListPagination(t *testing.T) {
	s := fresh(t)
	e := api(t, s, 1<<20)
	for _, path := range []string{"suites", "requirements", "issues", "webhooks", "dimensions"} {
		page := e.GET("/api/v1/projects/TC/"+path).WithQuery("pageSize", 5).Expect().Status(http.StatusOK).JSON().Object()
		page.HasValue("page", 1).HasValue("pageSize", 5).ContainsKey("totalItems").ContainsKey("totalPages").ContainsKey("items")
		for _, q := range []string{"page=0", "pageSize=101", "pageSize=", "page=x"} {
			e.GET("/api/v1/projects/TC/" + path).WithQueryString(q).Expect().Status(http.StatusBadRequest)
		}
	}
	e.GET("/api/v1/projects/TC/requirements").Expect().Status(http.StatusOK).JSON().Object().ContainsKey("coverageCounts")
	e.GET("/api/v1/projects/TC/issues").Expect().Status(http.StatusOK).JSON().Object().ContainsKey("verificationCounts")

	e.POST("/api/v1/test-cases").WithJSON(map[string]any{"title": "Refund by card"}).Expect().Status(http.StatusCreated)
	e.GET("/api/v1/test-cases").WithQuery("q", "refund").Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 1)
	e.GET("/api/v1/test-cases").WithQuery("q", "TC-1").Expect().Status(http.StatusOK).JSON().Object().HasValue("totalItems", 1)
	for _, q := range []string{"q=", "q=%20", "q=" + strings.Repeat("x", 201), "q=a%00b"} {
		e.GET("/api/v1/test-cases").WithQueryString(q).Expect().Status(http.StatusBadRequest)
	}
}
