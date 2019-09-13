package main

import (
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
	"github.com/mitchellh/go-homedir"
	"github.com/nektro/go-util/sqlite"
	"github.com/nektro/go-util/types"
	etc "github.com/nektro/go.etc"
	oauth2 "github.com/nektro/go.oauth2"

	. "github.com/nektro/go-util/alias"
	. "github.com/nektro/go-util/util"
)

const (
	Byte     = 1
	Kilobyte = Byte * 1024
	Megabyte = Kilobyte * 1024
)

var (
	dataRoot string
	config   *Config
	database *sqlite.DB
	usrMutex = sync.Mutex{}
	imgMutex = sync.Mutex{}
)

func main() {
	Log("Initializing Dacite...")

	//

	hd, _ := homedir.Dir()
	opRoot := hd + "/.config/dacite"
	configDir, _ := filepath.Abs(opRoot)
	Log("Reading configuration from", configDir)
	DieOnError(Assert(DoesFileExist(configDir), "Please make sure the directory exists!"))
	etc.InitConfig(configDir+"/config.json", &config)

	if config.Port == 0 {
		config.Port = 8000
	}

	DieOnError(Assert(config.Auth != "", "config.json[auth] must not be empty!"))
	DieOnError(Assert(config.ID != "", "config.json[id] must not be empty!"))
	DieOnError(Assert(config.Secret != "", "config.json[secret] must not be empty!"))
	DieOnError(Assert(config.Root != "", "config.json[root] must not be empty!"))

	dataRoot, _ = filepath.Abs(config.Root)
	Log("Saving data to", dataRoot)
	DieOnError(Assert(DoesDirectoryExist(dataRoot), "Directory does not exist!"))

	provider := oauth2.ProviderIDMap[config.Auth]

	//

	database = sqlite.Connect(configDir)
	checkErr(database.Ping())

	database.CreateTableStruct("users", User{})
	database.CreateTableStruct("images", ImageRow{})

	//

	gracefulStop := make(chan os.Signal)
	signal.Notify(gracefulStop, syscall.SIGTERM)
	signal.Notify(gracefulStop, syscall.SIGINT)

	go func() {
		sig := <-gracefulStop
		Log(F("Caught signal '%+v'", sig))
		Log("Gracefully shutting down...")

		database.Close()
		Log("Saved database to disk")

		Log("Done!")
		os.Exit(0)
	}()

	//

	etc.SetSessionName("session_dacite_test")
	p := F("%d", config.Port)
	dirs := []http.FileSystem{}

	//

	mw := chainMiddleware(mwAddAttribution)
	dirs = append(dirs, http.Dir("www"))
	wwFFS := types.MultiplexFileSystem{dirs}

	//

	http.HandleFunc("/", mw(http.FileServer(wwFFS).ServeHTTP))
	http.HandleFunc("/login", mw(oauth2.HandleOAuthLogin(isLoggedIn, "./portal", provider, config.ID)))
	http.HandleFunc("/callback", mw(oauth2.HandleOAuthCallback(provider, config.ID, config.Secret, saveOAuth2Info, "./portal")))

	http.HandleFunc("/portal", mw(func(w http.ResponseWriter, r *http.Request) {
		_, u, err := pageInit(r, w, http.MethodGet, true, true, false, true)
		if err != nil {
			return
		}
		hashes := queryImagesByUser(u)
		reverse(hashes)
		writePage(r, w, u, "home", "home", "Home", map[string]interface{}{
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

		bytes, _ := ioutil.ReadAll(fl)
		sum := sha256.Sum256(bytes)
		str := hex.EncodeToString(sum[:])
		original := true

		_, _, err = image.Decode(fl)
		if err != nil {
			writeJson(w, map[string]interface{}{
				"message": err.Error(),
			})
			return
		}

		hd := strings.Join(splitByWidthMake(str, 2), "/")
		ex := filepath.Ext(fh.Filename)
		fd := F("%s/%s", dataRoot, hd)
		fp := F("%s/image%s", fd, ex)
		os.MkdirAll(fd, os.ModePerm)
		if !DoesFileExist(fp) {
			ioutil.WriteFile(fp, bytes, os.ModePerm)
		}

		q := database.Query(false, F("select * from images where hash = '%s' and uploader = %d", str, u.ID))
		n := q.Next()
		q.Close()
		if n {
			original = false
		} else {
			imgMutex.Lock()
			id := database.QueryNextID("images")
			database.QueryPrepared(true, F("insert into images values (%d, '%s', %d, ?, '%s')", id, str, u.ID, T()), fh.Filename)
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
		database.QueryDoUpdate("users", k, v, "id", uid)
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
		etc.WriteHandlebarsFile(r, w, "./hbs/response.hbs", data)
	} else {
		writeJson(w, data)
	}
}

func queryUserBySnowflake(snowflake string) *User {
	rows := database.QueryDoSelect("users", "snowflake", snowflake)
	if rows.Next() {
		ru := scanUser(rows)
		rows.Close()
		return &ru
	}
	// else
	usrMutex.Lock()
	id := database.QueryNextID("users")
	database.QueryPrepared(true, F("insert into users values ('%d', '%s', '%s', 0, 0, '')", id, snowflake, T()))
	if id == 0 {
		database.QueryDoUpdate("users", "is_member", "1", "id", "0")
		database.QueryDoUpdate("users", "is_admin", "1", "id", "0")
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

func saveOAuth2Info(w http.ResponseWriter, r *http.Request, provider string, id string, name string) {
	sess := etc.GetSession(r)
	sess.Values["user"] = id
	sess.Save(r, w)
	queryUserBySnowflake(id)
	database.QueryDoUpdate("users", "username", name, "snowflake", id)
}

func writePage(r *http.Request, w http.ResponseWriter, user *User, hbs string, page string, title string, data map[string]interface{}) {
	etc.WriteHandlebarsFile(r, w, "./hbs/_header.hbs", map[string]interface{}{
		"base":  "/",
		"user":  user,
		"page":  page,
		"title": title,
	})
	etc.WriteHandlebarsFile(r, w, F("./hbs/%s.hbs", hbs), map[string]interface{}{
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
	rows := database.Query(false, F("select * from images where uploader = %d", user.ID))
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
	rows := database.Query(false, "select * from users")
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
