FROM golang:1.21 AS build
ENV GO111MODULE=on
ENV CGO_ENABLED=0


COPY . /app
WORKDIR /app

RUN go build -o vpn-proxy vpn-proxy.go
RUN strip vpn-proxy

FROM scratch

WORKDIR /

COPY --from=build /app .

ENTRYPOINT ["./vpn-proxy"]
