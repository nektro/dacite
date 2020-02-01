package main

import (
	"net/http"

	oauth2 "github.com/nektro/go.oauth2"
)

type Config struct {
	Root      string
	Port      int
	Providers []oauth2.Provider `json:"providers"`
	Clients   []oauth2.AppConf  `json:"clients"`
	Themes    []string          `json:"themes"`
	ImgAlgo   string
}

type User struct {
	ID        int    `json:"id"`
	Snowflake string `json:"snowflake" sqlite:"text"`
	JoinedOn  string `json:"joined_on" sqlite:"text"`
	IsMember  bool   `json:"is_member" sqlite:"tinyint(1)"`
	IsAdmin   bool   `json:"is_admin" sqlite:"tinyint(1)"`
	Username  string `json:"username" sqlite:"text"`
	Provider  string `json:"provider" sqlite:"text"`
}

type ImageRow struct {
	ID       int    `json:"id"`
	Hash     string `json:"hash" sqlite:"text"`
	Uploader int    `json:"uploader" sqlite:"int"`
	Name     string `json:"name" sqlite:"text"`
	AddedOn  string `json:"added_on" sqlite:"text"`
}

// Middleware provides a convenient mechanism for augmenting HTTP requests
// entering the application. It returns a new handler which may perform various
// operations and should finish by calling the next HTTP handler.
//
// @from https://gist.github.com/gbbr/dc731df098276f1a135b343bf5f2534a
type Middleware func(next http.HandlerFunc) http.HandlerFunc
