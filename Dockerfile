FROM golang:1.13 AS builder

WORKDIR /go/src/app
COPY . .
RUN GO111MODULE=on go mod download
RUN cd cmd/tss
RUN CGO_ENABLED=0 GOOS=linux go build -o tss

#
# Main
#
FROM golang:alpine

RUN mkdir -p /go/bin

COPY --from=builder /go/src/app/cmd/tss/tss /go/bin/tss
EXPOSE 6668
EXPOSE 8080
CMD ['/go/bin/tss']