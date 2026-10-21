package gxymetrics

import "github.com/prometheus/client_golang/prometheus"

// latencyBuckets 用于亚毫秒路径(actor 处理、Redis 命令、DB 查询)。
//
// prometheus.DefBuckets 从 5ms 起步,这三类样本的绝大多数会全落进第一个桶,
// 做延迟优化时没有分辨率;这里从 0.1ms 起,覆盖到 5s。
var latencyBuckets = []float64{
	.0001, .00025, .0005, .001, .0025, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5,
}

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
		Buckets: latencyBuckets,
	}, []string{"kind"})

	DBQueryDuration = prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "db_query_duration_seconds",
		Help:    "Database query duration",
		Buckets: latencyBuckets,
	})

	RedisRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "redis_request_duration_seconds",
		Help:    "Redis request duration",
		Buckets: latencyBuckets,
	}, []string{"cmd"})

	// RedisErrors 与耗时分开计数:耗时直方图里混着成功与失败,单看它算不出失败率。
	RedisErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "redis_errors_total",
		Help: "Redis command errors",
	}, []string{"cmd"})

	// MQQueueDepth 是待处理消息数(消息到达与出队时更新,不在抓取时计算)。
	MQQueueDepth = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "mq_queue_depth",
		Help: "Messages waiting for mq workers",
	})
	MQQueueCapacity = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "mq_queue_capacity",
		Help: "mq worker queue capacity (depth/capacity is the saturation ratio)",
	})
	// MQHandlerErrors 只计数不分主题:主题里含节点实例名(roleNotifyTopic),
	// 作为标签会让序列基数随实例数无限增长。
	MQHandlerErrors = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "mq_handler_errors_total",
		Help: "Message handlers that returned an error (message dropped, no retry)",
	})

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

	// GatewayPackets 的 type 表示"哪一层":message 是解码成功、进入处理;
	// frame 是帧层失败(解码失败或长度非法)。result 是 ok/error。
	// 一条成功投递的消息只记一次(message/ok 或 message/error),帧层失败单独记。
	GatewayPackets = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "gateway_packets_total",
		Help: "Total gateway inbox items by layer (message/frame) and result",
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
