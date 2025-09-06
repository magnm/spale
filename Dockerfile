FROM golang:1.25 AS builder

WORKDIR /src
COPY ./ ./
RUN GOOS=linux CGO_ENABLED=0 go build -ldflags "-s -w" -o spale

FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY tls /etc/ssl/
COPY --from=builder /src/spale /
ENTRYPOINT [ "/spale" ]