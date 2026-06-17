// Package pgwire 实现 PostgreSQL wire 协议（v3）服务端，使 WiDB 可被
// JDBC 驱动（如 pgJDBC）及 psql 等标准 PostgreSQL 客户端直接连接。
//
// 协议流程：
//   - 客户端发送 StartupMessage（或先发 SSLRequest 协商 SSL）
//   - 服务端回复 AuthenticationOk + ParameterStatus + BackendKeyData + ReadyForQuery
//   - 客户端发送 Query（Simple Query 协议）
//   - 服务端执行 SQL，回复 RowDescription + DataRow* + CommandComplete + ReadyForQuery
//   - 客户端发送 Terminate 断开连接
//
// 当前实现支持 trust 认证（无密码），仅处理 Simple Query 协议。
// 类型映射：BOOL→16, INT64→int8(20), FLOAT64→float8(701), STRING→text(25), TIMESTAMP→1114。
//
// # 安全与资源保护
//
// 当前认证方式为 trust（AuthenticationOk），任何能连上监听端口的客户端均可执行 SQL。
// 在不可信网络中部署时，应通过下列方式限制访问：
//   - 将监听地址绑定到回环或内网（如 127.0.0.1），避免暴露到公网；
//   - 在网络边界（防火墙/安全组）限制端口访问来源；
//   - 通过反向代理或带认证的网关前置 pgwire 端口。
//
// 为防止恶意客户端通过大量连接或长空闲连接耗尽 goroutine，NewServer 默认启用：
//   - 最大并发连接数（defaultMaxConns=256），超限连接立即关闭；
//   - 单次读取空闲超时（defaultIdleTimeout=5m），空闲连接自动断开；
//   - 单次写入超时（defaultWriteTimeout=30s），慢客户端不会阻塞 goroutine。
//
// 以上参数可通过 WithMaxConns / WithIdleTimeout / WithWriteTimeout 选项覆盖。
package pgwire
