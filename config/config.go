package config

type Config struct {
	Port                string   `env:"PORT" envDefault:"8080"`
	TlsPort             string   `env:"TLS_PORT" envDefault:"8443"`
	TlsCert             string   `env:"TLS_CERT"`
	TlsKey              string   `env:"TLS_KEY"`
	LogLevel            string   `env:"LOG_LEVEL" envDefault:"info"`
	NamespaceSelector   []string `env:"NAMESPACE_SELECTOR" envDefault:"*"`
	ExceptNamespaces    []string `env:"EXCEPT_NAMESPACES" envDefault:"kube-system"`
	SpotNodeLabels      []string `env:"SPOT_NODE_LABELS" envDefault:"nodepool=spot"`
	SpotNodeTolerations []string `env:"SPOT_NODE_TOLERATIONS" envDefault:"type=spot:NoSchedule"`
	SpotRatio           string   `env:"SPOT_RATIO" envDefault:"3:1"`
	// 0 disables the per-node startup throttle
	MaxStartingPodsPerNode int `env:"MAX_STARTING_PODS_PER_NODE" envDefault:"2"`
}

// Initialised by server/run.go
var Current Config
