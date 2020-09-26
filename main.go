package main

import (
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/nektro/go-util/arrays/stringsu"
	"github.com/nektro/go-util/util"
	"github.com/nektro/go-util/vflag"
	dbstorage "github.com/nektro/go.dbstorage"
	etc "github.com/nektro/go.etc"
	"github.com/nektro/go.etc/htp"

	_ "github.com/nektro/dacite/statik"
	. "github.com/nektro/go-util/alias"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

// byte size consts
const (
	Byte     = 1
	Kilobyte = Byte * 1024
	Megabyte = Kilobyte * 1024
)

// more global vars
var (
	Version  = "vMASTER"
	dataRoot string
	config   = new(Config)
	db       dbstorage.Database
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
		redir := []string{
			"astheno/jwt: token: signature is invalid",
			"astheno/jwt: token: token contains an invalid number of segments",
		}
		if stringsu.Contains(redir, msg) {
			w.Header().Set("location", "/login")
			w.WriteHeader(http.StatusFound)
			return
		}
		w.WriteHeader(code)
		writeJson(w, map[string]string{
			"message": msg,
		})
	}

	//

	htp.Register("/portal", "GET", func(w http.ResponseWriter, r *http.Request) {
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
	})

	htp.Register("/upload", "GET", func(w http.ResponseWriter, r *http.Request) {
		c := htp.GetController(r)
		u, err := pageInit(c, r, w, http.MethodGet, true, true, false, true)
		if err != nil {
			return
		}
		writePage(r, w, u, "upload", "upload", "Upload", map[string]interface{}{
			//
		})
	})

	htp.Register("/p/{hash:[0-9a-f]+}{ext:(?:.[0-9a-z]+)?}", "GET", func(w http.ResponseWriter, r *http.Request) {
		c := htp.GetController(r)
		_, err := pageInit(c, r, w, http.MethodGet, false, false, false, true)
		if err != nil {
			return
		}
		hsh := mux.Vars(r)["hash"]
		ext := mux.Vars(r)["ext"]
		hd := strings.Join(splitByWidthMake(hsh, 2, config.MaxFolderDepth), "/")
		fd := F("%s/%s", dataRoot, hd)
		fl, _ := ioutil.ReadDir(fd)
		if len(fl) == 0 {
			http.NotFound(w, r)
			return
		}
		file := fl[0]
		fn := file.Name()
		fe := filepath.Ext(fn)
		if len(ext) > 0 && fe != ext {
			http.NotFound(w, r)
			return
		}
		w.Header().Add("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Add("ETag", F(`"%s"`, hsh))
		w.Header().Add("Content-Disposition", `inline; filename="`+file.Name()+`"`)
		http.ServeFile(w, r, fd+"/"+file.Name())
	})

	htp.Register("/users", "GET", func(w http.ResponseWriter, r *http.Request) {
		c := htp.GetController(r)
		u, err := pageInit(c, r, w, http.MethodGet, true, true, true, true)
		if err != nil {
			return
		}
		writePage(r, w, u, "users", "users", "🔨 All Users", map[string]interface{}{
			"users": queryAllUsers(),
		})
	})

	//

	htp.Register("/b/upload", "POST", func(w http.ResponseWriter, r *http.Request) {
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
		str := hashBytes(bytesO)
		if len(str) == 0 {
			writeJson(w, map[string]interface{}{
				"message": "hash error",
			})
			return
		}
		original := true

		hd := strings.Join(splitByWidthMake(str, 2, config.MaxFolderDepth), "/")
		fd := F("%s/%s", dataRoot, hd)
		fp := F("%s/%s", fd, fh.Filename)
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
	})

	htp.Register("/b/users/update/*", "PUT", func(w http.ResponseWriter, r *http.Request) {
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
		if !(k == "is_member" || k == "is_admin") {
			return
		}
		if !(v == "0" || v == "1") {
			return
		}
		db.Build().Up("users", k, v).Wh("id", uid).Exe()
		writeJson(w, map[string]interface{}{
			"id":  uid,
			"key": k,
			"val": v,
		})
	})

	//

	etc.StartServer()
}
