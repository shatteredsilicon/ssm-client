package plugin

// Service names
const (
	NameLinux      = "linux"
	NameMySQL      = "mysql"
	NameMongoDB    = "mongodb"
	NamePostgreSQL = "postgresql"
	NameProxySQL   = "proxysql"
)

// Exporter names
const (
	NodeExporter       = "node_exporter"
	MySQLExporter      = "mysqld_exporter"
	MongoDBExporter    = "mongodb_exporter"
	PostgreSQLExporter = "postgres_exporter"
	ProxySQLExporter   = "proxysql_exporter"
	SSMQanAgent        = "ssm-qan-agent"
	PMMQanAgent        = "pmm-qan-agent"
)

// Data types
const (
	TypeMetrics = "metrics"
	TypeQueries = "queries"
)

// Service types
const (
	LinuxMetrics      = "linux:metrics"
	MySQLMetrics      = "mysql:metrics"
	MongoDBMetrics    = "mongodb:metrics"
	PostgreSQLMetrics = "postgresql:metrics"
	ProxySQLMetrics   = "proxysql:metrics"
	MySQLQueries      = "mysql:queries"
	MongoDBQueries    = "mongodb:queries"
)

const (
	SSMUsername = "ssm"
)
