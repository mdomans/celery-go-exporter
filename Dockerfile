FROM golang:1.18-alpine

# Install the Certificate-Authority certificates for the app to be able to make
# calls to HTTPS endpoints.
RUN apk add --no-cache ca-certificates

WORKDIR /app

# Grab dependencies and create the build environment
COPY go.mod ./
COPY go.sum ./
RUN go mod download

# build the app
COPY *.go ./
RUN go build -o /celery-go-exporter

EXPOSE 9808

# Perform all further action as an unprivileged user.
USER 65535:65535

ENTRYPOINT ["/celery-go-exporter"]