FROM golang:alpine as golang
WORKDIR /go/src/dacite
COPY . .
ARG BUILD_NUM
RUN apk add --no-cache git libc-dev musl-dev build-base gcc ca-certificates \
&& export GO_VERSION=$(go version | cut -d' ' -f3) \
&& export VCS_REF="v${BUILD_NUM}-docker-$GO_VERSION" \
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
