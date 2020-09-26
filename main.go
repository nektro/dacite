package main

import (
	"bytes"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"io/ioutil"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nektro/go-util/util"
	"github.com/nektro/go-util/vflag"
	dbstorage "github.com/nektro/go.dbstorage"
	etc "github.com/nektro/go.etc"
	"github.com/nektro/go.etc/htp"
	"github.com/zeebo/blake3"

	_ "github.com/nektro/dacite/statik"
	. "github.com/nektro/go-util/alias"

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
	Version       = "vMASTER"
	dataRoot      string
	config        = new(Config)
	compressables = []string{".png", ".jpg", ".jpeg"}
	db            dbstorage.Database
)

// http://localhost/

func main() {
	etc.AppID = "dacite"
	etc.Version = Version
	etc.FixBareVersion()
	util.Log("Initializing Dacite " + Version + "...")

	vflag.StringVar(&config.Root, "root", "", "Path of root directory for files.")
	vflag.StringVar(&config.ImgAlgo, "algo", "SHA1", "")
	vflag.BoolVar(&config.Public, "public", false, "If set to true, anyone who logs in will be able to upload files.")
	vflag.IntVar(&config.MaxFileSize, "max-file-size", 20, "Size in MB to limit user uploads to.")
	vflag.IntVar(&config.MaxFolderDepth, "max-folder-depth", 6, "Max depth of folders to make in /data. 0 for infinite.")
	etc.PreInit()

	etc.Init(&config, "./portal", saveOAuth2Info)

	//

	util.DieOnError(util.Assert(config.Root != "", "config.json[root] must not be empty!"))

	dataRoot, _ = filepath.Abs(config.Root)
	util.Log("Saving data to", dataRoot)
	util.DieOnError(util.Assert(util.DoesDirectoryExist(dataRoot), "Directory does not exist!"))

	util.DieOnError(util.Assert(len(util.Hash(config.ImgAlgo, []byte("hello"))) > 0, "Bad --algo value: "+config.ImgAlgo))

	//

	db = etc.Database
	db.CreateTableStruct("users", User{})
	db.CreateTableStruct("images", ImageRow{})

	//

	util.RunOnClose(func() {
		util.Log("Gracefully shutting down...")

		util.Log("Saving database to disk")
		db.Close()

		util.Log("Done!")
	})

	etc.HtpErrCb = func(r *http.Request, w http.ResponseWriter, good bool, code int, msg string) {
		w.WriteHeader(code)
		writeJson(w, map[string]string{
			"message": msg,
		})
	}

	//

	mw := chainMiddleware(mwAddAttribution)

	//

	htp.Register("/portal", "GET", mw(func(w http.ResponseWriter, r *http.Request) {
		c := htp.GetController(r)
		u, err := pageInit(c, r, w, http.MethodGet, true, true, false, true)
		if err != nil {
			return
		}
		hashes := queryImagesByUser(u)
		reverse(hashes)
		writePage(r, w, u, "portal", "home", "Home", map[string]interface{}{
			"hashes": hashes,
		})
	}))

	htp.Register("/upload", "GET", mw(func(w http.ResponseWriter, r *http.Request) {
		c := htp.GetController(r)
		u, err := pageInit(c, r, w, http.MethodGet, true, true, false, true)
		if err != nil {
			return
		}
		writePage(r, w, u, "upload", "upload", "Upload", map[string]interface{}{
			//
		})
	}))

	htp.Register("/p/{hash:[0-9a-f]+}", "GET", mw(func(w http.ResponseWriter, r *http.Request) {
		c := htp.GetController(r)
		_, err := pageInit(c, r, w, http.MethodGet, false, false, false, true)
		if err != nil {
			return
		}

		a := strings.Split(r.URL.Path, "/")
		b := a[len(a)-1]
		hd := strings.Join(splitByWidthMake(b, 2, config.MaxFolderDepth), "/")
		fd := F("%s/%s", dataRoot, hd)
		fl, _ := ioutil.ReadDir(fd)

		if len(fl) == 0 {
			http.NotFound(w, r)
			return
		}

		file := fl[0]
		ext := filepath.Ext(file.Name())

		w.Header().Add("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Add("ETag", F("\"%s\"", b))
		http.ServeFile(w, r, fd+"/image"+ext)
	}))

	htp.Register("/users", "GET", mw(func(w http.ResponseWriter, r *http.Request) {
		c := htp.GetController(r)
		u, err := pageInit(c, r, w, http.MethodGet, true, true, true, true)
		if err != nil {
			return
		}
		writePage(r, w, u, "users", "users", "🔨 All Users", map[string]interface{}{
			"users": queryAllUsers(),
		})
	}))

	//

	htp.Register("/b/upload", "POST", mw(func(w http.ResponseWriter, r *http.Request) {
		c := htp.GetController(r)
		u, err := pageInit(c, r, w, http.MethodPost, true, true, false, false)
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

		str := hashBytes(bytesO)
		if len(str) == 0 {
			writeJson(w, map[string]interface{}{
				"message": "hash error",
			})
			return
		}
		original := true

		hd := strings.Join(splitByWidthMake(str, 2, config.MaxFolderDepth), "/")
		ex := filepath.Ext(fh.Filename)
		fd := F("%s/%s", dataRoot, hd)
		fp := F("%s/image%s", fd, ex)
		os.MkdirAll(fd, os.ModePerm)
		if !util.DoesFileExist(fp) {
			ioutil.WriteFile(fp, bytesO, os.ModePerm)
		}

		if dbstorage.QueryHasRows(db.Build().Se("*").Fr("images").Wh("hash", str).Wh("uploader", strconv.Itoa(u.ID)).Exe()) {
			original = false
		} else {
			dbstorage.InsertsLock.Lock()
			id := db.QueryNextID("images")
			db.Build().Ins("images", id, str, u.ID, fh.Filename, T()).Exe()
			dbstorage.InsertsLock.Unlock()
			util.Log("Added file", str, "by", u.Username)
		}

		writeJson(w, map[string]interface{}{
			"message":  "success",
			"name":     fh.Filename,
			"hash":     str,
			"original": original,
			"url":      util.FullHost(r) + "/p/" + str,
		})
	}))

	htp.Register("/b/users/update/*", "PUT", mw(func(w http.ResponseWriter, r *http.Request) {
		c := htp.GetController(r)
		_, err := pageInit(c, r, w, http.MethodPut, true, true, true, false)
		if err != nil {
			writeJson(w, map[string]interface{}{})
			return
		}
		uid := r.RequestURI[16:]
		if !isInt(uid) {
			writeJson(w, map[string]interface{}{})
			return
		}
		k := c.GetFormString("key")
		v := c.GetFormString("value")
		for true {
			if k == "is_member" || k == "is_admin" {
				if v == "0" || v == "1" {
					break
				}
			}
			writeJson(w, map[string]interface{}{})
			return
		}
		db.Build().Up("users", k, v).Wh("id", uid).Exe()
		writeJson(w, map[string]interface{}{
			"id":  uid,
			"key": k,
			"val": v,
		})
	}))

	//

	etc.StartServer()
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
	adm := util.Btoi(id == 1)
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
			splited = append(splited, str[start:len(str)])
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

func getQueryInt(r *http.Request, w http.ResponseWriter, name string, required bool) (int64, error) {
	v := r.URL.Query().Get(name)
	if len(v) == 0 {
		return -1, E("")
	}
	return strconv.ParseInt(v, 10, 64)
}

func hashBytes(ba []byte) string {
	if config.ImgAlgo == "zeebo/blake3" {
		h := blake3.New()
		h.Write(ba)
		return hex.EncodeToString(h.Sum([]byte{}))
	}
	return util.Hash(config.ImgAlgo, ba)
}
