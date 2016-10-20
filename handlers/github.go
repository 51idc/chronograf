package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/google/go-github/github"
	"github.com/influxdata/chronograf"
	"golang.org/x/oauth2"
	ogh "golang.org/x/oauth2/github"
)

const (
	DefaultCookieName     = "session"
	DefaultCookieDuration = time.Hour * 24 * 30
)

// Cookie represents the location and expiration time of new cookies.
type Cookie struct {
	Name     string
	Duration time.Duration
}

// NewCookie creates a Cookie with DefaultCookieName and DefaultCookieDuration
func NewCookie() Cookie {
	return Cookie{
		Name:     DefaultCookieName,
		Duration: DefaultCookieDuration,
	}
}

// Github provides OAuth Login and Callback handlers. Callback will set
// an authentication cookie.  This cookie's value is a JWT containing
// the user's primary Github email address.
type Github struct {
	Cookie        Cookie
	Authenticator chronograf.Authenticator
	ClientID      string
	ClientSecret  string
	Scopes        []string
	SuccessURL    string // SuccessURL is redirect location after successful authorization
	FailureURL    string // FailureURL is redirect location after authorization failure
	Now           func() time.Time
	Logger        chronograf.Logger
}

// NewGithub constructs a Github with default cookie behavior and scopes.
func NewGithub(clientID, clientSecret, successURL, failureURL string, auth chronograf.Authenticator, log chronograf.Logger) Github {
	return Github{
		ClientID:      clientID,
		ClientSecret:  clientSecret,
		Cookie:        NewCookie(),
		Scopes:        []string{"user:email"},
		SuccessURL:    successURL,
		FailureURL:    failureURL,
		Authenticator: auth,
		Now:           time.Now,
		Logger:        log,
	}
}

func (g *Github) config() *oauth2.Config {
	return &oauth2.Config{
		ClientID:     g.ClientID,
		ClientSecret: g.ClientSecret,
		Scopes:       g.Scopes,
		Endpoint:     ogh.Endpoint,
	}
}

// Login returns a handler that redirects to Github's OAuth login.
// Uses JWT with a random string as the state validation method.
// JWTs are used because they can be validated without storing
// state.
func (g *Github) Login() http.Handler {
	conf := g.config()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// We are creating a token with an encoded random string to prevent CSRF attacks
		// This token will be validated during the OAuth callback.
		// We'll give our users 10 minutes from this point to type in their github password.
		// If the callback is not received within 10 minutes, then authorization will fail.
		csrf := randomString(32) // 32 is not important... just long
		state, err := g.Authenticator.Token(r.Context(), chronograf.Principal(csrf), 10*time.Minute)
		// This is likely an internal server error
		if err != nil {
			g.Logger.
				WithField("component", "auth").
				WithField("remote_addr", r.RemoteAddr).
				WithField("method", r.Method).
				WithField("url", r.URL).
				Error("Internal authentication error: ", err.Error())
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		url := conf.AuthCodeURL(state, oauth2.AccessTypeOnline)
		http.Redirect(w, r, url, http.StatusTemporaryRedirect)
	})
}

// Logout will expire our authentication cookie and redirect to the SuccessURL
func (g *Github) Logout() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deleteCookie := http.Cookie{
			Name:     g.Cookie.Name,
			Value:    "none",
			Expires:  g.Now().Add(-1 * time.Hour),
			HttpOnly: true,
			Path:     "/",
		}
		http.SetCookie(w, &deleteCookie)
		http.Redirect(w, r, g.SuccessURL, http.StatusTemporaryRedirect)
	})
}

// Callback used by github callback after authorization is granted.  If
// granted, Callback will set a cookie with a month-long expiration.  The
// value of the cookie is a JWT because the JWT can be validated without
// the need for saving state. The JWT contains the Github user's primary
// email address.
func (g *Github) Callback() http.Handler {
	conf := g.config()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log := g.Logger.
			WithField("component", "auth").
			WithField("remote_addr", r.RemoteAddr).
			WithField("method", r.Method).
			WithField("url", r.URL)

		state := r.FormValue("state")
		// Check if the OAuth state token is valid to prevent CSRF
		_, err := g.Authenticator.Authenticate(r.Context(), state)
		if err != nil {
			log.Error("Invalid OAuth state received: ", err.Error())
			http.Redirect(w, r, g.FailureURL, http.StatusTemporaryRedirect)
			return
		}

		code := r.FormValue("code")
		token, err := conf.Exchange(r.Context(), code)
		if err != nil {
			log.Error("Unable to exchange code for token ", err.Error())
			http.Redirect(w, r, g.FailureURL, http.StatusTemporaryRedirect)
			return
		}

		oauthClient := conf.Client(r.Context(), token)
		client := github.NewClient(oauthClient)

		emails, resp, err := client.Users.ListEmails(nil)
		if err != nil {
			switch resp.StatusCode {
			case http.StatusUnauthorized, http.StatusForbidden:
				log.Error("OAuth access to email address forbidden ", err.Error())
			default:
				log.Error("Unable to retrieve Github email ", err.Error())
			}

			http.Redirect(w, r, g.FailureURL, http.StatusTemporaryRedirect)
			return
		}

		email, err := primaryEmail(emails)
		if err != nil {
			log.Error("Unable to retrieve primary Github email ", err.Error())
			http.Redirect(w, r, g.FailureURL, http.StatusTemporaryRedirect)
		}

		// We create an auth token that will be used by all other endpoints to validate the principal has a claim
		authToken, err := g.Authenticator.Token(r.Context(), chronograf.Principal(email), g.Cookie.Duration)
		if err != nil {
			log.Error("Unable to create cookie auth token ", err.Error())
			http.Redirect(w, r, g.FailureURL, http.StatusTemporaryRedirect)
		}

		expireCookie := time.Now().Add(g.Cookie.Duration)
		cookie := http.Cookie{
			Name:     g.Cookie.Name,
			Value:    authToken,
			Expires:  expireCookie,
			HttpOnly: true,
			Path:     "/",
		}
		log.Info("User ", email, " is authenticated")
		http.SetCookie(w, &cookie)
		http.Redirect(w, r, g.SuccessURL, http.StatusTemporaryRedirect)
	})
}

func randomString(length int) string {
	k := make([]byte, length)
	if _, err := io.ReadFull(rand.Reader, k); err != nil {
		return ""
	}
	return base64.StdEncoding.EncodeToString(k)
}

func primaryEmail(emails []*github.UserEmail) (string, error) {
	for _, m := range emails {
		if m != nil && m.Primary != nil && m.Verified != nil && m.Email != nil {
			return *m.Email, nil
		}
	}
	return "", errors.New("No primary email address")
}
