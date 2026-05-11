package gxymetrics

import "github.com/prometheus/client_golang/prometheus"

var (
	TcpConnections = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "tcp_connections",
		Help: "Current TCP connections",
	}, []string{"role"})

	ActorActiveCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "actor_active_count",
		Help: "Currently active actors",
	}, []string{"kind"})

	ActorMessages = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "actor_messages_total",
		Help: "Total actor messages processed",
	}, []string{"kind"})

	ActorMessageDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "actor_message_duration_seconds",
		Help:    "Actor message processing duration",
		Buckets: prometheus.DefBuckets,
	}, []string{"kind"})

	DBQueryDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "db_query_duration_seconds",
		Help:    "Database query duration",
		Buckets: prometheus.DefBuckets,
	})

	RedisRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "redis_request_duration_seconds",
		Help:    "Redis request duration",
		Buckets: prometheus.DefBuckets,
	}, []string{"cmd"})
)
