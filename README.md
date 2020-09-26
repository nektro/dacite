# Dacite
![loc](https://sloc.xyz/github/nektro/dacite)
[![license](https://img.shields.io/github/license/nektro/dacite.svg)](https://github.com/nektro/dacite/blob/master/LICENSE)
[![discord](https://img.shields.io/discord/551971034593755159.svg?logo=discord)](https://discord.gg/P6Y4zQC)
[![circleci](https://circleci.com/gh/nektro/dacite.svg?style=svg)](https://circleci.com/gh/nektro/dacite)
[![release](https://img.shields.io/github/v/release/nektro/dacite)](https://github.com/nektro/dacite/releases/latest)
[![goreportcard](https://goreportcard.com/badge/github.com/nektro/dacite)](https://goreportcard.com/report/github.com/nektro/dacite)
[![codefactor](https://www.codefactor.io/repository/github/nektro/dacite/badge)](https://www.codefactor.io/repository/github/nektro/dacite)
[![issuehunt](https://img.shields.io/badge/issuehunt-dacite-38d39f)](https://issuehunt.io/r/nektro/dacite)

Hash-based image image storage and upload.

## Getting Started
These instructions will help you get the project up and running and are required before moving on.

### Flags

| Name | Type | Default | Description |
|------|------|---------|-------------|
| `--port` | `int` | `8000` | Port to bind web server to. |
| `--root` | `string` | none. | Path of root directory to store files. |
| `--algo` | `string` | `SHA1` | Hash algo to use for files. (One of `MD4`, `MD5`, `SHA1`, `SHA224`, `SHA256`, `SHA384`, `SHA512`, `MD5SHA1`, `RIPEMD160`, `SHA3_224`, `SHA3_256`, `SHA3_384`, `SHA3_512`, `SHA512_224`, `SHA512_256`, `BLAKE2s_256`, `BLAKE2b_256`, `BLAKE2b_384`, `BLAKE2b_512`, `zeebo/blake3`) |

### Creating External Auth Credentials
In order to get started with Dacite, you will need to create an app on your Identity Provider(s) of choice. See the [nektro/go.oauth2](https://github.com/nektro/go.oauth2#readme) docs for more detailed info on this process on where to go and what data you'll need.

Here you can also fill out a picture and description that will be displayed during the authorization of users on your chosen Identity Provider. When prompted for the "Redirect URI" during the app setup process, the URL to use will be `http://dacite/callback`, replacing `dacite` with any origins you wish Dacite to be usable from, such as `example.com` or `localhost:800`.

Once you have finished the app creation process you should now have a Client ID and Client Secret. These are passed into Dacite through flags as well.

| Name | Type | Default | Description |
|------|------|---------|-------------|
| `--auth-{IDP-ID}-id` | `string` | none. | Client ID. |
| `--auth-{IDP-ID}-secret` | `string` | none. | Client Secret. |

The Identity Provider IDs can be found from the table in the [nektro/go.oauth2](https://github.com/nektro/go.oauth2#readme) documentation.

## Development

### Prerequisites
- The Go Language 1.12+ (https://golang.org/dl/)
- Docker (https://www.docker.com/products/docker-desktop)
- Docker Compose (https://docs.docker.com/compose/install/)

### Installing
Run
```
$ git clone https://github.com/nektro/dacite
$ cd ./dacite/
$ go get -v .
$ docker-compose up
```

## Deployment
Pre-compiled binaries can be obtained from https://github.com/nektro/dacite/releases/latest.

Or you can build from source:
```
$ ./scripts/build_all.sh
```

# Built With
- https://github.com/gorilla/sessions
- https://github.com/nektro/go-util
- https://github.com/nektro/go.dbstorage
- https://github.com/nektro/go.etc
- https://github.com/nektro/go.oauth2
- https://github.com/rakyll/statik

## Contact
- hello@nektro.net
- Meghan#2032 on discordapp.com
- https://twitter.com/nektro

## License
Apache 2.0
