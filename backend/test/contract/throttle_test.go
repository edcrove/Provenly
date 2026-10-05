//go:build contract

package contract

import (
	"net/http"
	"testing"
)

// TestLoginThrottle: five failed sign-ins lock a username (even with the right password) with a 429 that says for
// how long; other usernames still sign in.
func TestLoginThrottle(t *testing.T) {
	s := fresh(t)
	e := anon(t, s, 1<<20)
	for range 5 {
		e.POST("/api/v1/auth/login").WithJSON(map[string]any{"username": adminUser, "password": "wrong password"}).Expect().Status(http.StatusUnauthorized)
	}
	e.POST("/api/v1/auth/login").WithJSON(map[string]any{"username": "ADMIN", "password": adminPassword}).Expect().
		Status(http.StatusTooManyRequests).JSON(problemOpts).Object().HasValue("code", "too_many_requests").
		HasValue("detail", "too many failed sign-ins for this username; try again in 15 minutes")
	e.POST("/api/v1/auth/login").WithJSON(map[string]any{"username": "someone-else", "password": "x"}).Expect().Status(http.StatusUnauthorized)
}
