package auth

import (
	"net/http"
	"time"
)

const (
	SessionCookie = "isane_session"
	SessionTTL    = 90 * 24 * time.Hour
	SessionMaxAge = 90 * 24 * time.Hour
)

func SetSession(w http.ResponseWriter, raw string, expires time.Time, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    raw,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		Secure:   secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func ClearSession(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Secure:   secure,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
}

func ReadSession(r *http.Request) (string, bool) {
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return "", false
	}
	return c.Value, true
}
