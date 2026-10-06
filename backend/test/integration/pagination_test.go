//go:build integration

package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/edcrove/provenly/backend/internal/app"
)

func TestListPages(t *testing.T) {
	t.Run("BE-INT-072_project_lists_page_count_over_every_page_and_pickers_search_on_the_server", func(t *testing.T) {
		s, _ := fresh(t)
		require.NoError(t, s.Identity.Bootstrap(context.Background(), "admin", "correct horse"))
		sess, err := s.Identity.Login(context.Background(), "admin", "correct horse")
		require.NoError(t, err)
		srv := httptest.NewServer(app.NewHandler(s, 1<<20))
		defer srv.Close()
		do := func(method, path, body string) map[string]any {
			req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+sess.Token)
			req.Header.Set("Content-Type", "application/json")
			res, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer res.Body.Close()
			require.Less(t, res.StatusCode, 300, "%s %s", method, path)
			var out map[string]any
			_ = json.NewDecoder(res.Body).Decode(&out)
			return out
		}
		// 105 test cases: more than a picker's page.
		var first map[string]any
		for i := 1; i <= 105; i++ {
			tc := do("POST", "/api/v1/test-cases", fmt.Sprintf(`{"title":"checkout step %03d","project":"TC"}`, i))
			if i == 1 {
				first = tc
			}
		}
		do("POST", "/api/v1/test-cases", `{"title":"Refund 50% off","project":"TC"}`)
		id := fmt.Sprint(int64(first["id"].(float64)))
		// 23 requirements, one covered by the first test case: more than a page of 20.
		for i := 1; i <= 23; i++ {
			req := do("POST", "/api/v1/projects/TC/requirements", fmt.Sprintf(`{"title":"requirement %d"}`, i))
			if i == 1 {
				do("PUT", fmt.Sprintf("/api/v1/projects/TC/requirements/%d/test-cases", int64(req["id"].(float64))), `{"testCaseIds":[`+id+`]}`)
			}
		}
		page2 := do("GET", "/api/v1/projects/TC/requirements?page=2", "")
		assert.Equal(t, float64(23), page2["totalItems"])
		assert.Equal(t, float64(2), page2["totalPages"])
		assert.Len(t, page2["items"], 3)
		assert.Equal(t, map[string]any{"uncovered": float64(22), "not_run": float64(1)}, page2["coverageCounts"],
			"the counts cover every page, not only this one")
		covered := do("GET", "/api/v1/projects/TC/requirements?testCase="+id, "")
		cases := covered["items"].([]any)[0].(map[string]any)["coverage"].(map[string]any)["testCases"].([]any)
		assert.Equal(t, first["key"], cases[0].(map[string]any)["testCaseKey"], "a linked test case reads with its key")

		search := func(q string) map[string]any {
			return do("GET", "/api/v1/test-cases?project=TC&pageSize=50&q="+url.QueryEscape(q), "")
		}
		assert.Equal(t, float64(105), search("Checkout STEP")["totalItems"], "any case")
		assert.Equal(t, float64(1), search("step 042")["totalItems"])
		assert.Equal(t, float64(1), search("50%")["totalItems"], "a literal percent sign")
		assert.Equal(t, float64(0), search("50_")["totalItems"], "a literal underscore")
		byKey := search(first["key"].(string))
		assert.Equal(t, float64(1), byKey["totalItems"])
		assert.Equal(t, first["key"], byKey["items"].([]any)[0].(map[string]any)["key"])
		assert.Equal(t, float64(1), search(strings.ToLower(first["key"].(string)))["totalItems"])

		for _, path := range []string{"suites", "issues", "webhooks", "dimensions"} {
			page := do("GET", "/api/v1/projects/TC/"+path+"?pageSize=1", "")
			assert.Contains(t, page, "totalPages", path)
			assert.Equal(t, float64(1), page["pageSize"], path)
		}
		dims := do("GET", "/api/v1/projects/TC/dimensions?pageSize=2&page=2", "")
		assert.Len(t, dims["items"], 2, "the built-in dimensions, a page at a time")
	})
}
