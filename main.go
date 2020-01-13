package main

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"io/ioutil"
	"math"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/gorilla/sessions"
	dbstorage "github.com/nektro/go.dbstorage"
	etc "github.com/nektro/go.etc"
	"github.com/spf13/pflag"

	. "github.com/nektro/go-util/alias"
	. "github.com/nektro/go-util/util"

	_ "github.com/nektro/dacite/statik"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

const (
	Byte     = 1
	Kilobyte = Byte * 1024
	Megabyte = Kilobyte * 1024
)

var (
	dataRoot string
	config   *Config
	usrMutex = sync.Mutex{}
	imgMutex = sync.Mutex{}
)

func main() {
	Log("Initializing Dacite...")

	flagRoot := pflag.String("storage", "", "Path of root directory for files")
	etc.PreInit()

	//

	etc.Init("dacite", &config, "./portal", saveOAuth2Info)

	if config.Port == 0 {
		config.Port = 8000
	}

	config.Root = findFirstNonEmpty(*flagRoot, config.Root)
	Log("Discovered option:", "--root", config.Root)

	DieOnError(Assert(config.Root != "", "config.json[root] must not be empty!"))

	dataRoot, _ = filepath.Abs(config.Root)
	Log("Saving data to", dataRoot)
	DieOnError(Assert(DoesDirectoryExist(dataRoot), "Directory does not exist!"))

	//

	etc.Database.CreateTableStruct("users", User{})
	etc.Database.CreateTableStruct("images", ImageRow{})

	//

	gracefulStop := make(chan os.Signal)
	signal.Notify(gracefulStop, syscall.SIGTERM)
	signal.Notify(gracefulStop, syscall.SIGINT)

	go func() {
		sig := <-gracefulStop
		Log(F("Caught signal '%+v'", sig))
		Log("Gracefully shutting down...")

		etc.Database.Close()
		Log("Saved database to disk")

		Log("Done!")
		os.Exit(0)
	}()

	//

	p := F("%d", config.Port)

	//

	mw := chainMiddleware(mwAddAttribution)

	//

	http.HandleFunc("/portal", mw(func(w http.ResponseWriter, r *http.Request) {
		_, u, err := pageInit(r, w, http.MethodGet, true, true, false, true)
		if err != nil {
			return
		}
		hashes := queryImagesByUser(u)
		reverse(hashes)
		writePage(r, w, u, "portal", "home", "Home", map[string]interface{}{
			"hashes": hashes,
		})
	}))

	http.HandleFunc("/upload", mw(func(w http.ResponseWriter, r *http.Request) {
		_, u, err := pageInit(r, w, http.MethodGet, true, true, false, true)
		if err != nil {
			return
		}
		writePage(r, w, u, "upload", "upload", "Upload", map[string]interface{}{
			//
		})
	}))

	http.HandleFunc("/p/", mw(func(w http.ResponseWriter, r *http.Request) {
		_, _, err := pageInit(r, w, http.MethodGet, false, false, false, true)
		if err != nil {
			return
		}

		a := strings.Split(r.RequestURI, "/")
		b := a[len(a)-1]
		hd := strings.Join(splitByWidthMake(b, 2), "/")
		fd := F("%s/%s", dataRoot, hd)
		fl, _ := ioutil.ReadDir(fd)

		if len(fl) == 0 {
			http.NotFound(w, r)
			return
		}

		file := fl[0]
		ext := filepath.Ext(file.Name())
		ct := mime.TypeByExtension(ext)

		w.Header().Add("Content-Type", ct)
		w.Header().Add("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Add("ETag", F("\"%s\"", b))
		http.ServeFile(w, r, fd+"/image"+ext)
	}))

	http.HandleFunc("/users", mw(func(w http.ResponseWriter, r *http.Request) {
		_, u, err := pageInit(r, w, http.MethodGet, true, true, true, true)
		if err != nil {
			return
		}
		writePage(r, w, u, "users", "users", "🔨 All Users", map[string]interface{}{
			"users": queryAllUsers(),
		})
	}))

	//

	http.HandleFunc("/b/upload", mw(func(w http.ResponseWriter, r *http.Request) {
		_, u, err := pageInit(r, w, http.MethodPost, true, true, false, false)
		if err != nil {
			return
		}
		fl, fh, err := r.FormFile("image")
		if err != nil {
			writeJson(w, map[string]interface{}{
				"message": err.Error(),
			})
			return
		}

		bytesO, _ := ioutil.ReadAll(fl)

		_, _, err = image.Decode(bytes.NewReader(bytesO))
		if err != nil {
			writeJson(w, map[string]interface{}{
				"message": err.Error(),
			})
			return
		}

		sum := sha256.Sum256(bytesO)
		str := hex.EncodeToString(sum[:])
		original := true

		hd := strings.Join(splitByWidthMake(str, 2), "/")
		ex := filepath.Ext(fh.Filename)
		fd := F("%s/%s", dataRoot, hd)
		fp := F("%s/image%s", fd, ex)
		os.MkdirAll(fd, os.ModePerm)
		if !DoesFileExist(fp) {
			ioutil.WriteFile(fp, bytesO, os.ModePerm)
		}

		if dbstorage.QueryHasRows(etc.Database.Query(false, F("select * from images where hash = '%s' and uploader = %d", str, u.ID))) {
			original = false
		} else {
			imgMutex.Lock()
			id := etc.Database.QueryNextID("images")
			etc.Database.QueryPrepared(true, F("insert into images values (%d, '%s', %d, ?, '%s')", id, str, u.ID, T()), fh.Filename)
			imgMutex.Unlock()
			Log("Added file", str, "by", u.Username)
		}

		writeJson(w, map[string]interface{}{
			"message":  "success",
			"name":     fh.Filename,
			"hash":     str,
			"original": original,
			"url":      FullHost(r) + "/p/" + str,
		})
	}))

	http.HandleFunc("/b/users/update/", mw(func(w http.ResponseWriter, r *http.Request) {
		_, _, err := pageInit(r, w, http.MethodPut, true, true, true, false)
		if err != nil {
			writeJson(w, map[string]interface{}{})
			return
		}
		uid := r.RequestURI[16:]
		if !isInt(uid) {
			writeJson(w, map[string]interface{}{})
			return
		}
		if etc.AssertPostFormValuesExist(r, "key", "value") != nil {
			writeJson(w, map[string]interface{}{})
			return
		}
		k := r.PostForm["key"][0]
		v := r.PostForm["value"][0]
		for true {
			if k == "is_member" || k == "is_admin" {
				if v == "0" || v == "1" {
					break
				}
			}
			writeJson(w, map[string]interface{}{})
			return
		}
		etc.Database.QueryDoUpdate("users", k, v, "id", uid)
		writeJson(w, map[string]interface{}{
			"id":  uid,
			"key": k,
			"val": v,
		})
	}))

	//

	Log("Initialization complete. Starting server on port " + p)
	http.ListenAndServe(":"+p, nil)
}

