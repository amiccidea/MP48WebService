package main

import (
	"net/http"
)

func logoutHandler(w http.ResponseWriter, r *http.Request) {
	session, _ := store.Get(r, "portal-session")
	session.Values["authenticated"] = false
	session.Values["username"] = ""
	session.Values["is_admin"] = false
	session.Options.MaxAge = -1
	session.Save(r, w)
	http.Redirect(w, r, "/login", http.StatusFound)
}
