package main

import (
	"flag"
	cache "github.com/jfarleyx/go-simple-cache"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	log "github.com/sirupsen/logrus"
	"github.com/svcavallar/celeriac.v1"
	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"
	"net/http"
	"os"
	"time"
)

func getEnv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

const (
	Started   string = "task-started"
	Received         = "task-received"
	Succeeded        = "task-succeeded"
	Failed           = "task-failed"
	Retried          = "task-retried"
	Revoked          = "task-revoked"
	Rejected         = "task-rejected"
)

var (
	// Settings
	taskBrokerURL = getEnv(
		"BROKER_URL",
		"amqp://guest:guest@127.0.0.1:5672/test-vhost",
	) // broker URL,

	addr = getEnv(
		"ADDR",
		":9808",
	) // addr to listen on
	// Metric collectors
	celeryTaskReceived = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "celery_task_received",
			Help: "Number of started celery tasks.",
		},
		[]string{"name", "hostname"},
	)
	celeryTaskStarted = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "celery_task_started",
			Help: "Number of started celery tasks.",
		},
		[]string{"name", "hostname"},
	)
	celeryTaskSucceeded = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "celery_task_succeeded",
			Help: "Number of succeeded celery tasks.",
		},
		[]string{"name", "hostname"},
	)
	unsuccessfulTaskMetrics = map[string]*prometheus.CounterVec{
		Failed: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "celery_task_failed",
				Help: "Number of succeeded celery tasks.",
			},
			[]string{"name", "hostname"},
		),
		Rejected: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "celery_task_rejected",
				Help: "Number of succeeded celery tasks.",
			},
			[]string{"name"},
		),
		Revoked: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "celery_task_revoked",
				Help: "Number of succeeded celery tasks.",
			},
			[]string{"name"},
		),
		Retried: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "celery_task_retried",
				Help: "Number of succeeded celery tasks.",
			},
			[]string{"name", "hostname"},
		),
	}

	celeryTaskRuntime = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "celery_task_runtime",
			Help:    "Histogram of task runtime measurements.",
			Buckets: prometheus.LinearBuckets(0.05, 0.10, 50),
		},
		[]string{"name", "hostname"},
	)
	celeryTaskRuntimeSummary = prometheus.NewSummaryVec(
		prometheus.SummaryOpts{
			Name:       "celery_task_runtime_summary",
			Help:       "Summary of task runtime measurements.",
			Objectives: map[float64]float64{0.5: 0.05, 0.9: 0.01, 0.95: 0.005, 0.99: 0.001},
		},
		[]string{"name"},
	)
	// Cache for task data. Celery doesn't pass reliably all task data in each event - e.g. name isn't provided in
	// task-started but is provided in task-received. In Python Celery State object is used to fix this, here I use
	// far simpler and more performant cache since I need only specific information.
	celeryTaskUUIDNameCache = cache.New(10 * time.Minute)
)

func handleBrokerListening() {

	// Connect to RabbitMQ task queue
	TaskQueueMgr, err := celeriac.NewTaskQueueMgr(taskBrokerURL)
	if err != nil {
		log.Errorf("Failed to connect to task queue: %v", err)
		os.Exit(-1)
	}

	log.Infof("Service connected to task queue - (URL: %s)", taskBrokerURL)

	for {
		select {
		case ev := <-TaskQueueMgr.Monitor.EventsChannel:
			if ev != nil {
				if x, ok := ev.(*celeriac.TaskEvent); ok {
					log.WithFields(log.Fields{"UUID": x.UUID, "type": x.Type}).Info("Received event")
					if x.Type == Started {
						taskName, found := celeryTaskUUIDNameCache.Get(x.UUID)
						if found {
							celeryTaskStarted.WithLabelValues(taskName.(string), x.Hostname).Inc()
						}
					} else if x.Type == Received {
						celeryTaskUUIDNameCache.Set(x.UUID, x.Name)
						celeryTaskReceived.WithLabelValues(x.Name, x.Hostname).Inc()
					} else if x.Type == Succeeded {
						taskName, found := celeryTaskUUIDNameCache.Get(x.UUID)
						if found {
							celeryTaskSucceeded.WithLabelValues(taskName.(string), x.Hostname).Inc()
							log.WithFields(log.Fields{"UUID": x.UUID, "type": x.Type}).Debugf("Observing task runtime: %f", float64(x.Runtime))
							celeryTaskRuntime.WithLabelValues(taskName.(string), x.Hostname).Observe(float64(x.Runtime))
							celeryTaskRuntimeSummary.WithLabelValues(taskName.(string)).Observe(float64(x.Runtime))
						}
					} else if slices.Contains(maps.Keys(unsuccessfulTaskMetrics), x.Type) {
						taskName, found := celeryTaskUUIDNameCache.Get(x.UUID)
						if found {
							metric := unsuccessfulTaskMetrics[x.Type]
							// for unsuccessful tasks only retried and failed have hostname val
							if x.Type == Failed || x.Type == Retried {
								metric.WithLabelValues(taskName.(string), x.Hostname).Inc()
							} else {
								metric.WithLabelValues(taskName.(string)).Inc()
							}

						}
					}
				} else if _, ok := ev.(*celeriac.Event); ok {
					log.Info("Celery Event Channel: General event")
				} else {
					log.Info("Celery Event Channel: Unhandled event")
				}
			}
		}
	}
}

func main() {
	verbose := flag.Bool("v", false, "Log verbose info, by default logs only Warn and more severe.")
	flag.Parse()
	if *verbose {
		log.SetLevel(log.DebugLevel)
	} else {
		log.SetLevel(log.ErrorLevel)
	}
	log.SetFormatter(&log.TextFormatter{
		DisableColors: false,
		FullTimestamp: true,
	})
	http.Handle("/metrics", promhttp.Handler())
	go handleBrokerListening()
	prometheus.MustRegister(celeryTaskReceived)
	prometheus.MustRegister(celeryTaskStarted)
	prometheus.MustRegister(celeryTaskSucceeded)
	for _, metric := range unsuccessfulTaskMetrics {
		prometheus.MustRegister(metric)
	}
	prometheus.MustRegister(celeryTaskRuntime)
	prometheus.MustRegister(celeryTaskRuntimeSummary)
	log.Fatal(http.ListenAndServe(addr, nil))
}
