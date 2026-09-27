package auth

import (
	"net/http"
	"strconv"
)

// RequireLevel guards a handler so only users whose RBACLevel is >= min may
// pass. Unauthenticated requests are sent to /login; authenticated-but-
// under-level requests get 403. This mirrors the plan's Phase-to-level
// table (M2), enforced per-endpoint rather than trusted from the UI.
func RequireLevel(min int, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := CurrentUser(r)
		if !ok || user == nil {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if user.RBACLevel < min {
			http.Error(w, "권한이 부족합니다 (필요 레벨: "+strconv.Itoa(min)+", 보유 레벨: "+strconv.Itoa(user.RBACLevel)+")", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}
