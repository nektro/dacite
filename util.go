package main

import (
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"

	"github.com/nektro/go-util/util"
	dbstorage "github.com/nektro/go.dbstorage"
	etc "github.com/nektro/go.etc"
	"github.com/nektro/go.etc/htp"
	"github.com/zeebo/blake3"

	. "github.com/nektro/go-util/alias"
)

// @from https://gist.github.com/gbbr/fa652db0bab132976620bcb7809fd89a
func chainMiddleware(mw ...Middleware) Middleware {
	return func(final http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			last := final
			for i := len(mw) - 1; i >= 0; i-- {
				last = mw[i](last)
			}
			last(w, r)
		}
	}
}

func mwAddAttribution(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Server", "nektro/dacite")
		next.ServeHTTP(w, r)
	}
}

func pageInit(c *htp.Controller, r *http.Request, w http.ResponseWriter, method string, requireLogin bool, requireMember bool, requireAdmin bool, htmlOut bool) (*User, error) {
	if r.Method != method {
		writeResponse(r, w, htmlOut, "Forbidden Method", F("%s is not allowed on this endpoint.", r.Method), "", "")
		return nil, E("bad http method")
	}
	if method == http.MethodPost {
		r.ParseMultipartForm(int64(config.MaxFileSize * Megabyte))
	}
	if method == http.MethodPut {
		r.ParseForm()
	}
	if !requireLogin {
		return nil, nil
	}

	s := etc.JWTGetClaims(c, r)
	sp := strings.SplitN(s["sub"].(string), "\n", 2)

	u := queryUserBySnowflake(sp[0], sp[1])
	if requireMember && !u.IsMember {
		writeResponse(r, w, htmlOut, "Access Forbidden", "You must be a member to view this page.", "", "")
		return u, E("not a member")
	}
	if requireAdmin && !u.IsAdmin {
		writeResponse(r, w, htmlOut, "Access Forbidden", "You must be an admin to view this page.", "", "")
		return u, E("not an admin")
	}

	return u, nil
}

func writeResponse(r *http.Request, w http.ResponseWriter, htmlOut bool, title string, message string, url string, link string) {
	data := map[string]interface{}{
		"base":    "/",
		"title":   title,
		"message": message,
		"url":     url,
		"link":    link,
	}
	if htmlOut {
		etc.WriteHandlebarsFile(r, w, "/response.hbs", data)
	} else {
		writeJson(w, data)
	}
}

func queryUserBySnowflake(provider, snowflake string) *User {
	rows := db.Build().Se("*").Fr("users").Wh("snowflake", snowflake).Exe()
	if rows.Next() {
		ru := scanUser(rows)
		rows.Close()
		return &ru
	}
	// else
	dbstorage.InsertsLock.Lock()
	id := db.QueryNextID("users")
	adm := id == 1
	db.Build().Ins("users", id, snowflake, T(), adm, adm, "", provider).Exe()
	dbstorage.InsertsLock.Unlock()
	return queryUserBySnowflake(provider, snowflake)
}

func scanUser(rows *sql.Rows) User {
	var u User
	rows.Scan(&u.ID, &u.Snowflake, &u.JoinedOn, &u.IsMember, &u.IsAdmin, &u.Username, &u.Provider)
	return u
}

func saveOAuth2Info(w http.ResponseWriter, r *http.Request, provider string, id string, name string, resp map[string]interface{}) {
	etc.JWTSet(w, provider+"\n"+id)
	if dbstorage.QueryHasRows(db.Build().Se("*").Fr("users").WR("provider", "IS", "NULL", true).Wh("snowflake", id).Exe()) {
		util.Log("update:", "user:", "provider:", provider)
		db.Build().Up("users", "provider", provider).WR("provider", "IS", "NULL", true).Wh("snowflake", id).Exe()
	}
	queryUserBySnowflake(provider, id)
	db.Build().Up("users", "username", name).Wh("snowflake", id).Exe()
}

func writePage(r *http.Request, w http.ResponseWriter, user *User, hbs string, page string, title string, data map[string]interface{}) {
	ctx := map[string]interface{}{
		"version": Version,
		"base":    "/",
		"user":    user,
		"page":    page,
		"title":   title,
	}
	etc.WriteHandlebarsFile(r, w, "/_header.hbs", ctx)
	ctx["data"] = data
	etc.WriteHandlebarsFile(r, w, F("/%s.hbs", hbs), ctx)
}

func writeJson(w http.ResponseWriter, val interface{}) {
	res, _ := json.Marshal(val)
	w.Header().Add("content-type", "application/json")
	fmt.Fprintln(w, string(res))
}

func splitByWidthMake(str string, size int, n int) []string {
	strLength := len(str)
	splitedLength := int(math.Ceil(float64(strLength) / float64(size)))
	splited := []string{}
	var start, stop, count int
	for i := 0; i < splitedLength; i++ {
		if n > 0 && count == n {
			splited = append(splited, str[start:])
			break
		}
		start = i * size
		stop = start + size
		if stop > strLength {
			stop = strLength
		}
		splited = append(splited, str[start:stop])
		count++
	}
	return splited
}

func queryImagesByUser(user *User) []string {
	var res []string
	rows := db.Build().Se("*").Fr("images").Wh("uploader", strconv.Itoa(user.ID)).Exe()
	for rows.Next() {
		res = append(res, scanImage(rows).Hash)
	}
	rows.Close()
	return res
}

func scanImage(rows *sql.Rows) ImageRow {
	var i ImageRow
	rows.Scan(&i.ID, &i.Hash, &i.Uploader, &i.Name, &i.AddedOn)
	return i
}

func reverse(a []string) {
	for i := len(a)/2 - 1; i >= 0; i-- {
		opp := len(a) - 1 - i
		a[i], a[opp] = a[opp], a[i]
	}
}

func queryAllUsers() []User {
	var res []User
	rows := db.Build().Se("*").Fr("users").Exe()
	for rows.Next() {
		res = append(res, scanUser(rows))
	}
	rows.Close()
	return res
}

func isInt(x string) bool {
	_, err := strconv.ParseInt(x, 10, 32)
	return err == nil
}

func hashBytes(ba []byte) string {
	if config.ImgAlgo == "zeebo/blake3" {
		h := blake3.New()
		h.Write(ba)
		return hex.EncodeToString(h.Sum([]byte{}))
	}
	return util.Hash(config.ImgAlgo, ba)
}
