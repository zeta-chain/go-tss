FROM golang:1.13-alpine AS builder

RUN apk update && apk add --no-cache git
WORKDIR /go/src/app
COPY . .
RUN GO111MODULE=on go mod download
RUN cd cmd/tss
RUN GOOS=linux GOARCH=amd64 go build -ldflags="-w -s" -o tss

#
# Main
#
FROM alpine:latest

RUN apk add --update ca-certificates
RUN mkdir -p /go/bin
COPY --from=builder /go/src/app/cmd/tss/tss /go/bin/tss
EXPOSE 6668
EXPOSE 8080
ENTRYPOINT /go/bin/tss
CMD ['/go/bin/tss']