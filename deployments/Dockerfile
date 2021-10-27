FROM golang:1.17 AS builder

# Set the Current Working Directory inside the container
WORKDIR /app

COPY go.mod .
COPY go.sum .

RUN go mod download

COPY . .

#RUN go test -v ./...

WORKDIR /app/cmd/ingress-coap

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build

FROM registry.access.redhat.com/ubi8/ubi-minimal
WORKDIR /opt/diwise

COPY --from=builder --chown=1001 /app/cmd/ingress-coap/ingress-coap /opt/diwise

RUN chown 1001 /opt/diwise
RUN chmod 700 /opt/diwise

USER 1001

ENTRYPOINT ["/opt/diwise/ingress-coap"]