func checkErr(err error, args ...string) {
	if err != nil {
		fmt.Println("Error")
		fmt.Println(F("%q: %s", err, args))
		debug.PrintStack()
		os.Exit(2)
	}
}

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

func isLoggedInS(sess *sessions.Session) bool {
	_, ok := sess.Values["user"]
	return ok
}

func isLoggedIn(r *http.Request) bool {
	return isLoggedInS(etc.GetSession(r))
}

func pageInit(r *http.Request, w http.ResponseWriter, method string, requireLogin bool, requireMember bool, requireAdmin bool, htmlOut bool) (*sessions.Session, *User, error) {
	if r.Method != method {
		writeResponse(r, w, htmlOut, "Forbidden Method", F("%s is not allowed on this endpoint.", r.Method), "", "")
		return nil, nil, E("bad http method")
	}
	if method == http.MethodPost {
		r.ParseMultipartForm(int64(20 * Megabyte))
	}
	if method == http.MethodPut || method == http.MethodPatch {
		r.ParseForm()
	}
	if !requireLogin {
		return nil, nil, nil
	}

	s := etc.GetSession(r)
	if requireLogin && !isLoggedInS(s) {
		writeResponse(r, w, htmlOut, "Authentication Required", "You must log in to access this site.", "/login", "Please Log In")
		return s, nil, E("not logged in")
	}

	u := queryUserBySnowflake(s.Values["user"].(string))
	if requireMember && !u.IsMember {
		writeResponse(r, w, htmlOut, "Access Forbidden", "You must be a member to view this page.", "", "")
		return s, u, E("not a member")
	}
	if requireAdmin && !u.IsAdmin {
		writeResponse(r, w, htmlOut, "Access Forbidden", "You must be an admin to view this page.", "", "")
		return s, u, E("not an admin")
	}

	return s, u, nil
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

func queryUserBySnowflake(snowflake string) *User {
	rows := etc.Database.QueryDoSelect("users", "snowflake", snowflake)
	if rows.Next() {
		ru := scanUser(rows)
		rows.Close()
		return &ru
	}
	// else
	usrMutex.Lock()
	id := etc.Database.QueryNextID("users")
	etc.Database.QueryPrepared(true, F("insert into users values ('%d', '%s', '%s', 0, 0, '')", id, snowflake, T()))
	if id == 0 {
		etc.Database.QueryDoUpdate("users", "is_member", "1", "id", "0")
		etc.Database.QueryDoUpdate("users", "is_admin", "1", "id", "0")
	}
	usrMutex.Unlock()
	return queryUserBySnowflake(snowflake)
}

func scanUser(rows *sql.Rows) User {
	var u User
	rows.Scan(&u.ID, &u.Snowflake, &u.JoinedOn, &u.IsMember, &u.IsAdmin, &u.Username)
	// &u.Passkey
	return u
}

func saveOAuth2Info(w http.ResponseWriter, r *http.Request, provider string, id string, name string, resp map[string]interface{}) {
	sess := etc.GetSession(r)
	sess.Values["user"] = id
	sess.Save(r, w)
	queryUserBySnowflake(id)
	etc.Database.QueryDoUpdate("users", "username", name, "snowflake", id)
}

func writePage(r *http.Request, w http.ResponseWriter, user *User, hbs string, page string, title string, data map[string]interface{}) {
	etc.WriteHandlebarsFile(r, w, "/_header.hbs", map[string]interface{}{
		"base":  "/",
		"user":  user,
		"page":  page,
		"title": title,
	})
	etc.WriteHandlebarsFile(r, w, F("/%s.hbs", hbs), map[string]interface{}{
		"base":  "/",
		"user":  user,
		"page":  page,
		"title": title,
		"data":  data,
	})
}

func writeJson(w http.ResponseWriter, val interface{}) {
	res, _ := json.Marshal(val)
	w.Header().Add("content-type", "application/json")
	fmt.Fprintln(w, string(res))
}

// from SO
func splitByWidthMake(str string, size int) []string {
	strLength := len(str)
	splitedLength := int(math.Ceil(float64(strLength) / float64(size)))
	splited := make([]string, splitedLength)
	var start, stop int
	for i := 0; i < splitedLength; i += 1 {
		start = i * size
		stop = start + size
		if stop > strLength {
			stop = strLength
		}
		splited[i] = str[start:stop]
	}
	return splited
}

func queryImagesByUser(user *User) []string {
	var res []string
	rows := etc.Database.Query(false, F("select * from images where uploader = %d", user.ID))
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
	rows := etc.Database.Query(false, "select * from users")
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

func findFirstNonEmpty(values ...string) string {
	for _, item := range values {
		if len(item) > 0 {
			return item
		}
	}
	return ""
}
