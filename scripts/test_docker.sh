#!/usr/bin/env bash

set -e
set -x

docker build --rm . -t mantle_local
docker run --rm -p 80:8000 -v ~/dev/golang/dacite/data/:/data/ mantle_local
