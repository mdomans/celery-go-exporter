package main

import (
	"flag"
	cache "github.com/jfarleyx/go-simple-cache"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/svcavallar/celeriac.v1"
	"log"
	"net/http"
	"os"
	"time"
)

var taskBrokerURI = flag.String(
	"taskBrokerURI",
	"amqp://guest:guest@127.0.0.1:5672/muckrack",
	"task broker URL",
)
var addr = flag.String(
	"addr",
	":9101",
	"addr to listen on",
)

var celeryTaskUUIDNameCache = cache.New(10 * time.Minute)

var celeryTaskStarted = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "celery_task_started",
		Help: "Number of started celery tasks.",
	},
	[]string{"name", "hostname"},
)

var celeryTaskReceived = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "celery_task_received",
		Help: "Number of started celery tasks.",
	},
	[]string{"name", "hostname"},
)

var celeryTaskSucceeded = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "celery_task_succeeded",
		Help: "Number of succeeded celery tasks.",
	},
	[]string{"name", "hostname"},
)

var celeryTaskRuntime = prometheus.NewHistogramVec(
	prometheus.HistogramOpts{
		Name:    "celery_task_runtime",
		Help:    "Histogram of task runtime measurements.",
		Buckets: prometheus.LinearBuckets(0.05, 0.10, 50),
	},
	[]string{"name", "hostname"},
)

var celeryTaskRuntimeSummary = prometheus.NewSummaryVec(
	prometheus.SummaryOpts{
		Name:       "celery_task_runtime_summary",
		Help:       "Summary of task runtime measurements.",
		Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.95: 0.005, 0.99: 0.001},
	},
	[]string{"name"},
)

type CeleryMetricsExporter struct {
	taskBrokerURI string
}

func NewCeleryMetricsExporter(taskBrokerURI string) *CeleryMetricsExporter {
	return &CeleryMetricsExporter{
		taskBrokerURI: taskBrokerURI,
	}
}

func (e *CeleryMetricsExporter) HandleBrokerListening() {

	// Connect to RabbitMQ task queue
	TaskQueueMgr, err := celeriac.NewTaskQueueMgr(e.taskBrokerURI)
	if err != nil {
		log.Printf("Failed to connect to task queue: %v", err)
		os.Exit(-1)
	}

	log.Printf("Service connected to task queue - (URL: %s)", e.taskBrokerURI)

	for {
		select {
		case ev := <-TaskQueueMgr.Monitor.EventsChannel:
			if ev != nil {
				if x, ok := ev.(*celeriac.TaskEvent); ok {
					log.Printf("Celery Event Channel: Task event - [ID]: %s, %s", x.UUID, x.Type)
					if x.Type == "task-started" {
						taskName, found := celeryTaskUUIDNameCache.Get(x.UUID)
						if found {
							celeryTaskStarted.WithLabelValues(taskName.(string), x.Hostname).Inc()
						}
					} else if x.Type == "task-received" {
						celeryTaskUUIDNameCache.Set(x.UUID, x.Name)
						celeryTaskReceived.WithLabelValues(x.Name, x.Hostname).Inc()
					} else if x.Type == "task-succeeded" {
						taskName, found := celeryTaskUUIDNameCache.Get(x.UUID)
						if found {
							celeryTaskSucceeded.WithLabelValues(taskName.(string), x.Hostname).Inc()
							log.Printf("Observing task runtime: %f", float64(x.Runtime))
							celeryTaskRuntime.WithLabelValues(taskName.(string), x.Hostname).Observe(float64(x.Runtime))
							celeryTaskRuntimeSummary.WithLabelValues(taskName.(string)).Observe(float64(x.Runtime))
						}
					}
				} else if x, ok := ev.(*celeriac.Event); ok {
					log.Printf("Celery Event Channel: General event - %s [Hostname]: %s - [Data]: %v", x.Type, x.Hostname, x.Data)
				} else {
					log.Printf("Celery Event Channel: Unhandled event: %v", ev)
				}
			}
		}
	}
}

func main() {
	log.SetPrefix("SERVER: ")
	log.SetFlags(0)
	flag.Parse()
	http.Handle("/metrics", promhttp.Handler())
	exporter := NewCeleryMetricsExporter(*taskBrokerURI)
	go exporter.HandleBrokerListening()
	prometheus.MustRegister(celeryTaskReceived)
	prometheus.MustRegister(celeryTaskStarted)
	prometheus.MustRegister(celeryTaskSucceeded)
	prometheus.MustRegister(celeryTaskRuntime)
	prometheus.MustRegister(celeryTaskRuntimeSummary)
	log.Fatal(http.ListenAndServe(*addr, nil))
}
