FROM golang:alpine as golang
WORKDIR /go/src/dacite
COPY . .
RUN apk add --no-cache git libc-dev musl-dev build-base gcc ca-certificates \
    && export VCS_REF=$(git describe --tags) \
    && echo $VCS_REF \
    && go install -v github.com/rakyll/statik \
    && $GOPATH/bin/statik -src="./www/" \
    && go get -v . \
    && CGO_ENABLED=1 go build -ldflags "-s -w -X main.Version=$VCS_REF" .

FROM alpine
COPY --from=golang /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=golang /go/src/dacite/dacite /app/dacite

VOLUME /data
ENTRYPOINT ["/app/dacite", "--port", "80", "--config", "/data/config.json"]
