// Link-account is the OAuth flow for an app with its own user accounts: each
// of your users links their STX account once, and from then on your server
// reads their STX account for them, refreshing tokens as needed. See
// https://docs.stxapp.io/oauth/connect-your-stx-account/.
//
//   - /connect?user=alice sends your user to STX to sign in and approve your
//     app (authorization code with PKCE).
//   - /callback stores their tokens under your own user id.
//   - /member?user=alice reads the linked member's profile and balance.
//   - POST /unlink?user=alice revokes the grant and forgets the tokens.
//
// Your app's client id, secret and redirect URI come from STX when you
// register it. The secret and every token stay on this server. Tokens are
// kept per user in stx-tokens.json, so a restart keeps every link; a real
// app keeps them encrypted in its database, and takes the user from its own
// login session instead of ?user=.
//
//	STX_HOST=demo.stxapp.io STX_OAUTH_CLIENT_ID=... STX_OAUTH_CLIENT_SECRET=... \
//	  STX_OAUTH_REDIRECT_URI=http://127.0.0.1:8787/callback go run ./examples/link-account
//
// Then open http://127.0.0.1:8787/connect?user=alice in a browser.
package main

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	stx "github.com/stxapp/stx-go"
	"github.com/stxapp/stx-go/oauth"
)

// scopes is the access the app asks each member for. Add
// oauth.ScopeOrdersWrite to trade for them; a write scope needs your
// server's addresses on the app's IP allow-list.
var scopes = []string{oauth.ScopeProfileRead, oauth.ScopeBalanceRead}

func main() {
	app, err := oauth.New(oauth.ConfigFromEnv())
	if err != nil {
		log.Fatal(err)
	}
	tokens := oauth.NewFileTokenStore("stx-tokens.json")
	pending := &oauth.MemoryPendingStore{}

	mux := http.NewServeMux()

	// Send your signed-in user to STX to approve the link.
	mux.HandleFunc("GET /connect", func(w http.ResponseWriter, r *http.Request) {
		user := r.URL.Query().Get("user")
		if user == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user_required"})
			return
		}
		auth, err := app.Begin(r.Context(), pending, oauth.BeginParams{
			AuthParams: oauth.AuthParams{Scopes: scopes},
			Data:       map[string]string{"user": user},
		})
		if err != nil {
			writeError(w, err)
			return
		}
		http.Redirect(w, r, auth.URL, http.StatusFound)
	})

	// STX sends the user back here; store their tokens under your user id.
	mux.HandleFunc("GET /callback", func(w http.ResponseWriter, r *http.Request) {
		done, err := app.Complete(r.Context(), pending, r.URL.Query(), oauth.RedeemOptions{
			Store:         tokens,
			MemberKeyFunc: func(data map[string]string) string { return data["user"] },
		})
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"linked": done.MemberKey, "scopes": done.Scopes})
	})

	// Read the linked member's account. The client refreshes the tokens
	// before they expire and stores the rotated refresh token.
	mux.HandleFunc("GET /member", func(w http.ResponseWriter, r *http.Request) {
		member, err := app.MemberClient(tokens, r.URL.Query().Get("user"), nil)
		if err != nil {
			writeError(w, err)
			return
		}
		me, err := member.Account.Me(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		balance, err := member.Account.Balance(r.Context())
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"me": me, "balance": balance})
	})

	// Revoke the grant at STX and forget the tokens.
	mux.HandleFunc("POST /unlink", func(w http.ResponseWriter, r *http.Request) {
		revoked, err := app.Unlink(r.Context(), tokens, r.URL.Query().Get("user"))
		if err != nil {
			writeError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"revoked": revoked})
	})

	log.Println("listening on http://127.0.0.1:8787")
	log.Fatal(http.ListenAndServe("127.0.0.1:8787", mux))
}

// writeError tells the browser what to do next: link again, finish
// verifying the STX account, accept new terms, or show the reason.
func writeError(w http.ResponseWriter, err error) {
	var apiErr *stx.APIError
	var oauthErr *oauth.Error
	switch {
	case errors.Is(err, oauth.ErrNotConnected), errors.Is(err, oauth.ErrGrantRevoked):
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "link_required"})
	case errors.Is(err, oauth.ErrAccountPending):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "account_pending"})
	case errors.Is(err, oauth.ErrAccessDenied):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "access_denied"})
	case errors.Is(err, stx.ErrTermsRequired) && errors.As(err, &apiErr):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "terms_required", "terms_url": oauth.TermsURLWithReturn(apiErr.TermsURL, "http://127.0.0.1:8787/")})
	case errors.As(err, &oauthErr):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": oauthErr.Code, "error_description": oauthErr.Description})
	case errors.As(err, &apiErr):
		writeJSON(w, apiErr.StatusCode, map[string]string{"error": apiErr.Message})
	default:
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
