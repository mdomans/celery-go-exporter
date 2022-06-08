FROM golang:1.17-alpine as bild-env
# Envs
ENV APP_NAME celery-go-exporter
ENV CMD_PATH main.go
# Copy application data
COPY . $GOPATH/src/$APP_NAME
WORKDIR $GOPATH/src/$APP_NAME
# Budild application
#RUN go build -v -o /$APP_NAME $GOPATH/src/$APP_NAME/$CMD_PATH
# Expose port
EXPOSE 9808
# Start app
CMD go run .