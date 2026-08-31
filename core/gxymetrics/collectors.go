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

	OnlinePlayers = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "online_players",
		Help: "Current online players on this node",
	})

	ClientRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "client_requests_total",
		Help: "Total client protocol requests processed",
	}, []string{"msg_id", "msg_name", "result"})

	ClientRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "client_request_duration_seconds",
		Help:    "Client protocol request handling duration",
		Buckets: prometheus.DefBuckets,
	}, []string{"msg_id", "msg_name", "result"})

	RoleModuleLimitTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "role_module_limit_total",
		Help: "Total Role business module admission decisions",
	}, []string{"module", "result"})

	RoleModuleDisabled = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "role_module_disabled",
		Help: "Whether a Role business module is disabled by startup policy",
	}, []string{"module"})

	GatewayPackets = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "gateway_packets_total",
		Help: "Total gateway packets received by type and result",
	}, []string{"type", "result"})

	SessionDisconnects = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "session_disconnect_total",
		Help: "Total session disconnects by reason",
	}, []string{"reason"})

	LoginInflight = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "login_inflight",
		Help: "Current Gateway logins holding an ActivateRole permit",
	})

	LoginQueueLength = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "login_queue_length",
		Help: "Current Gateway logins waiting for an ActivateRole permit",
	})

	LoginLimitTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "login_limit_total",
		Help: "Total Gateway login admission decisions",
	}, []string{"result"})

	LoginWaitDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "login_wait_duration_seconds",
		Help:    "Gateway login concurrency permit wait duration",
		Buckets: prometheus.DefBuckets,
	}, []string{"result"})

	RoleLogins = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "role_login_total",
		Help: "Total role logins by result",
	}, []string{"result"})

	RoleLogouts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "role_logout_total",
		Help: "Total role logouts by reason",
	}, []string{"reason"})

	RoleNotifyPublish = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "role_notify_publish_total",
		Help: "Total RoleNotify publish attempts",
	}, []string{"msg_type", "result", "target"})

	RoleNotifyConsume = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "role_notify_consume_total",
		Help: "Total RoleNotify consume attempts",
	}, []string{"msg_type", "result"})

	ActorLocate = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "actor_locate_total",
		Help: "Total actor locate attempts",
	}, []string{"kind", "result"})
)
