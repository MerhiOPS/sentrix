package httpapi

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"

	"github.com/MerhiOPS/sentrix/internal/domain"
)

// CookieName is the session cookie.
const CookieName = "sentrix_session"

type ctxKey struct{}

// sessionFrom returns the authenticated session placed by requireSession.
func sessionFrom(ctx context.Context) domain.Session {
	s, _ := ctx.Value(ctxKey{}).(domain.Session)
	return s
}

// scopeFrom builds the tenant scope from the session — never from input.
func scopeFrom(ctx context.Context) (domain.Scope, error) {
	return domain.NewScope(sessionFrom(ctx).Tenant)
}

// requireSession authenticates the cookie and enforces a minimum role.
func (a *API) requireSession(role domain.Role, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(CookieName)
		if err != nil {
			writeError(w, r, fmt.Errorf("%w: not logged in", domain.ErrUnauthenticated))
			return
		}
		token, err := base64.RawURLEncoding.DecodeString(c.Value)
		if err != nil {
			writeError(w, r, fmt.Errorf("%w: malformed session", domain.ErrUnauthenticated))
			return
		}
		sess, err := a.cfg.Sessions.Authenticate(r.Context(), token)
		if err != nil {
			writeError(w, r, err)
			return
		}
		if !sess.Role.Can(role) {
			writeError(w, r, fmt.Errorf("%w: requires %s", domain.ErrForbidden, role))
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, sess)))
	}
}

func sessionCookie(token []byte) *http.Cookie {
	return &http.Cookie{
		Name: CookieName, Value: base64.RawURLEncoding.EncodeToString(token), Path: "/",
		MaxAge: int(domain.SessionAbsolute.Seconds()), HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode,
	}
}

func clearedCookie() *http.Cookie {
	return &http.Cookie{Name: CookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: true, SameSite: http.SameSiteStrictMode}
}
