#!/usr/bin/env bash

set -x


########################
#    Golang Testing    #
########################

# https://golang.org/pkg/testing/
go test

# https://golang.org/cmd/vet/
go vet -composites=false

# https://github.com/fzipp/gocyclo
$GOPATH/bin/gocyclo -over 9 .

# https://golang.org/x/lint
$GOPATH/bin/golint .

# https://github.com/gordonklaus/ineffassign
$GOPATH/bin/ineffassign .

# https://github.com/client9/misspell
$GOPATH/bin/misspell -error .

# https://github.com/golangci/golangci-lint
$GOPATH/bin/golangci-lint run -D errcheck

########################
#  Javascript Testing  #
########################

# https://eslint.org/
eslint --format ./.eslintrc.fmt.js ./www/
