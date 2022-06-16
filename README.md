# celery-go-exporter

![build](https://github.com/mdomans/celery-go-exporter/actions/workflows/go.yml/badge.svg)

Prometheus exporter for Celery written in Go ... so that it's fast.

---

### Usage

Exporter needs to be able to connect to AMQP broker and have the valid URL provided. 

#### Configuration

Via env variables:
* `BROKER_URL` - broker URL in `amqp://login:pass@host:port/vhost` format, by default it uses `amqp://guest:guest@127.0.0.1:5672/test-vhost`, adjust your `vhost` at minimum
* `ADDR` - if you need exporter to be accessible at addr different than default `:9808`

`/metrics` will be available at `ADDR/metrics`
